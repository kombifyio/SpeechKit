//go:build linux

// Package dictation implements the POST /v1/dictation/transcribe handler.
// It is a thin adapter around the Framework's STT router: accept audio bytes
// and metadata, normalize to canonical PCM via internal/server/audio, and
// delegate to the router. The handler has no knowledge of which provider
// serves the request; that's the router's job.
package dictation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/storageauth"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// Transcriber is the minimal surface the handler needs from an STT router.
// The production implementation is `internal/router.Router`; tests provide
// a fake.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Options configures a single handler instance.
type Options struct {
	Router                 Transcriber
	MaxUploadMB            int    // request body ceiling; 0 disables the limit (discouraged)
	MaxDecodedAudioSeconds int    // decoded PCM duration ceiling; 0 disables the decode-duration cap
	DefaultPrompt          string // applied when the request does not provide a prompt
	Store                  store.Store
	ActiveTemplateIDs      []string
	// DefaultProviderProfileID is the server's configured Dictation primary
	// (ModelSelection.Dictate.PrimaryProfileID). It is the lowest-precedence
	// provider preference: explicit request override → edge-injected user
	// preference → this default → the router's configured fallback order.
	DefaultProviderProfileID string
}

// Handler implements the dictation HTTP surface.
type Handler struct {
	router           Transcriber
	maxBytes         int64
	decodeLimits     audio.DecodeLimits
	defaultPrompt    string
	store            store.Store
	activeTemplates  []string
	defaultProfileID string
}

// New constructs a Handler. The router must be non-nil; a zero maxBytes
// defaults to 25 MB to match the documented API contract.
func New(opts Options) (*Handler, error) {
	if opts.Router == nil {
		return nil, errors.New("dictation: router must not be nil")
	}
	maxBytes := int64(25) << 20
	if opts.MaxUploadMB > 0 {
		maxBytes = int64(opts.MaxUploadMB) << 20
	}
	return &Handler{
		router:           opts.Router,
		maxBytes:         maxBytes,
		decodeLimits:     audio.DecodeLimits{MaxDecodedAudioSeconds: opts.MaxDecodedAudioSeconds},
		defaultPrompt:    strings.TrimSpace(opts.DefaultPrompt),
		store:            opts.Store,
		activeTemplates:  append([]string(nil), opts.ActiveTemplateIDs...),
		defaultProfileID: strings.TrimSpace(opts.DefaultProviderProfileID),
	}, nil
}

// Mount registers the handler on the given mux at /v1/dictation/transcribe.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/dictation/transcribe", h)
}

// response body shape — kept stable across versions so API consumers and
// future OSS integrators can pin to this contract.
type transcribeResponse struct {
	Text                 string                          `json:"text"`
	Language             string                          `json:"language,omitempty"`
	DurationMs           int64                           `json:"duration_ms"`
	LatencyMs            int64                           `json:"latency_ms"`
	Provider             string                          `json:"provider,omitempty"`
	Model                string                          `json:"model,omitempty"`
	Confidence           float64                         `json:"confidence,omitempty"`
	SourceInfo           *sourceMeta                     `json:"source,omitempty"`
	Speakers             *speaker.DiarizationResult      `json:"speakers,omitempty"`
	CustomizationActions []speechkit.CustomizationAction `json:"customization_actions,omitempty"`
}

type sourceMeta struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	DurationMs int64  `json:"duration_ms"`
}

type transcribeJSONRequest struct {
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`   // "wav" | "mp3" | "pcm16"
	Language    string `json:"language"` // "de" | "en" | "auto"
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	// ProviderProfileID explicitly pins the STT provider profile for this
	// request (ops parity with the streaming `start` frame). Validated
	// against the Dictation provider-profile catalog; unknown IDs are
	// rejected with 400 invalid_provider_profile. When omitted, the server
	// resolves the provider from the edge-injected user preference headers
	// and its configured ModelSelection primary.
	ProviderProfileID string `json:"provider_profile_id"`
	// ConversationContext carries preceding dialogue turns (oldest first, no
	// speaker labels) for providers whose speech models condition on
	// conversational context — e.g. AssemblyAI Universal-3.5 Pro sync.
	// Providers without native support ignore it.
	ConversationContext []string        `json:"conversation_context"`
	Speaker             speaker.Options `json:"speaker"`
	SpeakerOptions      speaker.Options `json:"speaker_options"`
}

// ServeHTTP routes the request by Content-Type.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only POST is accepted on this endpoint")
		return
	}

	ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		h.handleMultipart(w, r)
	case strings.HasPrefix(ct, "application/json"):
		h.handleJSON(w, r)
	default:
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be multipart/form-data or application/json")
	}
}

func (h *Handler) handleMultipart(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBytes)
	defer func() { _ = r.Body.Close() }()

	// Bound memory: up to 32 KB in-memory before spilling to tmp.
	if err := r.ParseMultipartForm(32 << 10); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds maximum %d bytes", h.maxBytes))
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_multipart", "failed to parse multipart body: "+err.Error())
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "missing_audio",
			"multipart body must include an 'audio' file part")
		return
	}
	defer func() { _ = file.Close() }()

	if header.Size > 0 && header.Size > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio file %d bytes exceeds maximum %d bytes", header.Size, h.maxBytes))
		return
	}

	// The part's Content-Type is more reliable than the outer request's.
	partCT := header.Header.Get("Content-Type")

	profileRef, err := h.resolveProviderProfile(r.Context(), r.FormValue("provider_profile_id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_provider_profile", err.Error())
		return
	}
	opts := stt.TranscribeOpts{
		Language:          strings.TrimSpace(r.FormValue("language")),
		Model:             strings.TrimSpace(r.FormValue("model")),
		Prompt:            strings.TrimSpace(r.FormValue("prompt")),
		ProviderProfileID: profileRef,
		Speaker:           parseSpeakerOptionsFromForm(r),
	}
	opts.Prompt = h.resolvePrompt(opts.Prompt)
	h.transcribeAndReply(w, r, file, partCT, opts)
}

func (h *Handler) handleJSON(w http.ResponseWriter, r *http.Request) {
	bodyReader := http.MaxBytesReader(w, r.Body, h.maxBytes)
	defer func() { _ = r.Body.Close() }()

	var body transcribeJSONRequest
	if err := json.NewDecoder(bodyReader).Decode(&body); err != nil {
		// MaxBytesError sets a specific status on the writer; translate to
		// our error envelope.
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds maximum %d bytes", h.maxBytes))
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json",
			"failed to decode request JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.AudioBase64) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_audio",
			"JSON body must include 'audio_base64'")
		return
	}

	raw, err := base64.StdEncoding.DecodeString(body.AudioBase64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_base64",
			"audio_base64 is not valid base64: "+err.Error())
		return
	}

	// Build a Content-Type hint from the explicit format field.
	formatHint := ""
	switch strings.ToLower(strings.TrimSpace(body.Format)) {
	case "wav":
		formatHint = "audio/wav"
	case "mp3":
		formatHint = "audio/mpeg"
	case "pcm16", "pcm":
		formatHint = "audio/L16;rate=16000;channels=1"
	}

	profileRef, err := h.resolveProviderProfile(r.Context(), body.ProviderProfileID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_provider_profile", err.Error())
		return
	}
	opts := stt.TranscribeOpts{
		Language:            strings.TrimSpace(body.Language),
		Model:               strings.TrimSpace(body.Model),
		Prompt:              strings.TrimSpace(body.Prompt),
		ConversationContext: body.ConversationContext,
		ProviderProfileID:   profileRef,
		Speaker:             resolveSpeakerOptions(body.Speaker, body.SpeakerOptions),
	}
	opts.Prompt = h.resolvePrompt(opts.Prompt)

	h.transcribeBytes(w, r, raw, formatHint, opts)
}

func (h *Handler) resolvePrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt != "" {
		return prompt
	}
	return h.defaultPrompt
}

// providerLister is the optional router surface used to check whether a
// preferred provider is actually configured. The production STT router
// (internal/router.Router) implements it; a custom Transcriber may omit it,
// in which case preferences are applied without an availability check (the
// router still falls back when it cannot serve them).
type providerLister interface {
	AvailableProviders() []string
}

// resolveProviderProfile applies the server-side provider precedence for one
// batch request (AI voice target, "User Voice Preferences"):
//
//	explicit request override → edge-injected user preference (primary, then
//	secondary, skipping providers this server does not have) → configured
//	ModelSelection primary → router default order (empty).
//
// The returned reference is either a full provider-profile ID or a bare
// provider name; both are understood by the STT router's candidate
// prioritization, which keeps every other configured provider as fallback.
// Only the explicit override can fail — preferences degrade silently instead
// of hard-erroring, and the response reports the provider actually used.
func (h *Handler) resolveProviderProfile(ctx context.Context, explicit string) (string, error) {
	if trimmed := strings.TrimSpace(explicit); trimmed != "" {
		normalized := speechkit.NormalizeProviderProfileID(trimmed)
		if !dictationProfileExists(normalized) {
			return "", fmt.Errorf("unknown dictation provider profile %q; see GET /v1/catalog/profiles?mode=dictation", trimmed)
		}
		return normalized, nil
	}
	prefs := middleware.VoicePrefsFromContext(ctx)
	for _, pref := range []string{prefs.STTPrimary, prefs.STTSecondary} {
		provider := speechkit.NormalizeProviderID(pref)
		if provider == "" || strings.Contains(provider, ".") {
			continue
		}
		if h.providerAvailable(provider) {
			return provider, nil
		}
	}
	return h.defaultProfileID, nil
}

func (h *Handler) providerAvailable(provider string) bool {
	lister, ok := h.router.(providerLister)
	if !ok {
		return true
	}
	for _, name := range lister.AvailableProviders() {
		if strings.EqualFold(strings.TrimSpace(name), provider) {
			return true
		}
	}
	return false
}

func dictationProfileExists(profileID string) bool {
	for _, profile := range speechkit.ProfilesForMode(speechkit.ModeDictation) {
		if profile.ID == profileID {
			return true
		}
	}
	return false
}

// transcribeAndReply buffers the reader, decodes, and hands off.
func (h *Handler) transcribeAndReply(w http.ResponseWriter, r *http.Request, reader io.Reader, contentType string, opts stt.TranscribeOpts) {
	lr := io.LimitReader(reader, h.maxBytes+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_failed",
			"failed to read audio: "+err.Error())
		return
	}
	if int64(len(raw)) > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio exceeds maximum %d bytes", h.maxBytes))
		return
	}
	h.transcribeBytes(w, r, raw, contentType, opts)
}

func (h *Handler) transcribeBytes(w http.ResponseWriter, r *http.Request, raw []byte, contentType string, opts stt.TranscribeOpts) {
	decoded, err := audio.DecodeWithLimits(r.Context(), raw, contentType, h.decodeLimits)
	if err != nil {
		if errors.Is(err, audio.ErrUnsupportedFormat) {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_audio_format",
				err.Error())
			return
		}
		if errors.Is(err, audio.ErrDecodedAudioTooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_audio",
			"failed to decode audio: "+err.Error())
		return
	}
	if len(decoded.PCM) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "empty_audio",
			"decoded audio contains no samples")
		return
	}

	started := time.Now()
	durationSecs := float64(decoded.DurationMs) / 1000.0
	ctx := r.Context()
	opts, replacements := h.applyCustomizationHints(ctx, opts)
	result, err := h.router.Route(ctx, decoded.PCM, durationSecs, opts)
	latency := time.Since(started)

	if err != nil {
		// Router errors map to 503 (provider unavailable) in v1 since we
		// don't yet distinguish transient vs permanent. Revisit in v1.1.
		slog.Warn("dictation: router error", // #nosec G706 -- slog writes structured fields; request-derived values are attributes, not message format.
			"err", err,
			"duration_ms", decoded.DurationMs,
			"format", decoded.SourceFormat,
		)
		httpx.WriteError(w, http.StatusServiceUnavailable, "provider_unavailable",
			"no STT provider could satisfy the request: "+err.Error())
		return
	}
	if result == nil {
		httpx.WriteError(w, http.StatusInternalServerError, "empty_result",
			"STT router returned nil result")
		return
	}
	if strings.TrimSpace(result.Text) == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "empty_transcript",
			"STT returned an empty transcript; the audio may contain no speech")
		return
	}
	var customizationActions []speechkit.CustomizationAction
	if len(replacements) > 0 {
		applied, err := internalcustomize.Apply(result.Text, replacements, speechcustomize.StagePostSTT)
		if err != nil {
			slog.Debug("dictation: customization replacements skipped", "err", err)
		} else {
			result.Text = applied.Text
			customizationActions = internalcustomize.PublicActions(applied.Actions)
			h.recordCustomizationUsage(ctx, replacements, applied.Matches, firstNonEmpty(result.Language, opts.Language))
		}
	}

	resp := transcribeResponse{
		Text:       result.Text,
		Language:   firstNonEmpty(result.Language, opts.Language),
		DurationMs: decoded.DurationMs,
		LatencyMs:  latency.Milliseconds(),
		Provider:   result.Provider,
		Model:      result.Model,
		Confidence: result.Confidence,
		SourceInfo: &sourceMeta{
			Format:     decoded.SourceFormat,
			SampleRate: decoded.SourceRate,
			Channels:   decoded.SourceCh,
			DurationMs: decoded.DurationMs,
		},
		Speakers:             result.Speakers,
		CustomizationActions: customizationActions,
	}
	if h.store != nil {
		persistCtx := storageauth.ContextWithRequestOwner(ctx, r)
		audioAsset := audioAssetInput(raw, contentType, decoded.SourceFormat, decoded.DurationMs)
		var err error
		switch saver := h.store.(type) {
		case store.TranscriptionSpeakerStore:
			err = saver.SaveTranscriptionWithAudioAndSpeakers(persistCtx, result.Text, resp.Language, result.Provider, result.Model, decoded.DurationMs, latency.Milliseconds(), audioAsset, result.Speakers)
		case store.TranscriptionAudioStore:
			err = saver.SaveTranscriptionWithAudio(persistCtx, result.Text, resp.Language, result.Provider, result.Model, decoded.DurationMs, latency.Milliseconds(), audioAsset)
		default:
			err = h.store.SaveTranscription(persistCtx, result.Text, resp.Language, result.Provider, result.Model, decoded.DurationMs, latency.Milliseconds(), raw)
		}
		if err != nil {
			slog.Warn("dictation: persist transcript failed", "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) applyCustomizationHints(ctx context.Context, opts stt.TranscribeOpts) (stt.TranscribeOpts, []speechcustomize.Replacement) {
	service := internalcustomize.NewService(internalcustomize.ServiceOptions{
		Store:             h.store,
		ScopeOrder:        speechcustomize.DefaultServerScopeOrder(),
		ActiveTemplateIDs: h.activeTemplates,
	})
	resolved, err := service.Resolve(ctx, speechcustomize.Context{
		Mode:              speechcustomize.ModeDictation,
		Language:          opts.Language,
		Stage:             speechcustomize.StagePostSTT,
		ActiveTemplateIDs: h.activeTemplates,
	})
	if err != nil {
		slog.Debug("dictation: resolve customization failed", "err", err)
		return opts, nil
	}
	if resolved.Prompt != "" && !strings.Contains(opts.Prompt, resolved.Prompt) {
		if strings.TrimSpace(opts.Prompt) == "" || strings.TrimSpace(opts.Prompt) == h.defaultPrompt {
			opts.Prompt = resolved.Prompt
		} else {
			opts.Prompt = strings.TrimSpace(opts.Prompt) + "\n" + resolved.Prompt
		}
	}
	opts.Keyterms = mergeKeyterms(opts.Keyterms, resolved.Keyterms)
	return opts, resolved.Replacements
}

func (h *Handler) recordCustomizationUsage(ctx context.Context, replacements []speechcustomize.Replacement, matches []internalcustomize.MatchRecord, language string) {
	wordStore, _ := h.store.(store.WordStore)
	dictionaryStore, _ := h.store.(store.UserDictionaryStore)
	replacementStore, _ := h.store.(store.ReplacementStore)
	if wordStore == nil && dictionaryStore == nil && replacementStore == nil {
		return
	}
	replacementsByID := map[string]speechcustomize.Replacement{}
	for _, replacement := range replacements {
		replacementsByID[replacement.ID] = replacement
	}
	for _, match := range matches {
		usageCtx := ctx
		replacement := replacementsByID[match.ReplacementID]
		if replacement.Scope != nil {
			if scoped, ok := internalcustomize.StorageScopeForRef(speechstorage.ScopeFromContext(ctx), *replacement.Scope); ok {
				usageCtx = speechstorage.WithScope(ctx, scoped)
			}
		}
		if replacementStore != nil && match.ReplacementID != "" {
			if err := replacementStore.RecordReplacementUsage(usageCtx, match.ReplacementID); err != nil {
				slog.Debug("dictation: record replacement usage failed", "err", err)
			}
		}
		term := match.Term
		if strings.TrimSpace(replacement.Output.Text) != "" {
			term = replacement.Output.Text
		}
		if wordStore != nil {
			if err := wordStore.RecordWordUsage(usageCtx, term, language); err != nil {
				slog.Debug("dictation: record word usage failed", "err", err)
			}
		}
		if dictionaryStore != nil {
			if err := dictionaryStore.RecordUserDictionaryUsage(ctx, term, language); err != nil {
				slog.Debug("dictation: record dictionary usage failed", "err", err)
			}
		}
	}
}

func mergeKeyterms(existing, next []string) []string {
	if len(next) == 0 {
		return existing
	}
	merged := append([]string(nil), existing...)
	seen := map[string]struct{}{}
	for _, value := range merged {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range next {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, trimmed)
	}
	return merged
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func audioAssetInput(raw []byte, contentType, sourceFormat string, durationMs int64) store.AudioAssetInput {
	mimeType := strings.TrimSpace(contentType)
	if parsed, _, err := mime.ParseMediaType(mimeType); err == nil {
		mimeType = parsed
	}
	extension := audioExtension(mimeType, sourceFormat)
	if mimeType == "" {
		mimeType = audioMimeType(extension)
	}
	return store.AudioAssetInput{
		Data:       raw,
		MimeType:   mimeType,
		Extension:  extension,
		DurationMs: durationMs,
	}
}

func audioExtension(mimeType, sourceFormat string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0])) {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/opus":
		return ".opus"
	case "audio/webm":
		return ".webm"
	case "audio/l16", "audio/pcm":
		return ".pcm"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	}
	switch strings.ToLower(strings.TrimSpace(sourceFormat)) {
	case "mp3", "mpeg":
		return ".mp3"
	case "ogg":
		return ".ogg"
	case "opus":
		return ".opus"
	case "webm":
		return ".webm"
	case "pcm", "pcm16", "l16":
		return ".pcm"
	default:
		return ".wav"
	}
}

func audioMimeType(extension string) string {
	switch strings.ToLower(strings.TrimSpace(extension)) {
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".webm":
		return "audio/webm"
	case ".pcm":
		return "audio/L16"
	default:
		return "audio/wav"
	}
}

func resolveSpeakerOptions(primary, fallback speaker.Options) speaker.Options {
	if primary.WantsDiarization() || primary.Enabled {
		return primary.Normalized()
	}
	return fallback.Normalized()
}

func parseSpeakerOptionsFromForm(r *http.Request) speaker.Options {
	if r == nil {
		return speaker.Options{}
	}
	opts := speaker.Options{
		Enabled:             parseFormBool(r, "speaker_enabled"),
		Diarization:         parseFormBool(r, "speaker_diarization"),
		Identification:      parseFormBool(r, "speaker_identification"),
		Attribution:         parseFormBool(r, "speaker_attribution"),
		ProviderProfileID:   strings.TrimSpace(r.FormValue("speaker_provider_profile_id")),
		Model:               strings.TrimSpace(r.FormValue("speaker_model")),
		DiarizationModel:    strings.TrimSpace(r.FormValue("speaker_diarization_model")),
		SpeakerType:         strings.TrimSpace(r.FormValue("speaker_type")),
		SpeakersExpected:    parseFormInt(r, "speakers_expected"),
		MinSpeakersExpected: parseFormInt(r, "speaker_min"),
		MaxSpeakersExpected: parseFormInt(r, "speaker_max"),
		KnownValues:         splitCSV(r.FormValue("speaker_known_values")),
	}
	return opts.Normalized()
}

func parseFormBool(r *http.Request, name string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseFormInt(r *http.Request, name string) int {
	raw := strings.TrimSpace(r.FormValue(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
