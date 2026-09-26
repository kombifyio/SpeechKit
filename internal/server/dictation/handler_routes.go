//go:build linux

package dictation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

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
		provider := catalog.NormalizeProviderID(pref)
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
	for _, profile := range catalog.ProfilesForMode(speechkit.ModeDictation) {
		if profile.ID == profileID {
			return true
		}
	}
	return false
}
