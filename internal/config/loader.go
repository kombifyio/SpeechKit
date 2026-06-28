package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	customtemplates "github.com/kombifyio/SpeechKit/internal/customize/templates"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

var (
	lastPolicyMu sync.Mutex
	lastPolicy   PolicyValues
)

// LastPolicy returns the PolicyValues applied during the most recent Load call.
// This accessor lets the app layer read policy metadata (Origin, KeysFound)
// for the policy.applied audit event without changing Load's return signature.
// Returns a zero-value PolicyValues if Load has not yet been called.
func LastPolicy() PolicyValues {
	lastPolicyMu.Lock()
	defer lastPolicyMu.Unlock()
	return lastPolicy
}

// Load reads config from the given path. Falls back to defaults if file not found.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path == "" {
		path = defaultConfigPath()
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path is the application config path supplied by startup/config plumbing.
	if err != nil {
		if os.IsNotExist(err) {
			NormalizeSpeechDefaults(cfg)
			NormalizeCustomizationDefaults(cfg)
			NormalizeHandsFreeConfig(cfg, true)
			NormalizeOutputConfig(cfg)
			NormalizeCaptureConfig(cfg)
			NormalizeDeepgramSTTCodeSwitching(cfg)
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
		cfg := defaults()
		NormalizeCaptureConfig(cfg)
		NormalizeDeepgramSTTCodeSwitching(cfg)
		return cfg, nil
	}
	if err := rejectRemovedConfigAliases(meta); err != nil {
		return nil, err
	}

	// Apply registry policy overlay (ADMX/GPO). Must run BEFORE the backfill
	// chain so that policy values are in place when backfill logic mirrors them
	// (e.g. Update.Enabled → Telemetry.UpdateCheck). On non-Windows platforms
	// applyPolicyOverlay is a no-op (see policy_other.go).
	policy := ReadPolicyValues()
	applyPolicyOverlay(cfg, policy)
	lastPolicyMu.Lock()
	lastPolicy = policy
	lastPolicyMu.Unlock()
	if policy.KeysFound > 0 {
		slog.Info("config: policy overlay applied",
			"origin", policy.Origin,
			"keys_locked", policy.KeysFound)
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
	backfillVoiceAgentPromptLayers(cfg)
	backfillVoiceAgentSessionSummary(meta, cfg)
	backfillServerConnectionCustomURLAuth(meta, cfg)
	normalizeSpeechDefaults(cfg, meta.IsDefined("speech"), meta.IsDefined("speech", "language"), meta.IsDefined("general", "language"))
	NormalizeCustomizationDefaults(cfg)
	NormalizeHandsFreeConfig(cfg, meta.IsDefined("hands_free"))
	NormalizeOutputConfig(cfg)
	NormalizeCaptureConfig(cfg)
	NormalizeDeepgramSTTCodeSwitching(cfg)
	// Backfill: Telemetry.UpdateCheck mirrors Update.Enabled when update is disabled.
	// Phase 0 has only the update-check as telemetry; later phases may diverge.
	if !cfg.Update.Enabled {
		cfg.Telemetry.UpdateCheck = false
	}
	cfg.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID)
	cfg.VoiceAgent.AgentSequenceID = strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID)
	cfg.VoiceAgent.CloseBehavior = NormalizeVoiceAgentCloseBehavior(
		cfg.VoiceAgent.CloseBehavior,
		VoiceAgentCloseBehaviorContinue,
	)
	cfg.VoiceAgent.BargeIn = NormalizeVoiceAgentBargeIn(
		cfg.VoiceAgent.BargeIn,
		VoiceAgentBargeInAuto,
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
	NormalizeSpeechDefaults(cfg)
	NormalizeCustomizationDefaults(cfg)
	NormalizeHandsFreeConfig(cfg, true)
	NormalizeOutputConfig(cfg)
	NormalizeCaptureConfig(cfg)
	NormalizeDeepgramSTTCodeSwitching(cfg)
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

func NormalizeCaptureConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.General.DictationProcessingMode = NormalizeDictationProcessingMode(
		cfg.General.DictationProcessingMode,
		DictationProcessingModeFinalFull,
	)
	cfg.Audio.InputSource = NormalizeAudioInputSource(
		cfg.Audio.InputSource,
		AudioInputSourceMicrophone,
	)
}

func NormalizeSpeechDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	normalizeSpeechDefaults(cfg, true, strings.TrimSpace(cfg.Speech.Language) != "", strings.TrimSpace(cfg.General.Language) != "")
}

func NormalizeCustomizationDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Customization.ActiveTemplateIDs = customtemplates.NormalizeActiveTemplateIDs(cfg.Customization.ActiveTemplateIDs)
}

func normalizeSpeechDefaults(cfg *Config, speechDefined, speechLanguageDefined, generalLanguageDefined bool) {
	if cfg == nil {
		return
	}
	cfg.Speech.Language = strings.TrimSpace(cfg.Speech.Language)
	cfg.General.Language = strings.TrimSpace(cfg.General.Language)
	switch {
	case !speechDefined || !speechLanguageDefined:
		cfg.Speech.Language = firstNonEmptyConfig(cfg.General.Language, cfg.Speech.Language, "de")
	case !generalLanguageDefined:
		cfg.General.Language = firstNonEmptyConfig(cfg.Speech.Language, cfg.General.Language, "de")
	}
	if cfg.General.Language == "" {
		cfg.General.Language = firstNonEmptyConfig(cfg.Speech.Language, "de")
	}
	if cfg.Speech.Language == "" {
		cfg.Speech.Language = cfg.General.Language
	}
	if cfg.Speech.EndpointingMs < 0 {
		cfg.Speech.EndpointingMs = 0
	}
	if cfg.Speech.Speed <= 0 {
		cfg.Speech.Speed = 1
	}
	cfg.Speech.AudioFormat = strings.TrimSpace(cfg.Speech.AudioFormat)
	if cfg.Speech.AudioFormat == "" {
		cfg.Speech.AudioFormat = "mp3"
	}
}

func firstNonEmptyConfig(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func defaultConfigPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "config.toml")
}
