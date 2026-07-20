//go:build windows && cgo

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Mode struct {
		Target string `toml:"target"`
	} `toml:"mode"`
	SpeechKitServer struct {
		BaseURL  string `toml:"base_url"`
		TokenEnv string `toml:"token_env"`
	} `toml:"speechkit_server"`
	Box struct {
		InputDeviceHint  string `toml:"input_device_hint"`
		OutputDeviceHint string `toml:"output_device_hint"`
		// StatusPort waehlt den CDC-Statusport der Box: "" = Autodetect ueber
		// USB VID/PID, "COMx" = expliziter Port, "off" = kein Status-Link.
		StatusPort string `toml:"status_port"`
	} `toml:"box"`
	Wakeword struct {
		Backend      string   `toml:"backend"`
		ModelDir     string   `toml:"model_dir"`
		KeywordsFile string   `toml:"keywords_file"`
		Keywords     []string `toml:"keywords"`
		Phrase       string   `toml:"phrase"`
		Threshold    float32  `toml:"threshold"`
		MinFrames    int      `toml:"min_consecutive_frames"`
		InputGain    float32  `toml:"input_gain"`
		CooldownMS   int      `toml:"cooldown_ms"`
		// openWakeWord-Backend (backend = "openwakeword"):
		// OWWModelDir enthaelt melspectrogram.onnx, embedding_model.onnx und
		// das Phrasenmodell; leer = %LOCALAPPDATA%\SpeechKit\models\wakeword.
		OWWModelDir string `toml:"oww_model_dir"`
		// OWWModel ist der Dateiname des Phrasenmodells; leer = aus der
		// Phrase abgeleitet ("hey kombify" -> hey_kombify.onnx).
		OWWModel string `toml:"oww_model"`
		// OWWOnnxRuntime zeigt auf die onnxruntime.dll fuer den Sidecar;
		// leer = %LOCALAPPDATA%\SpeechKit\onnxruntime.dll.
		OWWOnnxRuntime string `toml:"oww_onnxruntime"`
		// OWWThreshold ist der openWakeWord-Score-Schwellwert (0..1);
		// 0 = Default 0.35. Getrennt vom sherpa-Threshold, andere Skala.
		OWWThreshold float32 `toml:"oww_threshold"`
	} `toml:"wakeword"`
	Capture struct {
		MaxUtteranceSec int `toml:"max_utterance_sec"`
		SilenceCutoffMS int `toml:"silence_cutoff_ms"`
		SilenceRMS      int `toml:"silence_rms"`
	} `toml:"capture"`
	STT struct {
		Provider  string `toml:"provider"`
		BaseURL   string `toml:"base_url"`
		Model     string `toml:"model"`
		APIKeyEnv string `toml:"api_key_env"`
		Language  string `toml:"language"`
	} `toml:"stt"`
	Local struct {
		Enabled   bool   `toml:"enabled"`
		Model     string `toml:"model"`
		ModelPath string `toml:"model_path"`
		Port      int    `toml:"port"`
		GPU       string `toml:"gpu"`
	} `toml:"local"`
	LocalLLM struct {
		Enabled   bool   `toml:"enabled"`
		BaseURL   string `toml:"base_url"`
		Model     string `toml:"model"`
		ModelPath string `toml:"model_path"`
		Port      int    `toml:"port"`
		GPU       string `toml:"gpu"`
	} `toml:"local_llm"`
	Assist struct {
		BaseURL      string `toml:"base_url"`
		Model        string `toml:"model"`
		APIKeyEnv    string `toml:"api_key_env"`
		SystemPrompt string `toml:"system_prompt"`
	} `toml:"assist"`
	TTS struct {
		Provider  string `toml:"provider"`
		BaseURL   string `toml:"base_url"`
		Model     string `toml:"model"`
		Voice     string `toml:"voice"`
		APIKeyEnv string `toml:"api_key_env"`
		Piper     struct {
			Enabled       bool              `toml:"enabled"`
			Binary        string            `toml:"binary"`
			VoiceDir      string            `toml:"voice_dir"`
			DefaultVoices map[string]string `toml:"default_voices"`
			TimeoutSec    int               `toml:"timeout_sec"`
		} `toml:"piper"`
	} `toml:"tts"`
	HomeAssistant struct {
		BaseURL  string `toml:"base_url"`  // e.g. https://ha.example.com:8123
		TokenEnv string `toml:"token_env"` // env var holding the HA Long-Lived Access Token
		Language string `toml:"language"`  // optional; falls back to STT.Language
	} `toml:"home_assistant"`
	VoiceAgent struct {
		// Transport: "local" (Default) spricht direkt mit dem Realtime-
		// Provider; "server" nutzt den speechkit-server Voice-Agent-WS.
		Transport         string `toml:"transport"`
		Provider          string `toml:"provider"`
		PersonaID         string `toml:"persona_id"`
		RoleID            string `toml:"role_id"`
		SequenceID        string `toml:"sequence_id"`
		MediaTransport    string `toml:"media_transport"`
		Model             string `toml:"model"`
		Voice             string `toml:"voice"`
		Locale            string `toml:"locale"`
		SystemPrompt      string `toml:"system_prompt"`
		Thinking          string `toml:"thinking"`
		WaitTimeoutSec    int    `toml:"wait_timeout_sec"`
		IdleReminderSec   int    `toml:"idle_reminder_sec"`
		IdleDeactivateSec int    `toml:"idle_deactivate_sec"`
		// BYO-Think (nur Deepgram): OpenAI-kompatibler Endpoint als Brain
		// des Agenten — z. B. das kombify AI Gateway.
		ThinkProvider    string `toml:"think_provider"`
		ThinkModel       string `toml:"think_model"`
		ThinkEndpointURL string `toml:"think_endpoint_url"`
		ThinkAPIKeyEnv   string `toml:"think_api_key_env"`
	} `toml:"voice_agent"`
}

func loadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if cfg.Wakeword.Phrase == "" {
		cfg.Wakeword.Phrase = "hey jarvis"
	}
	if cfg.Wakeword.Backend == "" {
		cfg.Wakeword.Backend = "sherpa_kws"
	}
	if cfg.Wakeword.Threshold <= 0 {
		cfg.Wakeword.Threshold = 0.25
	}
	if cfg.Wakeword.MinFrames <= 0 {
		cfg.Wakeword.MinFrames = 1
	}
	if cfg.Wakeword.InputGain <= 0 {
		cfg.Wakeword.InputGain = 1
	}
	if cfg.Capture.MaxUtteranceSec <= 0 {
		cfg.Capture.MaxUtteranceSec = 12
	}
	if cfg.Capture.SilenceCutoffMS <= 0 {
		cfg.Capture.SilenceCutoffMS = 1400
	}
	if cfg.Capture.SilenceRMS <= 0 {
		cfg.Capture.SilenceRMS = 500
	}
	if cfg.Mode.Target == "" {
		cfg.Mode.Target = "assist"
	}
	if cfg.SpeechKitServer.BaseURL == "" {
		cfg.SpeechKitServer.BaseURL = os.Getenv("SPEECHKIT_SERVER_URL")
	}
	if cfg.SpeechKitServer.BaseURL == "" {
		cfg.SpeechKitServer.BaseURL = "https://speechkit.kombify.io"
	}
	if cfg.SpeechKitServer.TokenEnv == "" {
		cfg.SpeechKitServer.TokenEnv = "SPEECHKIT_SERVER_TOKEN"
	}
	if cfg.VoiceAgent.Transport == "" {
		cfg.VoiceAgent.Transport = "local"
	}
	if cfg.VoiceAgent.Provider == "" {
		cfg.VoiceAgent.Provider = "deepgram"
	}
	if cfg.VoiceAgent.MediaTransport == "" {
		cfg.VoiceAgent.MediaTransport = "websocket"
	}
	if cfg.VoiceAgent.Locale == "" {
		cfg.VoiceAgent.Locale = cfg.STT.Language
	}
	if cfg.VoiceAgent.Locale == "" {
		cfg.VoiceAgent.Locale = "de"
	}
	if cfg.VoiceAgent.SystemPrompt == "" {
		cfg.VoiceAgent.SystemPrompt = "Du bist kombify, ein kurzer deutscher Voice Agent auf einer kleinen Box. Antworte natuerlich, knapp und gut hoerbar."
	}
	if cfg.VoiceAgent.Thinking == "" {
		cfg.VoiceAgent.Thinking = "low"
	}
	if cfg.VoiceAgent.WaitTimeoutSec <= 0 {
		cfg.VoiceAgent.WaitTimeoutSec = 45
	}
	cfg.applyAssistDefaults()
	return &cfg, nil
}

func (c *Config) sttKey() string    { return resolveCompanionSecret(c.STT.APIKeyEnv) }
func (c *Config) assistKey() string { return resolveCompanionSecret(c.Assist.APIKeyEnv) }
func (c *Config) ttsKey() string    { return resolveCompanionSecret(c.TTS.APIKeyEnv) }
func (c *Config) haToken() string   { return resolveCompanionSecret(c.HomeAssistant.TokenEnv) }
func (c *Config) speechKitServerToken() string {
	if c.SpeechKitServer.TokenEnv == "" {
		return ""
	}
	return resolveCompanionSecret(c.SpeechKitServer.TokenEnv)
}

// resolveCompanionSecret resolves a named secret from the environment first,
// then via the Doppler CLI when DOPPLER_PROJECT/DOPPLER_CONFIG are set.
// Examples consume only pkg/speechkit/*; the richer host secret store
// (Windows DPAPI) is loaded into the environment by the secret-runner
// scripts (scripts/secrets/With-SpeechKitSecrets.ps1) before launch.
func resolveCompanionSecret(envName string) string {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		return v
	}
	project := strings.TrimSpace(os.Getenv("DOPPLER_PROJECT"))
	config := strings.TrimSpace(os.Getenv("DOPPLER_CONFIG"))
	if project == "" || config == "" {
		return ""
	}
	doppler, err := exec.LookPath("doppler")
	if err != nil {
		return ""
	}
	out, err := exec.Command(doppler, "secrets", "get", envName,
		"--plain", "--project", project, "--config", config).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *Config) owwModelDir() string {
	if dir := strings.TrimSpace(c.Wakeword.OWWModelDir); dir != "" {
		return dir
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "SpeechKit", "models", "wakeword")
}

func (c *Config) owwPhraseModelFile() string {
	if m := strings.TrimSpace(c.Wakeword.OWWModel); m != "" {
		return m
	}
	phrase := strings.ToLower(strings.TrimSpace(c.Wakeword.Phrase))
	return strings.ReplaceAll(phrase, " ", "_") + ".onnx"
}

func (c *Config) owwOnnxRuntime() string {
	if p := strings.TrimSpace(c.Wakeword.OWWOnnxRuntime); p != "" {
		return p
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "SpeechKit", "onnxruntime.dll")
}

func (c *Config) owwThreshold() float32 {
	if c.Wakeword.OWWThreshold > 0 {
		return c.Wakeword.OWWThreshold
	}
	return 0.35
}

func (c *Config) targetMode() string {
	target := strings.ToLower(strings.TrimSpace(c.Mode.Target))
	switch target {
	case "voice_agent", "voice-agent", "agent", "live":
		return "voice_agent"
	case "wake_only", "wake-only", "wake", "kws":
		return "wake_only"
	default:
		return "assist"
	}
}

func (c *Config) cooldown() time.Duration {
	if c.Wakeword.CooldownMS <= 0 {
		return 1500 * time.Millisecond
	}
	return time.Duration(c.Wakeword.CooldownMS) * time.Millisecond
}

func (c *Config) applyAssistDefaults() {
	gatewayBase := strings.TrimRight(strings.TrimSpace(os.Getenv("KOMBIFY_GATEWAY_BASE_URL")), "/")
	if c.Local.Model == "" {
		c.Local.Model = "ggml-small.bin"
	}
	if c.Local.Port <= 0 {
		c.Local.Port = 9000
	}
	if c.Local.GPU == "" {
		c.Local.GPU = "auto"
	}
	if c.LocalLLM.BaseURL == "" {
		c.LocalLLM.BaseURL = "http://127.0.0.1:8082/v1"
	}
	if c.LocalLLM.Model == "" {
		c.LocalLLM.Model = "ggml-org/gemma-4-E2B-it-GGUF:Q8_0"
	}
	if c.LocalLLM.Port <= 0 {
		c.LocalLLM.Port = 8082
	}
	if c.LocalLLM.GPU == "" {
		c.LocalLLM.GPU = "auto"
	}

	if c.STT.Provider == "" {
		c.STT.Provider = "local"
	}
	if c.STT.Model == "" {
		if c.localSTTProvider() {
			c.STT.Model = c.Local.Model
		} else if c.deepgramSTTProvider() {
			c.STT.Model = "nova-3"
		} else {
			c.STT.Model = "whisper-1"
		}
	}
	if c.STT.APIKeyEnv == "" && c.deepgramSTTProvider() {
		c.STT.APIKeyEnv = "DEEPGRAM_API_KEY"
	} else if c.STT.APIKeyEnv == "" && !c.localSTTProvider() {
		c.STT.APIKeyEnv = "KOMBIFY_GATEWAY_TOKEN"
	}
	if c.STT.Language == "" {
		c.STT.Language = "de"
	}
	if gatewayBase != "" && !c.localSTTProvider() && !configuredBaseURL(c.STT.BaseURL) {
		c.STT.BaseURL = gatewayBase
	}

	if c.Assist.Model == "" {
		if c.LocalLLM.Enabled {
			c.Assist.Model = c.LocalLLM.Model
		} else {
			c.Assist.Model = "gpt-4o-mini"
		}
	}
	if c.Assist.APIKeyEnv == "" && !c.localAssistProvider() {
		c.Assist.APIKeyEnv = "KOMBIFY_GATEWAY_TOKEN"
	}
	if c.Assist.SystemPrompt == "" {
		c.Assist.SystemPrompt = "Du bist kombify, ein knapper deutscher Hands-free Companion auf einer kleinen Box. Antworte natuerlich, hilfreich und in ein bis zwei kurzen Saetzen."
	}
	if c.LocalLLM.Enabled && !configuredBaseURL(c.Assist.BaseURL) {
		c.Assist.BaseURL = c.LocalLLM.BaseURL
	}
	if gatewayBase != "" && !c.localAssistProvider() && !configuredBaseURL(c.Assist.BaseURL) {
		c.Assist.BaseURL = gatewayBase
	}

	if c.TTS.Provider == "" {
		c.TTS.Provider = "piper"
	}
	if c.TTS.Model == "" {
		c.TTS.Model = "tts-1"
	}
	if c.TTS.Voice == "" {
		if c.localTTSProvider() {
			c.TTS.Voice = "de_DE-thorsten-medium.onnx"
		} else {
			c.TTS.Voice = "alloy"
		}
	}
	if c.TTS.APIKeyEnv == "" && !c.localTTSProvider() {
		c.TTS.APIKeyEnv = "KOMBIFY_GATEWAY_TOKEN"
	}
	if gatewayBase != "" && !c.localTTSProvider() && !configuredBaseURL(c.TTS.BaseURL) {
		c.TTS.BaseURL = gatewayBase
	}
	if c.TTS.Piper.VoiceDir == "" {
		c.TTS.Piper.VoiceDir = defaultPiperVoiceDir()
	}
}

func (c *Config) sttReady() bool {
	if c.localSTTProvider() {
		return c.resolveWhisperModelPath() != ""
	}
	if c.directCloudSTTProvider() {
		return realSecret(c.sttKey())
	}
	return configuredBaseURL(c.STT.BaseURL) && realSecret(c.sttKey())
}

func (c *Config) assistReady() bool {
	if c.localAssistProvider() {
		return configuredBaseURL(c.Assist.BaseURL)
	}
	return configuredBaseURL(c.Assist.BaseURL) && realSecret(c.assistKey())
}

func (c *Config) ttsReady() bool {
	if c.localTTSProvider() {
		return piperUsable(c.TTS.Piper.Binary, c.TTS.Piper.VoiceDir)
	}
	return configuredBaseURL(c.TTS.BaseURL) && realSecret(c.ttsKey())
}

func configuredBaseURL(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.Contains(value, "REPLACE-WITH")
}

func realSecret(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "dummy"
}

func (c *Config) localSTTProvider() bool {
	return localAuthoritySTTProvider(c.STT.Provider)
}

func (c *Config) deepgramSTTProvider() bool {
	return strings.EqualFold(strings.TrimSpace(c.STT.Provider), "deepgram")
}

func (c *Config) directCloudSTTProvider() bool {
	switch strings.ToLower(strings.TrimSpace(c.STT.Provider)) {
	case "deepgram", "openai", "groq", "assemblyai":
		return true
	default:
		return false
	}
}

func (c *Config) localAssistProvider() bool {
	return c.LocalLLM.Enabled || isLoopbackBaseURL(c.Assist.BaseURL)
}

func (c *Config) localTTSProvider() bool {
	switch strings.ToLower(strings.TrimSpace(c.TTS.Provider)) {
	case "piper", "local", "tts.local.piper":
		return true
	default:
		return false
	}
}

func (c *Config) wakewordBackend() string {
	backend := strings.ToLower(strings.TrimSpace(c.Wakeword.Backend))
	switch backend {
	case "", "sherpa", "sherpa_kws":
		return "sherpa_kws"
	case "openwakeword", "open_wake_word", "livekit_openwakeword":
		// This example keeps the detector local. The SpeechKit desktop config
		// uses this ID for its OpenWakeWord sidecar; the box satellite currently
		// falls back to the self-contained Sherpa path unless that sidecar is
		// wired into the example.
		return "openwakeword"
	default:
		return backend
	}
}
