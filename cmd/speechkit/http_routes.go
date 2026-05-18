package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/desktop/controlplane"
	"github.com/kombifyio/SpeechKit/internal/frontendassets"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// redactIP removes the last octet of an IPv4 address (or the full IPv6
// address) before it is written to the audit log. Loopback addresses are
// returned as-is because they carry no personally-identifiable information.
func redactIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() {
		return host
	}
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%d.%d.%d.x", ip4[0], ip4[1], ip4[2])
	}
	return "[ipv6-redacted]"
}

// auditTokenRejection wraps the control-plane Guard and emits an auth.failed
// audit event whenever the guard rejects a request with 403 Forbidden due to
// an invalid or missing session token.
func auditTokenRejection(next http.Handler, sessionToken string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only bother recording when a token is configured and the request is
		// mutating — the guard only rejects on mutating methods.
		if sessionToken == "" || !controlplane.IsMutatingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if controlplane.HasValidTokenHeader(r, sessionToken) {
			// Fast path: token is valid, skip the recorder overhead.
			next.ServeHTTP(w, r)
			return
		}

		// Capture the response to detect a 403 from a missing/invalid token.
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)

		// Copy the captured response to the real writer.
		for k, vs := range rec.Header() {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())

		if rec.Code == http.StatusForbidden {
			_ = auditlog.AppendEvent(r.Context(), auditlog.Record{
				Event: auditlog.EventAuthFailed,
				Resource: map[string]any{
					"source_ip_redacted": redactIP(r.RemoteAddr),
					"endpoint":           r.URL.Path,
				},
				Outcome: auditlog.OutcomeFailure,
			})
		}
	})
}

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
	registerAppRoutes(mux, deps.ConfigPath, deps.Config, deps.State, deps.InstallState)
	registerDownloadRoutes(mux, deps.ConfigPath, deps.Config, deps.State)
	registerControlPlaneAPIRoutes(mux, deps)
	mux.Handle("/", http.FileServer(http.FS(frontendassets.Files())))
	return enforceControlPlaneRequestGuard(mux, controlPlaneTokenFromState(deps.State))
}

// enforceControlPlaneRequestGuard rejects cross-site and disallowed-origin
// mutating requests. It is the primary CSRF defence for the local control plane.
// An audit.failed event is emitted whenever a request is rejected due to a
// missing or invalid session token.
func enforceControlPlaneRequestGuard(next http.Handler, sessionToken string) http.Handler {
	return auditTokenRejection(controlplane.Guard(next, sessionToken), sessionToken)
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
