package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kombifyio/SpeechKit/internal/runtimepath"
)

// initRuntimeDirectories ensures every runtime directory the desktop
// client writes to exists with the correct permissions before downstream
// code (config load, secrets, audit log, feedback store, download
// manager) touches them. Without this, each consumer performs its own
// ad-hoc MkdirAll on first write, producing inconsistent error handling
// and making "first-run launch hits permission error in component X"
// reports hard to diagnose.
//
// The helper is idempotent. For each target it logs:
//
//   - desktop.startup.dir_created — MkdirAll succeeded against a missing
//     path. Includes the resolved path and the requested mode.
//   - desktop.startup.dir_present — the path already exists. Includes
//     the observed mode for permission-drift triage.
//   - desktop.startup.dir_create_failed — MkdirAll returned an error.
//     Logged at error level. Downstream consumers still have their own
//     lazy-MkdirAll fallbacks and the operator can fix permissions and
//     restart.
//
// Acceptance: kombify-SpeechKit-1pg.
func initRuntimeDirectories() {
	targets := []struct {
		purpose string
		path    string
		mode    os.FileMode
	}{
		{purpose: "data_dir", path: runtimepath.DataDir(), mode: 0o755},
		{purpose: "local_data_dir", path: runtimepath.LocalDataDir(), mode: 0o755},
		{purpose: "secrets_dir", path: runtimepath.SecretsDir(), mode: 0o700},
		{purpose: "logs_dir", path: runtimeLogsDir(), mode: 0o755},
	}
	for _, t := range targets {
		if t.path == "" {
			slog.Warn("desktop.startup.dir_skipped", "purpose", t.purpose, "reason", "empty_path")
			continue
		}
		if info, err := os.Stat(t.path); err == nil && info.IsDir() {
			slog.Info("desktop.startup.dir_present",
				"purpose", t.purpose,
				"path", t.path,
				"mode", info.Mode().Perm().String(),
			)
			continue
		}
		if err := os.MkdirAll(t.path, t.mode); err != nil {
			slog.Error("desktop.startup.dir_create_failed",
				"purpose", t.purpose,
				"path", t.path,
				"err", err,
			)
			continue
		}
		slog.Info("desktop.startup.dir_created",
			"purpose", t.purpose,
			"path", t.path,
			"mode", t.mode.String(),
		)
	}
}

// runtimeLogsDir mirrors the audit log directory resolution in
// app_lifecycle.go: <executable-dir>/logs. Returning an empty string
// when the executable directory cannot be resolved lets the caller
// skip initialisation rather than picking an unsafe fallback.
func runtimeLogsDir() string {
	exeDir := runtimepath.ExecutableDir()
	if exeDir == "" {
		return ""
	}
	return filepath.Join(exeDir, "logs")
}
