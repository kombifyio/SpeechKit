//go:build linux

package catalog

import (
	"encoding/json"
	"net/http"
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
)

type Handler struct {
	cfg          *config.Config
	healthStatus func(component string) string
	version      string
}

func New(cfg *config.Config, healthStatus func(component string) string, version string) *Handler {
	return &Handler{cfg: cfg, healthStatus: healthStatus, version: version}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/catalog/profiles", h.profiles)
	mux.HandleFunc("/v1/catalog/providers", h.providers)
	mux.HandleFunc("/v1/catalog/contracts", h.contracts)
	mux.HandleFunc("/v1/catalog/readiness", h.readiness)
	mux.HandleFunc("/v1/catalog/profiles/", h.profileReadiness)
}

func (h *Handler) profiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	profiles := framework.DefaultProviderProfiles()
	if rawModes, ok := r.URL.Query()["mode"]; ok {
		if len(rawModes) != 1 || strings.TrimSpace(rawModes[0]) == "" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_mode", "mode must be one of dictation, assist, voiceagent, or voice_agent")
			return
		}
		rawMode := strings.TrimSpace(rawModes[0])
		mode := normalizeMode(rawMode)
		if mode == framework.ModeNone {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_mode", "mode must be one of dictation, assist, voiceagent, or voice_agent")
			return
		}
		profiles = framework.ProfilesForMode(mode)
	}
	writeOKJSON(w, map[string]any{"profiles": profiles})
}

func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeOKJSON(w, map[string]any{
		"provider_matrix":   framework.DefaultProviderMatrix(),
		"provider_defaults": framework.DefaultProviderDefaults(),
	})
}

func (h *Handler) contracts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeOKJSON(w, map[string]any{"contracts": framework.DefaultModeContracts()})
}

func (h *Handler) readiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	readiness := make([]framework.Readiness, 0)
	for _, profile := range framework.DefaultProviderProfiles() {
		readiness = append(readiness, h.profileReadinessSummary(profile))
	}
	writeOKJSON(w, map[string]any{"readiness": readiness})
}

func (h *Handler) profileReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !strings.HasSuffix(r.URL.Path, "/readiness") {
		httpx.WriteError(w, http.StatusNotFound, "profile_not_found", "unknown provider profile subresource")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/catalog/profiles/")
	id = strings.TrimSuffix(id, "/readiness")
	id = strings.Trim(strings.TrimSpace(id), "/")
	if id == "" || strings.Contains(id, "/") {
		httpx.WriteError(w, http.StatusNotFound, "profile_not_found", "profile id missing")
		return
	}
	id = framework.NormalizeProviderProfileID(id)
	for _, profile := range framework.DefaultProviderProfiles() {
		if profile.ID == id {
			writeOKJSON(w, h.profileReadinessSummary(profile))
			return
		}
	}
	httpx.WriteError(w, http.StatusNotFound, "profile_not_found", "unknown provider profile")
}

func (h *Handler) profileReadinessSummary(profile framework.ProviderProfile) framework.Readiness {
	modeEnabled := h.modeEnabled(profile.Mode)
	providerEnabled := h.providerEnabled(profile)
	credentialsReady := h.credentialsReady(profile)
	runtimeReady := h.runtimeReady(profile)
	capabilityReady := framework.ValidateProfileForMode(profile, profile.Mode) == nil
	configured := providerEnabled && credentialsReady
	return framework.Readiness{
		SchemaVersion:    framework.ReadinessSchemaVersion,
		ProfileID:        profile.ID,
		Mode:             framework.NormalizeMode(profile.Mode),
		ProviderKind:     profile.ProviderKind,
		ExecutionMode:    profile.ExecutionMode,
		ModelID:          profile.ModelID,
		Source:           profile.Source,
		Active:           modeEnabled && providerEnabled && h.profileSelected(profile),
		Default:          profile.Default,
		Configured:       configured,
		CredentialsReady: credentialsReady,
		RuntimeReady:     runtimeReady,
		CapabilityReady:  capabilityReady,
		Ready:            configured && runtimeReady && capabilityReady,
		Missing:          missing(modeEnabled, providerEnabled, credentialsReady, runtimeReady),
	}
}

func (h *Handler) modeEnabled(mode framework.Mode) bool {
	mode = normalizeMode(string(mode))
	if h.cfg == nil || len(h.cfg.Server.Modes) == 0 {
		return true
	}
	// TTS is a capability-mode, not a runtime-mode. It is implicitly
	// enabled whenever the host has any product mode listed in
	// Server.Modes — operators don't have to add "tts" to the modes
	// flag the way they do for dictation/assist/voice-agent.
	if mode == framework.ModeTTS {
		return true
	}
	for _, configured := range h.cfg.Server.Modes {
		if normalizeMode(configured) == mode {
			return true
		}
	}
	return false
}

func (h *Handler) profileSelected(profile framework.ProviderProfile) bool {
	selected := h.selectedProfiles(profile.Mode)
	if len(selected) == 0 {
		return profile.Default
	}
	return selected[profile.ID]
}

func (h *Handler) selectedProfiles(mode framework.Mode) map[string]bool {
	if h.cfg == nil {
		return nil
	}
	var primary, fallback string
	switch normalizeMode(string(mode)) {
	case framework.ModeDictation:
		primary = h.cfg.ModelSelection.Dictate.PrimaryProfileID
		fallback = h.cfg.ModelSelection.Dictate.FallbackProfileID
	case framework.ModeAssist:
		primary = h.cfg.ModelSelection.Assist.PrimaryProfileID
		fallback = h.cfg.ModelSelection.Assist.FallbackProfileID
	case framework.ModeVoiceAgent:
		primary = h.cfg.ModelSelection.VoiceAgent.PrimaryProfileID
		fallback = h.cfg.ModelSelection.VoiceAgent.FallbackProfileID
	case framework.ModeTTS:
		primary = h.cfg.ModelSelection.TTS.PrimaryProfileID
		fallback = h.cfg.ModelSelection.TTS.FallbackProfileID
	case framework.ModeNone:
		// No mode-specific selection; primary/fallback stay empty.
	}
	out := map[string]bool{}
	for _, id := range []string{primary, fallback} {
		id = strings.TrimSpace(id)
		if id != "" {
			out[id] = true
		}
	}
	return out
}

func (h *Handler) providerEnabled(profile framework.ProviderProfile) bool {
	return config.ProviderEnabledForProfile(h.cfg, profile)
}

func (h *Handler) credentialsReady(profile framework.ProviderProfile) bool {
	if h.cfg == nil {
		return true
	}
	return config.ProviderCredentialAvailableForProfile(h.cfg, profile)
}

func (h *Handler) runtimeReady(profile framework.ProviderProfile) bool {
	if !h.modeEnabled(profile.Mode) {
		return false
	}
	switch framework.NormalizeMode(profile.Mode) {
	case framework.ModeDictation:
		return h.statusOK("mode.dictation")
	case framework.ModeAssist:
		return h.statusOK("mode.assist")
	case framework.ModeVoiceAgent:
		return h.statusOK("mode.voiceagent")
	case framework.ModeTTS:
		// TTS doesn't have a standalone server-mode endpoint — it's a
		// capability consumed by Assist + VoiceAgent. Tie readiness to
		// the global TTS feature flag instead.
		if h.cfg == nil {
			return false
		}
		return h.cfg.TTS.Enabled
	default:
		return false
	}
}

func (h *Handler) statusOK(component string) bool {
	if h.healthStatus == nil {
		return false
	}
	return h.healthStatus(component) == "ok"
}

func missing(modeEnabled, providerEnabled, credentialsReady, runtimeReady bool) []string {
	var out []string
	if !modeEnabled {
		out = append(out, "mode_disabled")
	}
	if !providerEnabled {
		out = append(out, "provider_disabled")
	}
	if providerEnabled && !credentialsReady {
		out = append(out, "credentials")
	}
	if modeEnabled && providerEnabled && !runtimeReady {
		out = append(out, "runtime")
	}
	return out
}

func normalizeMode(raw string) framework.Mode {
	if strings.EqualFold(strings.TrimSpace(raw), "voiceagent") {
		return framework.ModeVoiceAgent
	}
	return framework.NormalizeMode(framework.Mode(raw))
}

func writeOKJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func methodNotAllowed(w http.ResponseWriter, allowed ...string) {
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this resource")
}
