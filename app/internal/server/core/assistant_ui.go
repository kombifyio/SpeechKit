//go:build linux

package core

import (
	"net/http"

	"github.com/kombifyio/SpeechKit/app/internal/config"
	"github.com/kombifyio/SpeechKit/app/internal/server/assistantui"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
)

// registerAssistantUI serves the /assistant web page — the server-hosted
// surface of the standard Voice Assistant UI module (kit element + adapter,
// self-contained HTML, hash-pinned CSP). Operators can disable it with
// SPEECHKIT_SERVER_ASSISTANT_UI=false; the default is on, matching the
// onboarding/smoke UI toggle semantics.
func registerAssistantUI(app *App) {
	if app == nil || app.Mux == nil {
		return
	}
	if !envBoolDefault(config.ServerAssistantUIEnv, true) {
		return
	}
	handler := assistantUIHandler{app: app}
	app.Mux.Handle("/assistant", handler)
	app.Mux.Handle("/assistant/", handler)
}

type assistantUIHandler struct {
	app *App
}

func (h assistantUIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !operatorUIPublic() && middleware.IdentityFromContext(r.Context()).Role == "" {
		http.Error(w, "unauthenticated", http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/assistant", "/assistant/":
		smokeToken := ""
		if h.app != nil && h.app.AuthState != nil {
			smokeToken = h.app.AuthState.SmokeToken()
		}
		writeHTML(w, r, assistantui.AssistantUIHTML(smokeToken))
	default:
		http.NotFound(w, r)
	}
}
