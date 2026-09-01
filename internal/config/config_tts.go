package config

// Text-to-speech (TTS) provider configuration types.

// TTSConfig configures text-to-speech for Assist Mode.
type TTSConfig struct {
	Enabled     bool           `toml:"enabled"`
	Strategy    string         `toml:"strategy"` // "cloud-first", "local-first", "cloud-only", "local-only"
	Voice       string         `toml:"voice"`    // Global default voice override
	Speed       float64        `toml:"speed"`    // Global speed 0.25-4.0, default 1.0
	Format      string         `toml:"format"`   // "mp3", "wav", "opus", "pcm"
	OpenAI      TTSOpenAI      `toml:"openai"`
	Google      TTSGoogle      `toml:"google"`
	Deepgram    TTSDeepgram    `toml:"deepgram"`
	HuggingFace TTSHuggingFace `toml:"huggingface"`
	Foundry     TTSFoundry     `toml:"foundry"`
	Local       TTSLocal       `toml:"local"`
	Piper       TTSPiper       `toml:"piper"`
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

// TTSDeepgram configures the Deepgram Aura-2 TTS provider. It reuses the
// Deepgram API key from [providers.deepgram] (DEEPGRAM_API_KEY) — no separate
// credential. Model is an Aura-2 voice id like "aura-2-thalia-en".
type TTSDeepgram struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"` // Aura-2 voice id, e.g. "aura-2-thalia-en"
	Voice   string `toml:"voice"` // optional explicit voice override (alias of Model)
}

type TTSHuggingFace struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"` // e.g. "Qwen/Qwen3-TTS-12Hz-1.7B-Base"
}

// TTSFoundry configures Microsoft Foundry TTS over the OpenAI-compatible
// audio/speech surface. It reuses the Foundry API key and project endpoint
// from [providers.foundry] — no separate credential. Model is the deployment
// name (defaults to the [providers.foundry] tts_deployment).
type TTSFoundry struct {
	Enabled bool   `toml:"enabled"`
	Model   string `toml:"model"` // deployment name, e.g. "gpt-4o-mini-tts"
	Voice   string `toml:"voice"` // alloy, echo, fable, onyx, nova, shimmer
}

type TTSLocal struct {
	Enabled   bool   `toml:"enabled"`
	Model     string `toml:"model"`
	ModelPath string `toml:"model_path"`
	Port      int    `toml:"port"`
}

// TTSPiper configures the offline Piper subprocess TTS provider.
// The piper binary must be on PATH (or pointed at via Binary). Voice models
// are NOT bundled — operators run scripts/prepare-piper-voices.{ps1,sh} to
// fetch ONNX voices from rhasspy/piper-voices into VoiceDir.
type TTSPiper struct {
	Enabled  bool   `toml:"enabled"`
	Binary   string `toml:"binary"`    // path to piper executable; empty => "piper" on PATH
	VoiceDir string `toml:"voice_dir"` // directory holding *.onnx voice files
	// DefaultVoices maps a locale short-code ("en", "de", ...) to a voice
	// filename inside VoiceDir. Empty entries fall back to the built-in
	// defaults (en_US-amy-medium.onnx, de_DE-thorsten-medium.onnx).
	DefaultVoices map[string]string `toml:"default_voices"`
	TimeoutSec    int               `toml:"timeout_sec"` // subprocess timeout; 0 => 30 s
}
