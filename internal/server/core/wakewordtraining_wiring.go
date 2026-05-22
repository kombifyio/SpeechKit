//go:build linux

package core

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/wakewordtraining"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// wireWakewordTraining mounts the v0.37.5 REST endpoints that receive
// wake-word activation training-data uploads from device clients.
//
// Privacy gate: the mount only activates when
// `[server.training_data].accept_uploads = true`. When the toggle is
// false (the default) the handler still hangs the routes but every
// request returns 503 so the device-side uploader can detect and back
// off. This matches the privacy contract described in
// docs/wakeword-training-data.md.
//
// Storage gate: the backing store must satisfy
// store.WakewordActivationStore (both SQLite and Postgres do, see
// internal/store/wakeword_activations.go). If the assertion fails the
// component is marked unavailable on /readyz so the operator gets a
// clear signal — usually "store not configured".
func wireWakewordTraining(cfg *config.Config, app *App) {
	const componentID = "api.wakeword_training"

	if !cfg.Server.TrainingData.AcceptUploads {
		// Disabled-by-default branch. Still construct the handler so
		// the 503 path stays uniform (callers can probe the endpoint
		// and get a structured "training_data_disabled" payload).
		h, err := wakewordtraining.New(wakewordtraining.Options{
			AcceptUploads: false,
		})
		if err != nil {
			app.Health.SetReadyWithOptions(componentID, StatusUnavailable,
				fmt.Sprintf("init failed: %v", err),
				ComponentOptions{Blocking: false, Kind: "feature"})
			return
		}
		h.Mount(app.Mux)
		app.Health.SetReadyWithOptions(componentID, StatusUnavailable,
			"training-data uploads disabled (accept_uploads=false)",
			ComponentOptions{Blocking: false, Kind: "feature"})
		slog.Info("wakeword training endpoint mounted (disabled mode)",
			"path", "/v1/wakeword/activations",
			"accept_uploads", false)
		return
	}

	st, ok := app.Store.(store.WakewordActivationStore)
	if !ok {
		app.Health.SetReadyWithOptions(componentID, StatusUnavailable,
			"store does not support wake-word activations",
			ComponentOptions{Blocking: false, Kind: "feature"})
		return
	}

	audioDir := resolveAudioDir(cfg)
	quota := cfg.Server.TrainingData.PerUserQuotaBytes
	if quota == 0 {
		// Default per-user quota: 1 GiB. Generous enough for thousands
		// of 16 kHz mono S16LE clips (~32 KB / sec → ~9 hours stored).
		quota = 1 << 30
	}

	maxUploadBytes := int64(cfg.Server.MaxUploadMB) << 20
	if maxUploadBytes <= 0 {
		maxUploadBytes = 10 << 20 // 10 MiB safety net
	}

	h, err := wakewordtraining.New(wakewordtraining.Options{
		Store:             st,
		AudioDir:          audioDir,
		MaxUploadBytes:    maxUploadBytes,
		PerUserQuotaBytes: quota,
		AcceptUploads:     true,
	})
	if err != nil {
		app.Health.SetReadyWithOptions(componentID, StatusUnavailable,
			fmt.Sprintf("init failed: %v", err),
			ComponentOptions{Blocking: false, Kind: "feature"})
		return
	}
	h.Mount(app.Mux)
	app.Health.SetReady(componentID, StatusOK, "listening")
	slog.Info("wakeword training endpoint mounted",
		"path", "/v1/wakeword/activations",
		"audio_dir", audioDir,
		"quota_bytes", quota,
		"max_upload_bytes", maxUploadBytes)
}

// resolveAudioDir picks the on-disk location for uploaded activation
// audio. Operators can override via [server.training_data].audio_dir;
// when blank we fall back to <model_dir>/wakeword-activations which
// keeps voice data co-located with other large server artefacts so
// the operator only has to mount one persistent volume.
func resolveAudioDir(cfg *config.Config) string {
	if dir := strings.TrimSpace(cfg.Server.TrainingData.AudioDir); dir != "" {
		return dir
	}
	base := strings.TrimSpace(cfg.Server.ModelDir)
	if base == "" {
		base = "/var/lib/speechkit"
	}
	return filepath.Join(base, "wakeword-activations")
}
