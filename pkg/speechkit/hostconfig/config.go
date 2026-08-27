package hostconfig

import "github.com/BurntSushi/toml"

// Mode source values for [ModeSelection.ModeSource]. "local" runs the mode
// against the in-process framework kernel (the default); "server" routes the
// mode through [Config.ServerConnection] to a remote speechkit-server.
const (
	ModeSourceLocal  = "local"
	ModeSourceServer = "server"
)

// Config is the embedder-relevant subset of the SpeechKit TOML configuration.
// It carries exactly the tables [Load] and [ModeSettingsFrom] read; the
// reference desktop app's full configuration file decodes into it losslessly
// for these tables, and unknown tables or keys are simply ignored.
//
// Hosts can obtain a Config three ways: [Load] reads a config.toml with the
// reference app's defaults and compatibility backfills applied, [Parse]
// decodes raw TOML bytes verbatim, and literal construction works for hosts
// that synthesise their configuration programmatically.
type Config struct {
	General          General          `toml:"general"`
	ModelSelection   ModelSelection   `toml:"model_selection"`
	Vocabulary       Vocabulary       `toml:"vocabulary"`
	TTS              TTS              `toml:"tts"`
	VoiceAgent       VoiceAgent       `toml:"voice_agent"`
	ServerConnection ServerConnection `toml:"server_connection"`
}

// General mirrors the embedder-relevant [general] switches: which modes are
// enabled and how they are activated.
type General struct {
	DictateEnabled           bool   `toml:"dictate_enabled"`
	AssistEnabled            bool   `toml:"assist_enabled"`
	VoiceAgentEnabled        bool   `toml:"voice_agent_enabled"`
	DictateHotkey            string `toml:"dictate_hotkey"`
	AssistHotkey             string `toml:"assist_hotkey"`
	VoiceAgentHotkey         string `toml:"voice_agent_hotkey"`
	DictateHotkeyBehavior    string `toml:"dictate_hotkey_behavior"`
	AssistHotkeyBehavior     string `toml:"assist_hotkey_behavior"`
	VoiceAgentHotkeyBehavior string `toml:"voice_agent_hotkey_behavior"`
}

// ModelSelection carries the per-mode provider profile selection.
type ModelSelection struct {
	Dictate    ModeSelection `toml:"dictate"`
	Assist     ModeSelection `toml:"assist"`
	VoiceAgent ModeSelection `toml:"voice_agent"`
}

// ModeSelection pins the provider profile (and optional fallback) one mode
// runs with, and whether the mode executes locally or against a remote
// server.
type ModeSelection struct {
	PrimaryProfileID  string `toml:"primary_profile_id"`
	FallbackProfileID string `toml:"fallback_profile_id"`

	// ModeSource selects whether this mode runs locally (framework kernel
	// in-process, default) or against a remote SpeechKit Server-Target
	// configured under [server_connection]. Empty is treated as
	// [ModeSourceLocal] so a missing TOML field never silently means
	// "server"; read it through [ModeSelection.ResolvedModeSource].
	ModeSource string `toml:"mode_source"`
}

// ResolvedModeSource returns the effective mode source, normalising the
// empty default to [ModeSourceLocal].
func (sel ModeSelection) ResolvedModeSource() string {
	if normalizeLower(sel.ModeSource) == ModeSourceServer {
		return ModeSourceServer
	}
	return ModeSourceLocal
}

// Vocabulary mirrors the embedder-relevant [vocabulary] fields.
type Vocabulary struct {
	Dictionary string `toml:"dictionary"`
}

// TTS mirrors the embedder-relevant [tts] switch. Provider-level TTS detail
// stays host-owned; the mode mapping only needs to know whether spoken
// output is on.
type TTS struct {
	Enabled bool `toml:"enabled"`
}

// VoiceAgent mirrors the embedder-relevant [voice_agent] fields.
type VoiceAgent struct {
	EnableSessionSummary bool   `toml:"enable_session_summary"`
	PipelineFallback     bool   `toml:"pipeline_fallback"`
	CloseBehavior        string `toml:"close_behavior"`
	AgentProfileID       string `toml:"agent_profile_id"`
	AgentSequenceID      string `toml:"agent_sequence_id"`
}

// ServerConnection mirrors the [server_connection] table. The bearer token
// value is never part of configuration — only the env var name is carried.
type ServerConnection struct {
	Enabled              bool   `toml:"enabled"`
	URL                  string `toml:"url"`
	BearerTokenEnv       string `toml:"bearer_token_env"`
	AuthMode             string `toml:"auth_mode"`
	BetaInstallIDEnv     string `toml:"beta_install_id_env"`
	BetaInstallSecretEnv string `toml:"beta_install_secret_env"`
	FallbackToLocal      bool   `toml:"fallback_to_local"`
	RequestTimeoutSec    int    `toml:"request_timeout_sec"`
}

// Parse decodes raw TOML bytes into a [Config]. Unknown tables and keys are
// ignored, so the reference app's full config.toml parses cleanly.
//
// Parse decodes the file verbatim: it does not apply the reference app's
// defaults, legacy-field backfills, or registry policy overlay — use [Load]
// for full compatibility with configs written by the desktop app. The
// embedder-relevant normalisation (profile-ID canonicalisation, default
// primary profiles, mode-source and auth-mode fallbacks) is applied later by
// [ModeSettingsFrom] and [PolicyFrom] either way.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
