package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

var (
	dopplerLookPath              = exec.LookPath
	dopplerSecretLookup          = defaultDopplerSecretLookup
	managedHFDefaultOptIn        string
	managedDopplerDefaultProject string
	managedDopplerDefaultConfig  string
)

type Config struct {
	General     GeneralConfig     `toml:"general"`
	Audio       AudioConfig       `toml:"audio"`
	UI          UIConfig          `toml:"ui"`
	Local       LocalConfig       `toml:"local"`
	VPS         VPSConfig         `toml:"vps"`
	HuggingFace HuggingFaceConfig `toml:"huggingface"`
	Routing     RoutingConfig     `toml:"routing"`
	Feedback    FeedbackConfig    `toml:"feedback"` // legacy compat; prefer Store
	Store       StoreConfig       `toml:"store"`
}

type StoreConfig struct {
	Backend            string `toml:"backend"` // "sqlite" | "postgres" | registered name
	SQLitePath         string `toml:"sqlite_path"`
	PostgresDSN        string `toml:"postgres_dsn"`
	SaveAudio          bool   `toml:"save_audio"`
	AudioRetentionDays int    `toml:"audio_retention_days"`
	MaxAudioStorageMB  int    `toml:"max_audio_storage_mb"`
}

type GeneralConfig struct {
	Language          string `toml:"language"`
	Hotkey            string `toml:"hotkey"`
	DictateHotkey     string `toml:"dictate_hotkey"`
	AgentHotkey       string `toml:"agent_hotkey"`
	ActiveMode        string `toml:"active_mode"`
	HotkeyMode        string `toml:"hotkey_mode"` // "push_to_talk" or "toggle"
	AutoStopSilenceMs int    `toml:"auto_stop_silence_ms"`
	FastModeSilenceMs int    `toml:"fast_mode_silence_ms"` // silence threshold for Quick Capture auto-stop
}

type AudioConfig struct {
	Backend     string `toml:"backend"`
	DeviceID    string `toml:"device_id"`
	SampleRate  int    `toml:"sample_rate"`
	Channels    int    `toml:"channels"`
	FrameSizeMs int    `toml:"frame_size_ms"`
	LatencyHint string `toml:"latency_hint"`
}

type UIConfig struct {
	OverlayEnabled  bool   `toml:"overlay_enabled"`
	OverlayPosition string `toml:"overlay_position"` // "top", "bottom", "left", "right"
	Visualizer      string `toml:"visualizer"`
	Design          string `toml:"design"`
}

type LocalConfig struct {
	Enabled   bool   `toml:"enabled"`
	Model     string `toml:"model"`
	ModelPath string `toml:"model_path"`
	Port      int    `toml:"port"`
	GPU       string `toml:"gpu"`
}

type VPSConfig struct {
	Enabled   bool   `toml:"enabled"`
	URL       string `toml:"url"`
	APIKeyEnv string `toml:"api_key_env"`
}

type HuggingFaceConfig struct {
	Enabled  bool   `toml:"enabled"`
	Model    string `toml:"model"`
	TokenEnv string `toml:"token_env"`
}

type RoutingConfig struct {
	Strategy                string  `toml:"strategy"`
	PreferLocalUnderSeconds float64 `toml:"prefer_local_under_seconds"`
	ParallelCloud           bool    `toml:"parallel_cloud"`
	ReplaceOnBetter         bool    `toml:"replace_on_better"`
}

type FeedbackConfig struct {
	SaveAudio          bool   `toml:"save_audio"`
	AudioRetentionDays int    `toml:"audio_retention_days"`
	DBPath             string `toml:"db_path"`
	MaxAudioStorageMB  int    `toml:"max_audio_storage_mb"`
}

// Load reads config from the given path. Falls back to defaults if file not found.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path == "" {
		path = defaultConfigPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	meta, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
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

	return cfg, nil
}

func Save(path string, cfg *Config) error {
	if path == "" {
		path = defaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	defer file.Close()

	if err := toml.NewEncoder(file).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	return nil
}

func defaults() *Config {
	return &Config{
		General: GeneralConfig{
			Language:          "de",
			Hotkey:            "win+alt",
			DictateHotkey:     "win+alt",
			AgentHotkey:       "ctrl+shift+k",
			ActiveMode:        "dictate",
			HotkeyMode:        "push_to_talk",
			AutoStopSilenceMs: 500,
			FastModeSilenceMs: 1500,
		},
		Audio: AudioConfig{
			Backend:     "windows-wasapi-malgo",
			SampleRate:  16000,
			Channels:    1,
			FrameSizeMs: 32,
			LatencyHint: "interactive",
		},
		UI: UIConfig{
			OverlayEnabled:  true,
			OverlayPosition: "top",
			Visualizer:      "pill",
			Design:          "default",
		},
		Local: LocalConfig{
			Enabled: false,
			Model:   "ggml-small.bin",
			Port:    8080,
			GPU:     "auto",
		},
		VPS: VPSConfig{
			Enabled:   false,
			APIKeyEnv: "VPS_API_KEY",
		},
		HuggingFace: HuggingFaceConfig{
			Enabled:  false,
			Model:    "openai/whisper-large-v3",
			TokenEnv: "HF_TOKEN",
		},
		Routing: RoutingConfig{
			Strategy:                "cloud-only",
			PreferLocalUnderSeconds: 10,
			ParallelCloud:           false,
			ReplaceOnBetter:         false,
		},
		Feedback: FeedbackConfig{
			SaveAudio:          true,
			AudioRetentionDays: 7,
			MaxAudioStorageMB:  500,
		},
		Store: StoreConfig{
			Backend:            "sqlite",
			SaveAudio:          true,
			AudioRetentionDays: 7,
			MaxAudioStorageMB:  500,
		},
	}
}

func defaultConfigPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "config.toml")
}

// ResolveSecret resolves a secret by name. Checks environment first, then Doppler CLI
// when DOPPLER_PROJECT and DOPPLER_CONFIG are set explicitly.
func ResolveSecret(envName string) string {
	if v := os.Getenv(envName); v != "" {
		return v
	}
	return dopplerGet(envName)
}

func HuggingFaceTokenEnvName(cfg *Config) string {
	if cfg == nil {
		return "HF_TOKEN"
	}
	if tokenEnv := strings.TrimSpace(cfg.HuggingFace.TokenEnv); tokenEnv != "" {
		return tokenEnv
	}
	return "HF_TOKEN"
}

func HuggingFaceTokenStatus(cfg *Config) (secrets.TokenStatus, error) {
	tokenEnv := HuggingFaceTokenEnvName(cfg)
	return secrets.HuggingFaceTokenStatus(func() string {
		return ResolveSecret(tokenEnv)
	})
}

func ResolveHuggingFaceToken(cfg *Config) (string, secrets.TokenStatus, error) {
	tokenEnv := HuggingFaceTokenEnvName(cfg)
	return secrets.ResolveHuggingFaceToken(func() string {
		return ResolveSecret(tokenEnv)
	})
}

// dopplerGet tries to resolve a secret via `doppler secrets get` CLI.
func dopplerGet(key string) string {
	dopplerPath := findDopplerExecutable()
	if dopplerPath == "" {
		return ""
	}

	projects := dopplerProjects()
	configs := dopplerConfigs()
	if len(projects) == 0 || len(configs) == 0 {
		return ""
	}

	for _, project := range projects {
		for _, cfg := range configs {
			v, err := dopplerSecretLookup(dopplerPath, key, project, cfg)
			if err == nil && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func findDopplerExecutable() string {
	if custom := strings.TrimSpace(os.Getenv("DOPPLER_PATH")); custom != "" && fileExists(custom) {
		return custom
	}

	if resolved, err := dopplerLookPath("doppler"); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved
	}

	candidates := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "doppler.exe"),
		filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local", "Microsoft", "WinGet", "Links", "doppler.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Doppler", "doppler.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Doppler", "doppler.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Doppler", "doppler.exe"),
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func dopplerProjects() []string {
	if rawProject, ok := os.LookupEnv("DOPPLER_PROJECT"); ok {
		if project := strings.TrimSpace(rawProject); project != "" {
			return []string{project}
		}
		return nil
	}
	if project := strings.TrimSpace(managedDopplerDefaultProject); project != "" {
		return []string{project}
	}
	return nil
}

func dopplerConfigs() []string {
	if rawConfig, ok := os.LookupEnv("DOPPLER_CONFIG"); ok {
		if cfg := strings.TrimSpace(rawConfig); cfg != "" {
			return []string{cfg}
		}
		return nil
	}
	if cfg := strings.TrimSpace(managedDopplerDefaultConfig); cfg != "" {
		return []string{cfg}
	}
	return nil
}

func defaultDopplerSecretLookup(dopplerPath, key, project, cfg string) (string, error) {
	cmd := exec.Command(
		dopplerPath, "secrets", "get", key,
		"--plain",
		"--project", project,
		"--config", cfg,
		"--no-read-env",
	)
	// Hide the console window on Windows (prevents terminal flash in GUI mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func resetDopplerHooksForTests() {
	dopplerLookPath = exec.LookPath
	dopplerSecretLookup = defaultDopplerSecretLookup
}

func ApplyManagedIntegrationDefaults(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	if !managedHFOptInEnabled() {
		return false
	}

	if cfg.HuggingFace.Enabled || cfg.VPS.Enabled || cfg.Local.Enabled {
		return false
	}

	if cfg.Routing.Strategy != "cloud-only" {
		return false
	}

	tokenEnv := HuggingFaceTokenEnvName(cfg)
	cfg.HuggingFace.TokenEnv = tokenEnv

	token, _, err := ResolveHuggingFaceToken(cfg)
	if err != nil || token == "" {
		return false
	}

	cfg.HuggingFace.Enabled = true
	if strings.TrimSpace(cfg.HuggingFace.Model) == "" {
		cfg.HuggingFace.Model = "openai/whisper-large-v3"
	}
	return true
}

func managedHFOptInEnabled() bool {
	if raw, ok := os.LookupEnv("SPEECHKIT_ENABLE_MANAGED_HF"); ok {
		return parseManagedBool(raw)
	}
	return parseManagedBool(managedHFDefaultOptIn)
}

func parseManagedBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
