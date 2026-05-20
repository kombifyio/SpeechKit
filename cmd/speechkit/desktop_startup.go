package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

// seedRuntimeConfigFromInstallTemplate ensures cfgPath exists before
// config.Load runs. The NSIS installer writes config.toml + config.default.toml
// to $LOCALAPPDATA\SpeechKit (the install directory), but for installed
// (non-portable) builds runtimeConfigPath() resolves to %APPDATA%\SpeechKit
// (Roaming). These are different directories, so SpeechKit was silently
// loading Go defaults on every fresh install — most visibly causing the
// overlay to render at the "bottom" Go default instead of the "top" template
// value (regression 2026-05-19, observed live in user's log).
//
// On first launch we copy the install-dir config.default.toml to the runtime
// config path. config.Load can then read the template defaults; the wizard's
// /settings/update later persists user choices to the same path.
func seedRuntimeConfigFromInstallTemplate(cfgPath string) {
	if cfgPath == "" {
		return
	}
	if _, err := os.Stat(cfgPath); err == nil {
		return
	}
	exeDir := executableDir()
	if exeDir == "" {
		return
	}
	template := filepath.Join(exeDir, "config.default.toml")
	data, err := os.ReadFile(template)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("seed runtime config: read install template", "path", template, "err", err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		slog.Warn("seed runtime config: create dir", "path", filepath.Dir(cfgPath), "err", err)
		return
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		slog.Warn("seed runtime config: write", "path", cfgPath, "err", err)
		return
	}
	slog.Info("runtime config seeded from install template", "from", template, "to", cfgPath)
}

func loadDesktopStartupConfig() (string, *config.Config, *config.InstallState, error) {
	cfgPath := runtimeConfigPath()
	seedRuntimeConfigFromInstallTemplate(cfgPath)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return "", nil, nil, err
	}
	normalizeConfigModes(cfg)
	applySelectedVoiceAgentProfile(cfg, filteredModelCatalog())
	if migrated, err := secrets.MigrateInstallTokenBootstrap(); err != nil {
		slog.Warn("migrate install hugging face token", "err", err)
	} else if migrated {
		slog.Info("install token migrated Hugging Face bootstrap token into secure storage")
	}

	installState, err := config.LoadInstallState()
	if err != nil {
		slog.Warn("install state load failed", "err", err)
		installState = &config.InstallState{Mode: config.InstallModeLocal}
	}
	if installState.Mode == "" {
		installState.Mode = config.InstallModeLocal
		installState.SetupDone = false
		if err := config.SaveInstallState(installState); err != nil {
			slog.Warn("save install state", "err", err)
		}
		slog.Info("install mode: local (default, first run — setup wizard pending)")
	} else {
		slog.Info("install mode", "mode", installState.Mode)
	}

	if config.ApplyLocalInstallDefaults(cfg, installState) {
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save local install defaults", "err", err)
		} else {
			slog.Info("local install defaults: selected bundled local speech model for whisper.cpp")
		}
	}
	if applyBundledLocalSTTStarterDefault(cfg) {
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save bundled local STT starter default", "err", err)
		} else {
			slog.Info("local install defaults: selected bundled local STT starter model")
		}
	}
	if applyLocalSTTPortDefault(cfg) {
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save local STT port default", "err", err)
		} else {
			slog.Info("local install defaults: selected non-conflicting local STT port", "port", cfg.Local.Port)
		}
	}
	if reconcileRoutingStrategyFromSelection(cfg) {
		if err := config.Save(cfgPath, cfg); err != nil {
			slog.Warn("save reconciled routing strategy", "err", err)
		} else {
			slog.Info("routing strategy reconciled from current dictate primary selection", "strategy", cfg.Routing.Strategy)
		}
	}
	if config.ApplyManagedIntegrationDefaults(cfg) {
		slog.Info("managed integration: Hugging Face enabled by explicit opt-in with resolved credentials")
	}

	return cfgPath, cfg, installState, nil
}
