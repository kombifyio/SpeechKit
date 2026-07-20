//go:build linux

package core

import (
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
)

func registerAPIAlias(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	handler := func(w http.ResponseWriter, r *http.Request) {
		targetPath := strings.TrimPrefix(r.URL.Path, "/api")
		if !strings.HasPrefix(targetPath, "/v1") {
			http.NotFound(w, r)
			return
		}
		// The device-agent bridge is a physically local, separately paired
		// surface. Never project it onto the general/public API alias.
		if targetPath == "/v1/device-agent" || strings.HasPrefix(targetPath, "/v1/device-agent/") {
			http.NotFound(w, r)
			return
		}

		cloned := r.Clone(r.Context())
		cloned.URL.Path = targetPath
		cloned.URL.RawPath = ""
		cloned.RequestURI = ""
		cloned.Header = r.Header.Clone()
		cloned.Header.Set(httpx.APIPrefixHeader, "/api")
		mux.ServeHTTP(w, cloned)
	}
	mux.HandleFunc("/api/v1", handler)
	mux.HandleFunc("/api/v1/", handler)
}
