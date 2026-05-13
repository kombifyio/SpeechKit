package main

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/desktop/controlplane"
	"github.com/kombifyio/SpeechKit/internal/frontendassets"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// AppVersion is injected at build time via -ldflags. Defaults to "dev" for
// local development builds that skip the release toolchain.
var AppVersion = "dev"

const (
	controlPlaneTokenCookieName = controlplane.TokenCookieName
	controlPlaneTokenHeaderName = controlplane.TokenHeaderName
)

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
	return exec.Command("explorer.exe", "/select,", abs).Start() // #nosec G204 -- executable is fixed; abs is passed as an Explorer argument after .wav validation.
}

var openInstallerFileInShell = func(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("open installer: resolve path: %w", err)
	}
	if !isInstallerAssetName(abs) {
		return fmt.Errorf("open installer: only .exe or .msi files are supported")
	}
	return exec.Command(abs).Start() // #nosec G204 -- abs is restricted to installer asset extensions before launch.
}

// assetHandler builds the unified HTTP mux for the Wails control plane.
// Routes are registered by domain in dedicated routes_*.go files.
func assetHandler(cfg *config.Config, cfgPath string, state *appState, sttRouter *router.Router, feedbackStore store.Store, installState *config.InstallState) http.Handler {
	return newControlPlaneHandler(controlPlaneDeps{
		Config:        cfg,
		ConfigPath:    cfgPath,
		State:         state,
		STTRouter:     sttRouter,
		FeedbackStore: feedbackStore,
		InstallState:  installState,
	})
}

type controlPlaneDeps struct {
	Config        *config.Config
	ConfigPath    string
	State         *appState
	STTRouter     *router.Router
	FeedbackStore store.Store
	InstallState  *config.InstallState
}

func newControlPlaneHandler(deps controlPlaneDeps) http.Handler {
	mux := http.NewServeMux()
	registerOverlayRoutes(mux, deps.ConfigPath, deps.Config, deps.State)
	registerSettingsRoutes(mux, deps.ConfigPath, deps.Config, deps.State, deps.STTRouter, deps.FeedbackStore)
	registerDashboardRoutes(mux, deps.State, deps.FeedbackStore)
	registerQuickNoteRoutes(mux, deps.Config, deps.State, deps.FeedbackStore)
	registerFeatureRoutes(mux, deps.InstallState)
	registerAuthRoutes(mux)
	registerAppRoutes(mux, deps.ConfigPath, deps.State, deps.InstallState)
	registerDownloadRoutes(mux, deps.ConfigPath, deps.Config, deps.State)
	registerControlPlaneAPIRoutes(mux, deps)
	mux.Handle("/", http.FileServer(http.FS(frontendassets.Files())))
	return enforceControlPlaneRequestGuard(mux, controlPlaneTokenFromState(deps.State))
}

// enforceControlPlaneRequestGuard rejects cross-site and disallowed-origin
// mutating requests. It is the primary CSRF defence for the local control plane.
func enforceControlPlaneRequestGuard(next http.Handler, sessionToken string) http.Handler {
	return controlplane.Guard(next, sessionToken)
}

func newControlPlaneToken() string {
	return controlplane.NewToken()
}

func controlPlaneTokenFromState(state *appState) string {
	if state == nil {
		return ""
	}
	return state.controlPlaneToken
}

func hasValidControlPlaneTokenHeader(r *http.Request, expected string) bool {
	return controlplane.HasValidTokenHeader(r, expected)
}
