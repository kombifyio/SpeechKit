//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
)

// startWakewordTrainingUploader wires the v0.37.5 device-side
// background uploader that POSTs locally captured wake-word
// activations to the server's /v1/wakeword/activations endpoint.
//
// Privacy gates (all default OFF):
//   - cfg.Wakeword.TrainingData.LocalCaptureEnabled must be true
//     (otherwise there are no .wav/.json files to upload).
//   - cfg.Wakeword.TrainingData.UploadEnabled must be true.
//
// On a misconfiguration (missing URL / unreachable token env / etc)
// the function logs once and returns without starting a goroutine.
// Wake-word capture continues working locally; only the upload pump
// is skipped. This keeps the user's local training data intact even
// when the remote side is down.
func startWakewordTrainingUploader(ctx context.Context, cfg *config.Config, state *appState, cleanup *desktopCleanupStack) {
	if cfg == nil {
		return
	}
	td := cfg.Wakeword.TrainingData
	if !td.LocalCaptureEnabled || !td.UploadEnabled {
		return
	}

	dir := resolveTrainingCaptureDir(td.LocalCaptureDir)
	if err := ensureTrainingCaptureDir(dir); err != nil {
		slog.Warn("training_uploader: capture dir unavailable — uploader disabled",
			"dir", dir, "err", err)
		state.addLog(fmt.Sprintf("Wake-word uploader disabled (capture dir): %v", err), "warn")
		return
	}

	serverURL := strings.TrimSpace(td.UploadServerURL)
	if serverURL == "" {
		slog.Warn("training_uploader: upload_server_url missing — uploader disabled")
		state.addLog("Wake-word uploader disabled (upload_server_url empty)", "warn")
		return
	}

	token := ""
	if env := strings.TrimSpace(td.UploadTokenEnv); env != "" {
		token = strings.TrimSpace(os.Getenv(env))
		if token == "" {
			slog.Warn("training_uploader: upload_token_env set but empty — uploader disabled",
				"env", env)
			state.addLog(fmt.Sprintf("Wake-word uploader disabled ($%s empty)", env), "warn")
			return
		}
	}

	interval := time.Duration(maxInt(td.UploadIntervalMinutes, 5)) * time.Minute

	up, err := wakeword.NewTrainingUploader(wakeword.TrainingUploaderConfig{
		Dir:         dir,
		ServerURL:   serverURL,
		BearerToken: token,
		Interval:    interval,
		OnlyLabeled: td.UploadOnlyLabeled,
	})
	if err != nil {
		slog.Warn("training_uploader: init failed", "err", err)
		state.addLog(fmt.Sprintf("Wake-word uploader disabled (init): %v", err), "warn")
		return
	}

	// Use the parent ctx directly so the uploader stops cleanly on
	// app shutdown without an extra cancel-leak watchdog. The
	// cleanup callback calls Close() which makes the next tick
	// observe the closed flag and return.
	go func() {
		err := up.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("training_uploader: run exited", "err", err)
		}
	}()

	if cleanup != nil {
		cleanup.Add(up.Close)
	}
	slog.Info("training_uploader: started",
		"dir", dir,
		"server_url", serverURL,
		"interval", interval,
		"only_labeled", td.UploadOnlyLabeled)
	state.addLog("Wake-word training uploader started", "info")
}
