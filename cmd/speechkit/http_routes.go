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

// AppVersion is injected at build time via -ldflags. Defaults to "dev" for
// local development builds that skip the release toolchain.
var AppVersion = "dev"

// maxControlPlaneBodySize limits the request body for mutating control-plane
// requests. All POST data is small form fields; 1 MB is generous headroom.
const maxControlPlaneBodySize = 1 << 20 // 1 MB

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
	return exec.Command("explorer.exe", "/select,", abs).Start() //nolint:gosec // subprocess path is application-controlled, not user input
}

var openInstallerFileInShell = func(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("open installer: resolve path: %w", err)
	}
	if !isInstallerAssetName(abs) {
		return fmt.Errorf("open installer: only .exe or .msi files are supported")
	}
	return exec.Command(abs).Start() //nolint:gosec // subprocess path is application-controlled, not user input
}

// assetHandler builds the unified HTTP mux for the Wails control plane.
// Routes are registered by domain in dedicated routes_*.go files.
func assetHandler(cfg *config.Config, cfgPath string, state *appState, sttRouter *router.Router, feedbackStore store.Store, installState *config.InstallState) http.Handler {
	mux := http.NewServeMux()
	registerOverlayRoutes(mux, cfgPath, cfg, state)
	registerSettingsRoutes(mux, cfgPath, cfg, state, sttRouter)
	registerDashboardRoutes(mux, state, feedbackStore)
	registerQuickNoteRoutes(mux, cfg, state, feedbackStore)
	registerFeatureRoutes(mux, installState)
	registerAuthRoutes(mux)
	registerAppRoutes(mux, cfgPath, state, installState)
	registerDownloadRoutes(mux, cfgPath, cfg, state)
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

		// Limit request body size for mutating requests (defence in depth).
		r.Body = http.MaxBytesReader(w, r.Body, maxControlPlaneBodySize)

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
