package hostconfig

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// Canonical values for the embedder-relevant enumerations. The reference
// desktop app and the server read the same constants through this package, so
// a TOML value is normalised identically wherever it is loaded.
const (
	// HotkeyBehaviorHoldToTalk is "hold the shortcut while you speak, release
	// to end". The historical push_to_talk spelling is accepted as an alias.
	HotkeyBehaviorHoldToTalk = "hold_to_talk"
	HotkeyBehaviorToggle     = "toggle"

	legacyHotkeyBehaviorPushToTalk = "push_to_talk"

	VoiceAgentCloseBehaviorContinue = "continue"
	VoiceAgentCloseBehaviorNewChat  = "new_chat"

	ServerConnectionAuthModeBearer = "bearer"
	ServerConnectionAuthModeAPIKey = "api_key"
	// ServerConnectionAuthModeEdgeBeta sends no shared server credential; the
	// client identifies the installation to a managed edge broker with
	// per-install headers instead.
	ServerConnectionAuthModeEdgeBeta = "edge_beta"

	DefaultBearerTokenEnv       = "SPEECHKIT_SERVER_TOKEN" //nolint:gosec // env var name, not a credential
	DefaultBetaInstallIDEnv     = "SPEECHKIT_BETA_INSTALL_ID"
	DefaultBetaInstallSecretEnv = "SPEECHKIT_BETA_INSTALL_SECRET"

	// Default*PrimaryProfileID are the fresh-install selections per mode. All
	// three resolve to the local-only path, so a config without
	// [model_selection] runs with zero cloud keys.
	DefaultDictatePrimaryProfileID    = "stt.local.whispercpp"
	DefaultAssistPrimaryProfileID     = "assist.builtin.gemma4-e4b"
	DefaultVoiceAgentPrimaryProfileID = "realtime.builtin.pipeline"

	DefaultDictateHotkey    = "ctrl+win"
	DefaultAssistHotkey     = "win+alt"
	DefaultVoiceAgentHotkey = "ctrl+shift"

	// DefaultVoiceAgentProfileID is the built-in voice-agent behaviour profile
	// an empty agent_profile_id resolves to. Non-empty ids pass through
	// unchanged; which ids exist is the host's behaviour catalog.
	DefaultVoiceAgentProfileID = "default"
)

// ErrMalformedConfig wraps TOML decode failures from Load.
var ErrMalformedConfig = errors.New("hostconfig: malformed config")

// Defaults returns the shipped defaults for the embedder-relevant tables:
// dictation on with the local Whisper profile, assist and voice agent off,
// hold-to-talk hotkeys, TTS on, session summary on, server connection off
// with bearer auth. It is the starting point Load decodes a file over.
func Defaults() *Config {
	return &Config{
		General: General{
			DictateEnabled:           true,
			DictateHotkey:            DefaultDictateHotkey,
			AssistHotkey:             DefaultAssistHotkey,
			VoiceAgentHotkey:         DefaultVoiceAgentHotkey,
			DictateHotkeyBehavior:    HotkeyBehaviorHoldToTalk,
			AssistHotkeyBehavior:     HotkeyBehaviorHoldToTalk,
			VoiceAgentHotkeyBehavior: HotkeyBehaviorHoldToTalk,
		},
		ModelSelection: ModelSelection{
			Dictate:    ModeSelection{PrimaryProfileID: DefaultDictatePrimaryProfileID, ModeSource: ModeSourceLocal},
			Assist:     ModeSelection{PrimaryProfileID: DefaultAssistPrimaryProfileID, ModeSource: ModeSourceLocal},
			VoiceAgent: ModeSelection{PrimaryProfileID: DefaultVoiceAgentPrimaryProfileID, ModeSource: ModeSourceLocal},
		},
		TTS: TTS{Enabled: true},
		VoiceAgent: VoiceAgent{
			EnableSessionSummary: true,
			CloseBehavior:        VoiceAgentCloseBehaviorContinue,
			AgentProfileID:       DefaultVoiceAgentProfileID,
		},
		ServerConnection: ServerConnection{
			BearerTokenEnv:       DefaultBearerTokenEnv,
			AuthMode:             ServerConnectionAuthModeBearer,
			BetaInstallIDEnv:     DefaultBetaInstallIDEnv,
			BetaInstallSecretEnv: DefaultBetaInstallSecretEnv,
			FallbackToLocal:      true,
			RequestTimeoutSec:    30,
		},
	}
}

// LegacyGeneral carries the removed [general] keys older config files may
// still set. Normalize folds them into the current fields; they are never
// part of Config itself.
type LegacyGeneral struct {
	Hotkey      string `toml:"hotkey"`
	AgentHotkey string `toml:"agent_hotkey"`
	AgentMode   string `toml:"agent_mode"`
	HotkeyMode  string `toml:"hotkey_mode"`
}

// KeyDefined reports whether the given TOML key path was present in the
// decoded document (as opposed to filled by defaults). Load passes
// toml.MetaData.IsDefined; hosts that decode elsewhere pass their own.
type KeyDefined func(keys ...string) bool

// Normalize applies the shared legacy backfills and value normalisation to
// cfg in place. It is the single definition of "what a decoded config means"
// for the embedder-relevant tables; Load calls it, and the reference app's
// internal loader delegates to it for the same fields.
//
// defined answers whether a key was explicitly present; nil is treated as
// "everything was defined", which disables the presence-dependent backfills.
// legacy supplies the removed [general] keys (zero value when none).
func Normalize(cfg *Config, defined KeyDefined, legacy LegacyGeneral) {
	if cfg == nil {
		return
	}
	if defined == nil {
		defined = func(...string) bool { return true }
	}

	normalizeGeneral(&cfg.General, defined, legacy)
	normalizeModeSelection(&cfg.ModelSelection.Dictate)
	normalizeModeSelection(&cfg.ModelSelection.Assist)
	normalizeModeSelection(&cfg.ModelSelection.VoiceAgent)

	if !defined("voice_agent", "enable_session_summary") {
		cfg.VoiceAgent.EnableSessionSummary = true
	}
	cfg.VoiceAgent.CloseBehavior = NormalizeVoiceAgentCloseBehavior(cfg.VoiceAgent.CloseBehavior, VoiceAgentCloseBehaviorContinue)
	cfg.VoiceAgent.AgentProfileID = strings.TrimSpace(cfg.VoiceAgent.AgentProfileID)
	if cfg.VoiceAgent.AgentProfileID == "" {
		cfg.VoiceAgent.AgentProfileID = DefaultVoiceAgentProfileID
	}
	cfg.VoiceAgent.AgentSequenceID = strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID)

	normalizeServerConnection(&cfg.ServerConnection, defined)
}

func normalizeGeneral(g *General, defined KeyDefined, legacy LegacyGeneral) {
	if strings.TrimSpace(g.DictateHotkey) == "" {
		g.DictateHotkey = strings.TrimSpace(legacy.Hotkey)
	}
	if strings.TrimSpace(g.DictateHotkey) == "" {
		g.DictateHotkey = DefaultDictateHotkey
	}

	legacyAgentHotkey := strings.TrimSpace(legacy.AgentHotkey)
	legacyAgentMode := strings.TrimSpace(legacy.AgentMode)
	if legacyAgentMode != "voice_agent" {
		legacyAgentMode = "assist"
	}
	if !defined("general", "assist_hotkey") && strings.TrimSpace(g.AssistHotkey) == "" && legacyAgentMode == "assist" {
		g.AssistHotkey = legacyAgentHotkey
	}
	if !defined("general", "voice_agent_hotkey") && strings.TrimSpace(g.VoiceAgentHotkey) == "" && legacyAgentMode == "voice_agent" {
		g.VoiceAgentHotkey = legacyAgentHotkey
	}
	g.AssistHotkey = strings.TrimSpace(g.AssistHotkey)
	g.VoiceAgentHotkey = strings.TrimSpace(g.VoiceAgentHotkey)

	// Pre-v0.30 installs shipped dictate/assist swapped; move them onto the
	// current defaults when the file still carries that exact old triple.
	if sameCombo(g.DictateHotkey, DefaultAssistHotkey) &&
		sameCombo(g.AssistHotkey, DefaultDictateHotkey) &&
		sameCombo(g.VoiceAgentHotkey, DefaultVoiceAgentHotkey) {
		g.DictateHotkey = DefaultDictateHotkey
		g.AssistHotkey = DefaultAssistHotkey
		g.VoiceAgentHotkey = DefaultVoiceAgentHotkey
	}

	if !defined("general", "dictate_enabled") {
		g.DictateEnabled = strings.TrimSpace(g.DictateHotkey) != ""
	}
	if !defined("general", "assist_enabled") {
		g.AssistEnabled = strings.TrimSpace(g.AssistHotkey) != ""
	}
	if !defined("general", "voice_agent_enabled") {
		g.VoiceAgentEnabled = strings.TrimSpace(g.VoiceAgentHotkey) != ""
	}

	legacyBehavior := NormalizeHotkeyBehavior(legacy.HotkeyMode, HotkeyBehaviorHoldToTalk)
	legacyBehaviorDefined := defined("general", "hotkey_mode")
	if legacyBehaviorDefined && !defined("general", "dictate_hotkey_behavior") {
		g.DictateHotkeyBehavior = legacyBehavior
	}
	if legacyBehaviorDefined && !defined("general", "assist_hotkey_behavior") {
		g.AssistHotkeyBehavior = legacyBehavior
	}
	if legacyBehaviorDefined && !defined("general", "voice_agent_hotkey_behavior") {
		g.VoiceAgentHotkeyBehavior = legacyBehavior
	}
	g.DictateHotkeyBehavior = NormalizeHotkeyBehavior(g.DictateHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	g.AssistHotkeyBehavior = NormalizeHotkeyBehavior(g.AssistHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	g.VoiceAgentHotkeyBehavior = NormalizeHotkeyBehavior(g.VoiceAgentHotkeyBehavior, HotkeyBehaviorHoldToTalk)
}

func normalizeModeSelection(sel *ModeSelection) {
	if strings.TrimSpace(sel.ModeSource) == "" {
		sel.ModeSource = ModeSourceLocal
	}
}

func normalizeServerConnection(sc *ServerConnection, defined KeyDefined) {
	if defined("server_connection", "url") &&
		strings.TrimRight(strings.TrimSpace(sc.URL), "/") != "" &&
		!defined("server_connection", "auth_mode") &&
		NormalizeServerConnectionAuthMode(sc.AuthMode) == ServerConnectionAuthModeAPIKey {
		// A custom URL without an explicit auth mode is a self-hosted server,
		// which speaks bearer; api_key only applies to the managed target.
		sc.AuthMode = ServerConnectionAuthModeBearer
	}
	sc.AuthMode = NormalizeServerConnectionAuthMode(sc.AuthMode)
}

func sameCombo(value, combo string) bool {
	return strings.EqualFold(strings.TrimSpace(value), combo)
}

// NormalizeHotkeyBehavior canonicalises a hotkey behavior value, accepting
// the legacy push_to_talk alias. Unknown values resolve to fallback, and an
// unknown fallback (or one equal to the unknown value) to hold-to-talk.
func NormalizeHotkeyBehavior(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case HotkeyBehaviorHoldToTalk, legacyHotkeyBehaviorPushToTalk:
		return HotkeyBehaviorHoldToTalk
	case HotkeyBehaviorToggle:
		return HotkeyBehaviorToggle
	default:
		if strings.TrimSpace(fallback) == "" {
			return HotkeyBehaviorHoldToTalk
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return HotkeyBehaviorHoldToTalk
		}
		return NormalizeHotkeyBehavior(fallback, "")
	}
}

// NormalizeVoiceAgentCloseBehavior canonicalises what happens to the
// conversation when the voice agent window closes.
func NormalizeVoiceAgentCloseBehavior(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VoiceAgentCloseBehaviorContinue:
		return VoiceAgentCloseBehaviorContinue
	case VoiceAgentCloseBehaviorNewChat:
		return VoiceAgentCloseBehaviorNewChat
	default:
		if strings.TrimSpace(fallback) == "" {
			return VoiceAgentCloseBehaviorContinue
		}
		if strings.EqualFold(strings.TrimSpace(fallback), value) {
			return VoiceAgentCloseBehaviorContinue
		}
		return NormalizeVoiceAgentCloseBehavior(fallback, "")
	}
}

// NormalizeServerConnectionAuthMode canonicalises the [server_connection]
// auth mode; anything unrecognised is bearer.
func NormalizeServerConnectionAuthMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case ServerConnectionAuthModeAPIKey:
		return ServerConnectionAuthModeAPIKey
	case ServerConnectionAuthModeEdgeBeta:
		return ServerConnectionAuthModeEdgeBeta
	default:
		return ServerConnectionAuthModeBearer
	}
}

// LoadConfig reads the TOML file at path over Defaults and applies Normalize.
// A missing file yields the normalised defaults; a file that fails to decode
// returns an error wrapping ErrMalformedConfig rather than silently falling
// back — a library host should see a broken config, not run on defaults.
func LoadConfig(path string) (*Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path) // #nosec G304 -- path is the host-supplied config location.
	if err != nil {
		if os.IsNotExist(err) {
			Normalize(cfg, nil, LegacyGeneral{})
			return cfg, nil
		}
		return nil, fmt.Errorf("hostconfig: read config: %w", err)
	}
	return decodeConfig(cfg, string(data))
}

func decodeConfig(cfg *Config, data string) (*Config, error) {
	meta, err := toml.Decode(data, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedConfig, err)
	}
	var legacy struct {
		General LegacyGeneral `toml:"general"`
	}
	if _, err := toml.Decode(data, &legacy); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedConfig, err)
	}
	Normalize(cfg, meta.IsDefined, legacy.General)
	return cfg, nil
}
