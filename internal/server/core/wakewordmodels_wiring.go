//go:build linux

package core

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/wakewordmodels"
)

// wireWakewordModels mounts the public wake-word model catalog endpoints
// (GET /v1/wakeword/models*). Always mounted so a device can probe the surface;
// when [server.features].wakeword_models is false every request returns 503 so
// the device can detect the operator disabled it.
//
// The routes are PUBLIC (a low-power ESPHome/box device holds no bearer) — see
// serverPublicRoutes in testui.go. Payloads are already-public model metadata
// and redirects to already-public files, so exposing them leaks no secret. The
// authenticated activation-collector (wireWakewordTraining) is a separate,
// still-authenticated surface.
func wireWakewordModels(cfg *config.Config, app *App) {
	const componentID = "api.wakeword_models"

	modelDir := strings.TrimSpace(cfg.Server.ModelDir)
	if modelDir == "" {
		modelDir = "/var/lib/speechkit/models"
	}
	enabled := cfg.Server.Features.WakewordModels
	h := wakewordmodels.New(wakewordmodels.Options{
		Enabled:       enabled,
		PublicBaseURL: cfg.Server.PublicBaseURL,
		// Byte source: the startup R2 sync populates <model_dir>/wakeword (see
		// deploy/docker/entrypoint). Empty/missing dir → /files 404s and clients
		// fall back to the origin the catalog URLs already name.
		LocalDir: filepath.Join(modelDir, "wakeword"),
	})
	h.Mount(app.Mux)

	if enabled {
		app.Health.SetReady(componentID, StatusOK, "listening")
		slog.Info("wakeword models endpoint mounted", "path", "/v1/wakeword/models")
		return
	}
	app.Health.SetReadyWithOptions(componentID, StatusDisabled,
		"wake-word model serving disabled (features.wakeword_models=false)",
		ComponentOptions{Blocking: false, Kind: "feature"})
	slog.Info("wakeword models endpoint mounted (disabled mode)", "path", "/v1/wakeword/models")
}
