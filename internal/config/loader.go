package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

// Load reads config from the given path. Falls back to defaults if file not found.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path == "" {
		path = defaultConfigPath()
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is the application config path supplied by startup/config plumbing.
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	// Warn (but do not block) when the config file is accessible to group or
	// other users on POSIX systems. No-op on Windows where Go's os.FileMode
	// does not reflect NTFS ACLs.
	if warning, permErr := checkConfigFilePermissions(path); permErr == nil && warning != "" {
		slog.Warn("config file permissions are loose", "msg", warning)
	}

	meta, err := toml.Decode(string(data), cfg)
	if err != nil {
		slog.Warn("malformed config.toml, using defaults", "err", err)
		return defaults(), nil
	}

	// Bridge legacy [feedback] to [store] if store.backend is not explicitly set.
	if cfg.Store.Backend == "" || cfg.Store.Backend == "sqlite" {
		if cfg.Feedback.DBPath != "" && !meta.IsDefined("store", "sqlite_path") && cfg.Store.SQLitePath == "" {
			cfg.Store.SQLitePath = cfg.Feedback.DBPath
		}
		if cfg.Feedback.MaxAudioStorageMB > 0 && !meta.IsDefined("store", "max_audio_storage_mb") && cfg.Store.MaxAudioStorageMB == 0 {
			cfg.Store.MaxAudioStorageMB = cfg.Feedback.MaxAudioStorageMB
		}
		if cfg.Feedback.AudioRetentionDays > 0 && !meta.IsDefined("store", "audio_retention_days") && cfg.Store.AudioRetentionDays == 0 {
			cfg.Store.AudioRetentionDays = cfg.Feedback.AudioRetentionDays
		}
		if !meta.IsDefined("store", "save_audio") {
			cfg.Store.SaveAudio = cfg.Feedback.SaveAudio
		}
	}

	backfillLegacyAssistModels(meta, cfg)
	backfillLegacyModeHotkeys(meta, cfg)
	backfillStartupBehavior(meta, cfg)
	backfillVoiceAgentPromptLayers(meta, cfg)
	backfillVoiceAgentSessionSummary(meta, cfg)
	backfillServerConnectionCustomURLAuth(meta, cfg)
	cfg.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID)
	cfg.VoiceAgent.AgentSequenceID = strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID)
	cfg.VoiceAgent.CloseBehavior = NormalizeVoiceAgentCloseBehavior(
		cfg.VoiceAgent.CloseBehavior,
		VoiceAgentCloseBehaviorContinue,
	)
	cfg.UI.AssistOverlayMode = NormalizeOverlayFeedbackMode(
		cfg.UI.AssistOverlayMode,
		OverlayFeedbackModeSmallFeedback,
	)
	cfg.UI.VoiceAgentOverlayMode = NormalizeOverlayFeedbackMode(
		cfg.UI.VoiceAgentOverlayMode,
		OverlayFeedbackModeSmallFeedback,
	)
	return cfg, nil
}

func backfillServerConnectionCustomURLAuth(meta toml.MetaData, cfg *Config) {
	if cfg == nil || !meta.IsDefined("server_connection", "url") {
		return
	}
	url := strings.TrimRight(strings.TrimSpace(cfg.ServerConnection.URL), "/")
	if url == "" {
		return
	}
	if !meta.IsDefined("server_connection", "auth_mode") &&
		NormalizeServerConnectionAuthMode(cfg.ServerConnection.AuthMode) == ServerConnectionAuthModeAPIKey {
		cfg.ServerConnection.AuthMode = ServerConnectionAuthModeBearer
	}
}

func Save(path string, cfg *Config) error {
	if path == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	file, err := os.Create(path) // #nosec G304 -- path is the application config path supplied by startup/config plumbing.
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer file.Close() //nolint:errcheck // file close on write, error not actionable after encode

	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	return nil
}

func defaultConfigPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "config.toml")
}
