package main

import (
	"log/slog"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

func loadDesktopStartupConfig() (string, *config.Config, *config.InstallState, error) {
	cfgPath := runtimeConfigPath()
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
			slog.Info("local install defaults: onboarding will download a local model before enabling whisper.cpp")
		}
	}
	if config.ApplyManagedIntegrationDefaults(cfg) {
		slog.Info("managed integration: Hugging Face enabled by explicit opt-in with resolved credentials")
	}

	return cfgPath, cfg, installState, nil
}
