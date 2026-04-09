package config

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

var (
	dopplerLookPath              = exec.LookPath
	dopplerSecretLookup          = secrets.DefaultDopplerSecretLookup
	managedHFBuildEnabled        string
	managedHFDefaultOptIn        string
	managedDopplerDefaultProject string
	managedDopplerDefaultConfig  string
)

type Config struct {
	General     GeneralConfig     `toml:"general"`
	Audio       AudioConfig       `toml:"audio"`
	UI          UIConfig          `toml:"ui"`
	Vocabulary  VocabularyConfig  `toml:"vocabulary"`
	Local       LocalConfig       `toml:"local"`
	VPS         VPSConfig         `toml:"vps"`
	HuggingFace HuggingFaceConfig `toml:"huggingface"`
	Routing     RoutingConfig     `toml:"routing"`
	Feedback    FeedbackConfig    `toml:"feedback"` // legacy compat; prefer Store
	Store       StoreConfig       `toml:"store"`
	Providers   ProvidersConfig   `toml:"providers"`
	TTS         TTSConfig         `toml:"tts"`
	VoiceAgent  VoiceAgentConfig  `toml:"voice_agent"`
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
	Hotkey            string `toml:"hotkey"` // Deprecated: legacy single-hotkey field kept for config file compat. Use DictateHotkey.
	DictateHotkey     string `toml:"dictate_hotkey"`
	AgentHotkey       string `toml:"agent_hotkey"`
	AgentMode         string `toml:"agent_mode"`  // "assist" or "voice_agent" â€” determines what agent_hotkey triggers
	ActiveMode        string `toml:"active_mode"` // legacy compat
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

type VocabularyConfig struct {
	Dictionary string `toml:"dictionary"`
}

type UIConfig struct {
	OverlayEnabled  bool   `toml:"overlay_enabled"`
	OverlayPosition string `toml:"overlay_position"` // "top", "bottom", "left", "right"
	OverlayMovable  bool   `toml:"overlay_movable"`
	OverlayFreeX    int    `toml:"overlay_free_x"`
	OverlayFreeY    int    `toml:"overlay_free_y"`
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
	Enabled      bool   `toml:"enabled"`
	Model        string `toml:"model"`
	UtilityModel string `toml:"utility_model"`
	AgentModel   string `toml:"agent_model"`
	TokenEnv     string `toml:"token_env"`
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

// ProvidersConfig groups all external provider configurations.
type ProvidersConfig struct {
	OpenAI     OpenAIProviderConfig     `toml:"openai"`
	Groq       GroqProviderConfig       `toml:"groq"`
	Google     GoogleProviderConfig     `toml:"google"`
	Ollama     OllamaProviderConfig     `toml:"ollama"`
	OpenRouter OpenRouterProviderConfig `toml:"openrouter"`
}

type OpenAIProviderConfig struct {
	Enabled       bool   `toml:"enabled"`
	APIKeyEnv     string `toml:"api_key_env"`
	STTModel      string `toml:"stt_model"`
	UtilityModel  string `toml:"utility_model"`
	AgentModel    string `toml:"agent_model"`
	TTSModel      string `toml:"tts_model"`
	TTSVoice      string `toml:"tts_voice"`
	RealtimeModel string `toml:"realtime_model"`
}

type GroqProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AgentModel   string `toml:"agent_model"`
}

type GoogleProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AgentModel   string `toml:"agent_model"`
}

type OllamaProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	BaseURL      string `toml:"base_url"`
	STTModel     string `toml:"stt_model"`
	UtilityModel string `toml:"utility_model"`
	AgentModel   string `toml:"agent_model"`
}

type OpenRouterProviderConfig struct {
	Enabled      bool   `toml:"enabled"`
	APIKeyEnv    string `toml:"api_key_env"`
	UtilityModel string `toml:"utility_model"`
	AgentModel   string `toml:"agent_model"`
}

// TTSConfig configures text-to-speech for Assist Mode.
type TTSConfig struct {
	Enabled     bool           `toml:"enabled"`
	Strategy    string         `toml:"strategy"` // "cloud-first", "local-first", "cloud-only", "local-only"
	Voice       string         `toml:"voice"`    // Global default voice override
	Speed       float64        `toml:"speed"`    // Global speed 0.25-4.0, default 1.0
	Format      string         `toml:"format"`   // "mp3", "wav", "opus", "pcm"
	OpenAI      TTSOpenAI      `toml:"openai"`
	Google      TTSGoogle      `toml:"google"`
	HuggingFace TTSHuggingFace `toml:"huggingface"`
	Local       TTSLocal       `toml:"local"`
}

type TTSOpenAI struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"` // "tts-1" or "tts-1-hd"
	Voice   string `toml:"voice"` // alloy, echo, fable, onyx, nova, shimmer
}

type TTSGoogle struct {
	Enabled bool   `toml:"enabled"`
	Voice   string `toml:"voice"` // e.g. "de-DE-Neural2-B"
}

type TTSHuggingFace struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"` // e.g. "parler-tts/parler-tts-mini-multilingual-v1.1"
}

type TTSLocal struct {
	Enabled   bool   `toml:"enabled"`
	Model     string `toml:"model"`
	ModelPath string `toml:"model_path"`
	Port      int    `toml:"port"`
}

// VoiceAgentConfig configures the real-time Voice Agent Mode.
type VoiceAgentConfig struct {
	Enabled                bool   `toml:"enabled"`
	Model                  string `toml:"model"`          // Real-time model ID (e.g. "gemini-3.1-flash-live-preview")
	FallbackModel          string `toml:"fallback_model"` // Fallback real-time model
	Voice                  string `toml:"voice"`          // Voice name for real-time model
	ReminderAfterIdleSec   int    `toml:"reminder_after_idle_sec"`
	DeactivateAfterIdleSec int    `toml:"deactivate_after_idle_sec"`
	PipelineFallback       bool   `toml:"pipeline_fallback"` // Use STT+LLM+TTS as last resort
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
			AgentMode:         "assist",
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
		Vocabulary: VocabularyConfig{
			Dictionary: "",
		},
		UI: UIConfig{
			OverlayEnabled:  true,
			OverlayPosition: "top",
			OverlayMovable:  false,
			OverlayFreeX:    0,
			OverlayFreeY:    0,
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
			Enabled:      ManagedHuggingFaceAvailableInBuild(),
			Model:        "openai/whisper-large-v3",
			UtilityModel: "",
			AgentModel:   "",
			TokenEnv:     "HF_TOKEN",
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
		TTS: TTSConfig{
			Enabled:  true,
			Strategy: "cloud-first",
			Speed:    1.0,
			Format:   "mp3",
			OpenAI: TTSOpenAI{
				Enabled: true,
				Model:   "tts-1",
				Voice:   "nova",
			},
			Google: TTSGoogle{
				Enabled: false,
				Voice:   "de-DE-Neural2-B",
			},
			HuggingFace: TTSHuggingFace{
				Enabled: false,
				Model:   "parler-tts/parler-tts-mini-multilingual-v1.1",
			},
			Local: TTSLocal{
				Enabled: false,
				Model:   "hexgrad/Kokoro-82M",
				Port:    8081,
			},
		},
		VoiceAgent: VoiceAgentConfig{
			Enabled:                true,
			Model:                  "gemini-3.1-flash-live-preview",
			FallbackModel:          "gpt-realtime-mini",
			Voice:                  "Kore",
			ReminderAfterIdleSec:   300,
			DeactivateAfterIdleSec: 900,
			PipelineFallback:       true,
		},
		Providers: ProvidersConfig{
			OpenAI: OpenAIProviderConfig{
				APIKeyEnv:     "OPENAI_API_KEY",
				STTModel:      "whisper-1", // Fallback only; HuggingFace is primary STT
				UtilityModel:  "gpt-4o-mini",
				AgentModel:    "gpt-4o",
				TTSModel:      "tts-1",
				TTSVoice:      "nova",
				RealtimeModel: "gpt-realtime-mini",
			},
			Groq: GroqProviderConfig{
				APIKeyEnv:    "GROQ_API_KEY",
				STTModel:     "whisper-large-v3-turbo",
				UtilityModel: "llama-3.1-8b-instant",
				AgentModel:   "llama-3.3-70b-versatile",
			},
			Google: GoogleProviderConfig{
				APIKeyEnv:    "GOOGLE_AI_API_KEY",
				STTModel:     "chirp_3",
				UtilityModel: "gemini-3.1-flash-lite-preview",
				AgentModel:   "gemini-3.1-pro-preview",
			},
			Ollama: OllamaProviderConfig{
				BaseURL:      "http://localhost:11434",
				UtilityModel: "gemma4:e4b",
				AgentModel:   "gemma4:e4b",
			},
			OpenRouter: OpenRouterProviderConfig{
				APIKeyEnv:    "OPENROUTER_API_KEY",
				UtilityModel: "meta-llama/llama-3.1-8b-instruct",
				AgentModel:   "google/gemini-2.5-flash",
			},
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
	if strings.TrimSpace(envName) == "" {
		return ""
	}
	value, _, err := secrets.ResolveNamedSecret(envName, func() string {
		return ResolveSecretFromEnvironmentOrDoppler(envName)
	})
	if err == nil && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return ResolveSecretFromEnvironmentOrDoppler(envName)
}

func ResolveSecretFromEnvironmentOrDoppler(envName string) string {
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
		return ResolveSecretFromEnvironmentOrDoppler(tokenEnv)
	})
}

func ResolveHuggingFaceToken(cfg *Config) (string, secrets.TokenStatus, error) {
	tokenEnv := HuggingFaceTokenEnvName(cfg)
	return secrets.ResolveHuggingFaceToken(func() string {
		return ResolveSecretFromEnvironmentOrDoppler(tokenEnv)
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
	return secrets.FindDopplerExecutable(dopplerLookPath)
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

func resetDopplerHooksForTests() {
	dopplerLookPath = exec.LookPath
	dopplerSecretLookup = secrets.DefaultDopplerSecretLookup
}

func ApplyManagedIntegrationDefaults(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	if !ManagedHuggingFaceAvailableInBuild() {
		cfg.HuggingFace.Enabled = false
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

func ManagedHuggingFaceAvailableInBuild() bool {
	return parseManagedBool(managedHFBuildEnabled)
}

func OverrideManagedHuggingFaceBuildForTests(value string) func() {
	previous := managedHFBuildEnabled
	managedHFBuildEnabled = value
	return func() {
		managedHFBuildEnabled = previous
	}
}

func parseManagedBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
