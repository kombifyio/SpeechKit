//go:build linux

package core

import (
	"bytes"
	"net/http"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/assistantui"
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
	switch r.URL.Path {
	case "/assistant", "/assistant/":
		smokeToken := ""
		if h.app != nil && h.app.AuthState != nil {
			smokeToken = h.app.AuthState.SmokeToken()
		}
		writeHTML(w, r, assistantui.AssistantUIHTML(smokeToken))
	case "/assistant/marks/rosette.png":
		serveAssistantMark(w, r, assistantui.RosettePNG())
	case "/assistant/marks/k.png":
		serveAssistantMark(w, r, assistantui.MonogramPNG())
	default:
		http.NotFound(w, r)
	}
}

func serveAssistantMark(w http.ResponseWriter, r *http.Request, data []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}
