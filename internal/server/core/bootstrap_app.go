//go:build linux

package core

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
)

func newServerApp(cfg *config.Config, opts RunOptions) *App {
	return &App{
		Cfg:     cfg,
		Mux:     http.NewServeMux(),
		Health:  NewHealthRegistry(),
		Modes:   resolveModes(cfg.Server.Modes),
		Version: opts.Version,
	}
}

func registerCoreEndpoints(app *App) {
	registerHealth(app)
	registerTestUI(app)
	registerAssistantUI(app)
	registerServerSettings(app)
	registerDeploymentStatus(app)
	registerPprof(app)
	registerAPIAlias(app.Mux)
}

// resolveModes turns the toml "modes" list into a lookup map. Empty input means
// all modes are enabled, matching the documented default.
func resolveModes(configured []string) map[Mode]bool {
	result := map[Mode]bool{
		ModeDictation:  false,
		ModeAssist:     false,
		ModeVoiceAgent: false,
	}
	if len(configured) == 0 {
		for k := range result {
			result[k] = true
		}
		return result
	}
	for _, raw := range configured {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case string(ModeDictation):
			result[ModeDictation] = true
		case string(ModeAssist):
			result[ModeAssist] = true
		case string(ModeVoiceAgent):
			result[ModeVoiceAgent] = true
		}
	}
	return result
}

// ModeEnabled reports whether the given mode is active for this process.
func (a *App) ModeEnabled(m Mode) bool {
	if a == nil || a.Modes == nil {
		return false
	}
	return a.Modes[m]
}

// needsSTT reports whether any enabled mode depends on the STT router.
// Dictation always needs it; Assist and Cascaded-VoiceAgent use it for the
// STT stage of their pipelines; native realtime Voice Agent providers perform
// speech handling in their own sessions.
func needsSTT(modes map[Mode]bool) bool {
	return modes[ModeDictation] || modes[ModeAssist] || modes[ModeVoiceAgent]
}

func firstVANonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func mountModeDisabled(mux *http.ServeMux, mode Mode, patterns ...string) {
	if mux == nil {
		return
	}
	for _, pattern := range patterns {
		pattern := strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			httpx.WriteModeDisabled(w, string(mode))
		})
	}
}
