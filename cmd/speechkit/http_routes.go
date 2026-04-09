package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/frontendassets"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// AppVersion is the current release version. Updated at release time.
var AppVersion = "0.14.9"

// revealAudioFileInShell opens the containing folder in Explorer and selects
// the file. Only .wav files are accepted to prevent path-traversal abuse.
var revealAudioFileInShell = func(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("reveal: resolve path: %w", err)
	}
	if ext := strings.ToLower(filepath.Ext(abs)); ext != ".wav" {
		return fmt.Errorf("reveal: only .wav files are supported (got %q)", ext)
	}
	return exec.Command("explorer.exe", "/select,", abs).Start()
}

// assetHandler builds the unified HTTP mux for the Wails control plane.
// Routes are registered by domain in dedicated routes_*.go files.
func assetHandler(cfg *config.Config, cfgPath string, state *appState, sttRouter *router.Router, feedbackStore store.Store, installState *config.InstallState) http.Handler {
	mux := http.NewServeMux()
	registerOverlayRoutes(mux, cfg, state)
	registerSettingsRoutes(mux, cfgPath, cfg, state, sttRouter)
	registerDashboardRoutes(mux, state, feedbackStore)
	registerQuickNoteRoutes(mux, cfg, state, feedbackStore)
	registerFeatureRoutes(mux, installState)
	registerAuthRoutes(mux)
	registerAppRoutes(mux, installState)
	mux.Handle("/", http.FileServer(http.FS(frontendassets.Files())))
	return enforceControlPlaneRequestGuard(mux)
}

// enforceControlPlaneRequestGuard rejects cross-site and disallowed-origin
// mutating requests. It is the primary CSRF defence for the local control plane.
func enforceControlPlaneRequestGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutatingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
			http.Error(w, "cross-site requests are not allowed", http.StatusForbidden)
			return
		}

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && !isAllowedControlPlaneOrigin(origin) {
			http.Error(w, "origin is not allowed", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isAllowedControlPlaneOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if host == "wails.localhost" || strings.HasSuffix(host, ".wails.localhost") {
		return true
	}
	return false
}
