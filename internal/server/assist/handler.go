//go:build linux

// Package assist implements the POST /v1/assist/process handler. It accepts
// either an audio payload (→ STT → Assist pipeline) or a text transcript
// directly, runs the Framework's Assist Pipeline, and returns the result
// plus optional TTS audio as base64 in the JSON response.
//
// Host-side tool execution (clipboard, selection, quick-note) is NOT done
// server-side — the server returns an `action: "execute"` signal and the
// calling client performs the action. This keeps the Server-Target safe
// for multi-tenant deployments where host-level side effects would be
// nonsensical.
package assist

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	assistpkg "github.com/kombifyio/SpeechKit/internal/assist"
	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// sessionKeyFromRequest derives a stable v0.38.0 multi-turn session
// key from the request's Identity. The key is opaque to the
// Pipeline — it just needs to be the same value across the user's
// follow-up turns and unique across users so contexts do not bleed.
// Identity{UserID, OrgID} satisfies both constraints. When no
// Identity is on the context (e.g. auth_mode=none), we fall back to
// the per-process anonymous default which still gives each user
// their own conversation slot.
func sessionKeyFromRequest(r *http.Request) string {
	id := middleware.IdentityFromContext(r.Context())
	if id.OrgID != "" {
		return id.OrgID + "/" + id.UserID
	}
	return id.UserID
}

// Transcriber is the STT surface the handler needs when the caller sends
// audio. If nil, only text-only requests are accepted.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Processor is the Assist-pipeline surface the handler needs. The production
// implementation is `internal/assist.Pipeline`; tests supply a fake.
type Processor interface {
	Process(ctx context.Context, transcript string, opts assistpkg.ProcessOpts) (*assistpkg.Result, error)
}

// Options configures a single Handler instance.
type Options struct {
	Processor              Processor
	Transcriber            Transcriber // optional; nil disables audio input
	MaxUploadMB            int
	MaxDecodedAudioSeconds int
	DefaultLocale          string
	Store                  store.Store
	ActiveTemplateIDs      []string
}

// Handler implements the /v1/assist/process HTTP surface.
type Handler struct {
	processor       Processor
	transcriber     Transcriber
	maxBytes        int64
	decodeLimits    audio.DecodeLimits
	defaultLocale   string
	store           store.Store
	activeTemplates []string
}

// New constructs a Handler. processor must be non-nil. Transcriber is
// optional — when omitted, the handler rejects requests that carry audio
// with a 400/missing-transcriber code.
func New(opts Options) (*Handler, error) {
	if opts.Processor == nil {
		return nil, errors.New("assist: processor must not be nil")
	}
	maxBytes := int64(25) << 20
	if opts.MaxUploadMB > 0 {
		maxBytes = int64(opts.MaxUploadMB) << 20
	}
	return &Handler{
		processor:       opts.Processor,
		transcriber:     opts.Transcriber,
		maxBytes:        maxBytes,
		decodeLimits:    audio.DecodeLimits{MaxDecodedAudioSeconds: opts.MaxDecodedAudioSeconds},
		defaultLocale:   strings.TrimSpace(opts.DefaultLocale),
		store:           opts.Store,
		activeTemplates: append([]string(nil), opts.ActiveTemplateIDs...),
	}, nil
}

// Mount registers the handler at /v1/assist/process.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/assist/process", h)
	mux.HandleFunc("/v1/assist/self-test", h.ServeSelfTest)
}

// ── request / response shapes ───────────────────────────────────────────────

type processJSONRequest struct {
	Text        string `json:"text"`
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	Locale      string `json:"locale"`
	Selection   string `json:"selection"`
	Context     string `json:"context"`
	// App and WindowTitle describe the foreground application on the
	// integrating client. The server has no desktop, so the caller
	// supplies these; the kernel folds them into the LLM context block.
	App         string `json:"app"`
	WindowTitle string `json:"window_title"`
	// TTS overrides — the Pipeline already knows whether TTS is globally
	// enabled; these fields let the caller opt out per-request.
	TTS            *bool           `json:"tts,omitempty"`
	TTSFormat      string          `json:"tts_format,omitempty"`
	TTSVoice       string          `json:"tts_voice,omitempty"`
	TTSSpeed       float64         `json:"tts_speed,omitempty"`
	Speaker        speaker.Options `json:"speaker,omitempty"`
	SpeakerOptions speaker.Options `json:"speaker_options,omitempty"`
}

type processResponse struct {
	Text                 string                          `json:"text"`
	SpeakText            string                          `json:"speak_text,omitempty"`
	Action               string                          `json:"action"`
	Locale               string                          `json:"locale,omitempty"`
	Shortcut             string                          `json:"shortcut,omitempty"`
	Surface              string                          `json:"surface,omitempty"`
	Kind                 string                          `json:"kind,omitempty"`
	MessageID            localization.MessageID          `json:"message_id,omitempty"`
	ReasonCode           string                          `json:"reason_code,omitempty"`
	Transcript           string                          `json:"transcript,omitempty"`
	AudioBase64          string                          `json:"audio_base64,omitempty"`
	AudioFormat          string                          `json:"audio_format,omitempty"`
	LatencyMs            int64                           `json:"latency_ms"`
	SourceInfo           *sourceMeta                     `json:"source,omitempty"`
	Speakers             *speaker.DiarizationResult      `json:"speakers,omitempty"`
	CustomizationActions []speechkit.CustomizationAction `json:"customization_actions,omitempty"`
}

type selfTestResponse struct {
	Status    string         `json:"status"`
	Text      string         `json:"text,omitempty"`
	Action    string         `json:"action,omitempty"`
	Locale    string         `json:"locale,omitempty"`
	LatencyMs int64          `json:"latency_ms"`
	Details   map[string]any `json:"details,omitempty"`
}

type sourceMeta struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	DurationMs int64  `json:"duration_ms"`
}

func (h *Handler) ServeSelfTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only POST is accepted on this endpoint")
		return
	}

	started := time.Now()
	result, err := h.processor.Process(r.Context(), "Reply with exactly the single word: pong.", assistpkg.ProcessOpts{Locale: "en"})
	latency := time.Since(started)
	if err != nil {
		slog.Warn("assist: self-test failed", "err", err)
		writePipelineError(w, err, latency)
		return
	}
	if result == nil || strings.TrimSpace(result.Text) == "" {
		httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", "Assist self-test returned an empty result", map[string]any{
			"stage":      "llm",
			"category":   "empty_result",
			"retryable":  true,
			"latency_ms": latency.Milliseconds(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(selfTestResponse{
		Status:    "ok",
		Text:      result.Text,
		Action:    result.Action,
		Locale:    result.Locale,
		LatencyMs: latency.Milliseconds(),
	})
}

// ── ServeHTTP ───────────────────────────────────────────────────────────────

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

	if err := r.ParseMultipartForm(32 << 10); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds maximum %d bytes", h.maxBytes))
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_multipart", "failed to parse multipart body: "+err.Error())
		return
	}

	locale := strings.TrimSpace(r.FormValue("locale"))
	selection := r.FormValue("selection")
	contextStr := r.FormValue("context")
	app := r.FormValue("app")
	windowTitle := r.FormValue("window_title")
	ttsOverride, ttsFormat, ttsVoice := parseTTSOverrides(r.FormValue("tts"), r.FormValue("tts_format"), r.FormValue("tts_voice"))
	speakerOpts := parseSpeakerOptionsFromForm(r)

	text := strings.TrimSpace(r.FormValue("text"))
	file, header, fileErr := r.FormFile("audio")

	switch {
	case text != "" && fileErr != nil:
		// Text-only path.
		h.processTranscript(r.Context(), w, text, nil, nil, assistpkg.ProcessOpts{
			Locale:      h.resolveLocale(locale),
			Selection:   selection,
			Context:     contextStr,
			ActiveApp:   app,
			WindowTitle: windowTitle,
			SessionKey:  sessionKeyFromRequest(r),
		}, ttsOverride, ttsFormat, ttsVoice)
		return
	case fileErr == nil:
		defer func() { _ = file.Close() }()
		if header.Size > 0 && header.Size > h.maxBytes {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("audio file %d bytes exceeds maximum %d bytes", header.Size, h.maxBytes))
			return
		}
		h.processAudio(r.Context(), w, file, header.Header.Get("Content-Type"), assistpkg.ProcessOpts{
			// Deliberately NOT resolved here: on the audio path the locale
			// is handed to STT as a request-level override, so filling in
			// the server default would pin every transcription to it. The
			// default is applied after transcription instead, once the
			// provider has had its chance to report a language.
			Locale:      locale,
			Selection:   selection,
			Context:     contextStr,
			ActiveApp:   app,
			WindowTitle: windowTitle,
			SessionKey:  sessionKeyFromRequest(r),
		}, speakerOpts, ttsOverride, ttsFormat, ttsVoice)
		return
	default:
		httpx.WriteError(w, http.StatusBadRequest, "missing_input",
			"multipart body must include either an 'audio' file part or a 'text' form field")
	}
}

func (h *Handler) handleJSON(w http.ResponseWriter, r *http.Request) {
	bodyReader := http.MaxBytesReader(w, r.Body, h.maxBytes)
	defer func() { _ = r.Body.Close() }()

	var body processJSONRequest
	if err := json.NewDecoder(bodyReader).Decode(&body); err != nil {
		if errors.As(err, new(*http.MaxBytesError)) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("request body exceeds maximum %d bytes", h.maxBytes))
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json",
			"failed to decode request JSON: "+err.Error())
		return
	}

	opts := assistpkg.ProcessOpts{
		// Left unresolved on purpose. The audio branch hands this to STT as
		// a request-level override, where the server default would pin every
		// transcription; the text branch has no STT to learn from and
		// resolves it below.
		Locale:      body.Locale,
		Selection:   body.Selection,
		Context:     body.Context,
		ActiveApp:   body.App,
		WindowTitle: body.WindowTitle,
		SessionKey:  sessionKeyFromRequest(r),
	}

	text := strings.TrimSpace(body.Text)
	switch {
	case text != "":
		opts.Locale = h.resolveLocale(opts.Locale)
		h.processTranscript(r.Context(), w, text, nil, nil, opts, body.TTS, body.TTSFormat, body.TTSVoice)
	case strings.TrimSpace(body.AudioBase64) != "":
		raw, err := base64.StdEncoding.DecodeString(body.AudioBase64)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_base64",
				"audio_base64 is not valid base64: "+err.Error())
			return
		}
		formatHint := audioFormatHint(body.Format)
		h.processAudioBytes(r.Context(), w, raw, formatHint, opts, resolveSpeakerOptions(body.Speaker, body.SpeakerOptions), body.TTS, body.TTSFormat, body.TTSVoice)
	default:
		httpx.WriteError(w, http.StatusBadRequest, "missing_input",
			"JSON body must include either 'text' or 'audio_base64'")
	}
}

// ── core flow ───────────────────────────────────────────────────────────────

func (h *Handler) processAudio(ctx context.Context, w http.ResponseWriter, reader io.Reader, contentType string, opts assistpkg.ProcessOpts, speakerOpts speaker.Options, ttsOverride *bool, ttsFormat, ttsVoice string) {
	lr := io.LimitReader(reader, h.maxBytes+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "read_failed", "failed to read audio: "+err.Error())
		return
	}
	if int64(len(raw)) > h.maxBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio exceeds maximum %d bytes", h.maxBytes))
		return
	}
	h.processAudioBytes(ctx, w, raw, contentType, opts, speakerOpts, ttsOverride, ttsFormat, ttsVoice)
}

func (h *Handler) processAudioBytes(ctx context.Context, w http.ResponseWriter, raw []byte, contentType string, opts assistpkg.ProcessOpts, speakerOpts speaker.Options, ttsOverride *bool, ttsFormat, ttsVoice string) {
	if h.transcriber == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "stt_unavailable",
			"this deployment does not expose an STT router; send 'text' instead of 'audio'")
		return
	}

	decoded, err := audio.DecodeWithLimits(ctx, raw, contentType, h.decodeLimits)
	if err != nil {
		if errors.Is(err, audio.ErrUnsupportedFormat) {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_audio_format", err.Error())
			return
		}
		if errors.Is(err, audio.ErrDecodedAudioTooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", err.Error())
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid_audio", "failed to decode audio: "+err.Error())
		return
	}
	if len(decoded.PCM) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "empty_audio", "decoded audio contains no samples")
		return
	}

	durationSecs := float64(decoded.DurationMs) / 1000.0
	sttOpts, _ := h.applyCustomizationHints(ctx, stt.TranscribeOpts{Language: opts.Locale, Speaker: speakerOpts})
	sttResult, err := h.transcriber.Route(ctx, decoded.PCM, durationSecs, sttOpts)
	if err != nil {
		slog.Warn("assist: STT failed", "err", err, "duration_ms", decoded.DurationMs) // #nosec G706 -- slog writes structured fields; provider errors are attributes, not message format.
		httpx.WriteError(w, http.StatusServiceUnavailable, "stt_provider_unavailable",
			"no STT provider could satisfy the request: "+err.Error())
		return
	}
	if sttResult == nil || strings.TrimSpace(sttResult.Text) == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "empty_transcript",
			"STT returned an empty transcript; the audio may contain no speech")
		return
	}

	if opts.Locale == "" && isConcreteLocale(sttResult.Language) {
		opts.Locale = sttResult.Language
	}
	// Now that STT has had its say, fall back to the server default so the
	// reply and any TTS still have a locale to work with.
	opts.Locale = h.resolveLocale(opts.Locale)
	if sttResult.Speakers != nil {
		opts.Context = appendSpeakerContext(opts.Context, sttResult.Speakers)
	}
	source := &sourceMeta{
		Format:     decoded.SourceFormat,
		SampleRate: decoded.SourceRate,
		Channels:   decoded.SourceCh,
		DurationMs: decoded.DurationMs,
	}
	h.processTranscript(ctx, w, sttResult.Text, source, sttResult.Speakers, opts, ttsOverride, ttsFormat, ttsVoice)
}

func (h *Handler) processTranscript(ctx context.Context, w http.ResponseWriter, transcript string, source *sourceMeta, speakers *speaker.DiarizationResult, opts assistpkg.ProcessOpts, ttsOverride *bool, ttsFormat, ttsVoice string) {
	if strings.TrimSpace(transcript) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "empty_transcript", "transcript or text field must not be empty")
		return
	}
	// Resolve the user's Words/Replacements customization once and use it for
	// both the post-STT transcript replacement and the Assist-LLM vocabulary
	// hint. VoiceAgentHint is the generation-oriented, mode-neutral phrasing
	// ("Prefer these names and product terms …") that the kernel folds into
	// the context block. Resolving once keeps the two consistent — a second
	// resolve can observe a different store state mid-request — and avoids
	// duplicating the store and template work on every Assist call.
	var customizationActions []speechkit.CustomizationAction
	if resolved, err := h.resolveAssistCustomization(ctx, opts.Locale); err != nil {
		slog.Debug("assist: resolve customization failed", "err", err)
	} else {
		transcript, customizationActions = h.applyResolvedTranscriptCustomization(ctx, resolved, transcript, opts.Locale)
		opts.VocabularyHint = resolved.VoiceAgentHint
	}

	started := time.Now()
	result, err := h.processor.Process(ctx, transcript, opts)
	latency := time.Since(started)
	if err != nil {
		slog.Warn("assist: pipeline error", "err", err, "transcript_chars", len(transcript)) // #nosec G706 -- slog writes structured fields; provider errors are attributes, not message format.
		writePipelineError(w, err, latency)
		return
	}
	if result == nil {
		httpx.WriteError(w, http.StatusInternalServerError, "empty_result",
			"Assist pipeline returned nil result")
		return
	}
	if strings.TrimSpace(result.Text) == "" {
		httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", "Assist pipeline returned an empty result", map[string]any{
			"stage":      "llm",
			"category":   "empty_result",
			"retryable":  true,
			"latency_ms": latency.Milliseconds(),
		})
		return
	}

	// Respect TTS opt-out from the request. We cannot opt IN when the
	// pipeline was built without a TTS router — Audio will already be empty
	// in that case.
	if ttsOverride != nil && !*ttsOverride {
		result.Audio = nil
		result.Format = ""
	}
	_ = ttsFormat // reserved for post-synthesis transcoding (v1.1)
	_ = ttsVoice  // reserved for per-request voice override (v1.1)

	resp := processResponse{
		Text:                 result.Text,
		SpeakText:            result.SpeakText,
		Action:               result.Action,
		Locale:               firstNonEmptyString(result.Locale, opts.Locale),
		Shortcut:             result.Shortcut,
		Surface:              string(result.Surface),
		Kind:                 string(result.Kind),
		MessageID:            result.MessageID,
		ReasonCode:           result.ReasonCode,
		Transcript:           transcript,
		LatencyMs:            latency.Milliseconds(),
		SourceInfo:           source,
		AudioFormat:          result.Format,
		Speakers:             speakers,
		CustomizationActions: customizationActions,
	}
	if len(result.Audio) > 0 {
		resp.AudioBase64 = base64.StdEncoding.EncodeToString(result.Audio)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// ── helpers ─────────────────────────────────────────────────────────────────

// isConcreteLocale reports whether a language reported by an STT provider
// names an actual locale. Providers echo routing pseudo-values back on the
// result — Deepgram returns "multi" for multilingual code-switching — and
// those must not become the reply/TTS locale.
func isConcreteLocale(language string) bool {
	language = strings.TrimSpace(language)
	if language == "" {
		return false
	}
	switch strings.ToLower(language) {
	case "multi", "auto":
		return false
	}
	return true
}

func (h *Handler) resolveLocale(requested string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	return h.defaultLocale
}

func (h *Handler) resolveAssistCustomization(ctx context.Context, locale string) (internalcustomize.ResolvedSet, error) {
	service := internalcustomize.NewService(internalcustomize.ServiceOptions{
		Store:             h.store,
		ScopeOrder:        speechcustomize.DefaultServerScopeOrder(),
		ActiveTemplateIDs: h.activeTemplates,
	})
	return service.Resolve(ctx, speechcustomize.Context{
		Mode:              speechcustomize.ModeAssist,
		Language:          locale,
		Stage:             speechcustomize.StagePostSTT,
		ActiveTemplateIDs: h.activeTemplates,
	})
}

func (h *Handler) applyCustomizationHints(ctx context.Context, opts stt.TranscribeOpts) (stt.TranscribeOpts, []speechcustomize.Replacement) {
	resolved, err := h.resolveAssistCustomization(ctx, opts.Language)
	if err != nil {
		slog.Debug("assist: resolve customization failed", "err", err)
		return opts, nil
	}
	if resolved.Prompt != "" && !strings.Contains(opts.Prompt, resolved.Prompt) {
		if strings.TrimSpace(opts.Prompt) == "" {
			opts.Prompt = resolved.Prompt
		} else {
			opts.Prompt = strings.TrimSpace(opts.Prompt) + "\n" + resolved.Prompt
		}
	}
	opts.Keyterms = mergeKeyterms(opts.Keyterms, resolved.Keyterms)
	return opts, resolved.Replacements
}

// applyResolvedTranscriptCustomization applies the post-STT replacement rules
// from an already-resolved customization set, recording usage for any matches.
// Callers resolve once (see processTranscript) so the transcript replacement
// and the LLM vocabulary hint always come from the same snapshot.
func (h *Handler) applyResolvedTranscriptCustomization(ctx context.Context, resolved internalcustomize.ResolvedSet, transcript, locale string) (string, []speechkit.CustomizationAction) {
	if resolved.Applier == nil {
		return transcript, nil
	}
	applied := resolved.Applier.Apply(transcript)
	if len(applied.Matches) > 0 {
		h.recordCustomizationUsage(ctx, resolved.Replacements, applied.Matches, locale)
	}
	return applied.Text, internalcustomize.PublicActions(applied.Actions)
}

func (h *Handler) recordCustomizationUsage(ctx context.Context, replacements []speechcustomize.Replacement, matches []internalcustomize.MatchRecord, language string) {
	if h.store == nil || len(matches) == 0 {
		return
	}
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
	baseScope := speechstorage.ScopeFromContext(ctx)
	for _, match := range matches {
		usageCtx := ctx
		replacement := replacementsByID[match.ReplacementID]
		if replacement.Scope != nil {
			if scoped, ok := internalcustomize.StorageScopeForRef(baseScope, *replacement.Scope); ok {
				usageCtx = speechstorage.WithScope(ctx, scoped)
			}
		}
		if replacementStore != nil && match.ReplacementID != "" {
			if err := replacementStore.RecordReplacementUsage(usageCtx, match.ReplacementID); err != nil {
				slog.Debug("assist: record replacement usage failed", "err", err)
			}
		}
		term := match.Term
		if strings.TrimSpace(replacement.Output.Text) != "" {
			term = replacement.Output.Text
		}
		if wordStore != nil {
			if err := wordStore.RecordWordUsage(usageCtx, term, language); err != nil {
				slog.Debug("assist: record word usage failed", "err", err)
			}
		}
		if dictionaryStore != nil {
			if err := dictionaryStore.RecordUserDictionaryUsage(ctx, term, language); err != nil {
				slog.Debug("assist: record dictionary usage failed", "err", err)
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
		key := strings.ToLower(strings.TrimSpace(value))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, strings.TrimSpace(value))
	}
	return merged
}

func audioFormatHint(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mpeg"
	case "pcm16", "pcm":
		return "audio/L16;rate=16000;channels=1"
	default:
		return ""
	}
}

// parseTTSOverrides interprets multipart form fields. "tts" is parsed as a
// permissive boolean; other fields pass through verbatim.
func parseTTSOverrides(ttsRaw, ttsFormat, ttsVoice string) (override *bool, format, voice string) {
	if trimmed := strings.TrimSpace(ttsRaw); trimmed != "" {
		switch strings.ToLower(trimmed) {
		case "1", "true", "yes", "on":
			t := true
			override = &t
		case "0", "false", "no", "off":
			f := false
			override = &f
		}
	}
	return override, strings.TrimSpace(ttsFormat), strings.TrimSpace(ttsVoice)
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func writePipelineError(w http.ResponseWriter, err error, latency time.Duration) {
	classification := classifyPipelineError(err)
	classification["latency_ms"] = latency.Milliseconds()
	message := "Assist pipeline failed"
	if stage, _ := classification["stage"].(string); stage != "" {
		message += " during " + stage
	}
	if category, _ := classification["category"].(string); category != "" {
		message += ": " + category
	}
	httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", message, classification)
}

func classifyPipelineError(err error) map[string]any {
	details := map[string]any{
		"stage":     "pipeline",
		"category":  "runtime_error",
		"retryable": true,
	}
	if err == nil {
		return details
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "llm failed") || strings.Contains(lower, "no llm flow") || strings.Contains(lower, "no models configured") {
		details["stage"] = "llm"
	}
	if strings.Contains(msg, "Invalid configuration type") || strings.Contains(msg, "GenerateContentConfig") || strings.Contains(msg, "GenerationCommonConfig") {
		details["stage"] = "llm"
		details["category"] = "provider_config"
		details["retryable"] = false
		details["provider"] = "googleai"
		return details
	}
	if strings.Contains(lower, "no llm flow") || strings.Contains(lower, "no models configured") {
		details["category"] = "missing_model"
		details["retryable"] = false
		return details
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "429") {
		details["category"] = "rate_limited"
		return details
	}
	if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "401") || strings.Contains(lower, "403") {
		details["category"] = "credentials"
		details["retryable"] = false
		return details
	}
	return details
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

func appendSpeakerContext(existing string, result *speaker.DiarizationResult) string {
	if result == nil || len(result.Segments) == 0 {
		return existing
	}
	var b strings.Builder
	if strings.TrimSpace(existing) != "" {
		b.WriteString(strings.TrimSpace(existing))
		b.WriteString("\n\n")
	}
	b.WriteString("Speaker transcript:\n")
	for _, segment := range result.Segments {
		label := firstNonEmptyString(segment.DisplayName, segment.Role, segment.SpeakerLabel)
		if label == "" {
			label = "speaker"
		}
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		b.WriteString("[")
		b.WriteString(label)
		b.WriteString("] ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
