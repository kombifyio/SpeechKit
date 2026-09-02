// Package hostconfig turns a SpeechKit TOML configuration file into the public
// SDK types an embedding host drives the framework with: a
// [speechkit.ModeSettings] (which modes are on, their hotkeys and selected
// provider profiles) and a permissive [speechkit.RuntimePolicy] (which modes
// the host exposes and whether fallbacks are allowed).
//
// Without this package a library host has to build ModeSettings by hand from a
// parsed config, the way the reference Windows app does internally. [Load] does
// that wiring once so a host can go from a config.toml on disk to a
// ready-to-validate ModeSettings/RuntimePolicy pair in a single call:
//
//	settings, policy, err := hostconfig.Load("config.toml")
//
// Hosts that load or synthesise configuration themselves construct a public
// [Config] (directly, or from raw TOML via [Parse]) and hand it to
// [ModeSettingsFrom] and [PolicyFrom].
//
// The conversion is intentionally kernel-clean. It maps the embedder-relevant
// fields only; the reference desktop app additionally introspects the host
// secret store to report whether a server bearer token env var is set, which is
// a device-UI concern and is deliberately left out here.
//
// Boundary note: this package owns the loader semantics for the tables it
// reads — [Defaults], the legacy-field backfills and value normalisation in
// [Normalize]. The reference desktop app's internal config loader delegates to
// the same functions for those fields, so [Load] and the desktop app agree by
// construction rather than by a parallel reimplementation. The package imports
// no internal/* code.
package hostconfig

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Load reads and decodes the SpeechKit TOML config at path over [Defaults],
// applies [Normalize], and returns the host-facing ModeSettings together with
// a RuntimePolicy derived from it. A missing file yields the defaults; a
// malformed file returns an error wrapping [ErrMalformedConfig]. Use
// [LoadConfig] to keep the intermediate [Config]. Unlike the desktop app,
// an empty path is not resolved to the app's per-user config location; the
// host decides where its configuration lives.
//
// The returned policy enables exactly the modes turned on in config and allows
// fallback profiles only when the config actually pins one. It leaves
// AllowedProfiles and FixedProfiles empty (meaning "all") so the host can
// tighten the policy further before calling
// [speechkit.ValidateModeSettingsForPolicy].
func Load(path string) (speechkit.ModeSettings, speechkit.RuntimePolicy, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return speechkit.ModeSettings{}, speechkit.RuntimePolicy{}, err
	}
	return ModeSettingsFrom(cfg), PolicyFrom(cfg), nil
}

// ModeSettingsFrom converts a [Config] into the public ModeSettings shape,
// applying the embedder-relevant normalisation: profile IDs are canonicalised,
// an empty primary selection falls back to the built-in default profile for
// that mode, a fallback identical to its primary is dropped, and mode source
// and auth mode normalise their documented defaults. A nil cfg yields the
// zero ModeSettings.
func ModeSettingsFrom(cfg *Config) speechkit.ModeSettings {
	if cfg == nil {
		return speechkit.ModeSettings{}
	}

	dictate := normalizeSelection(cfg.ModelSelection.Dictate)
	if dictate.PrimaryProfileID == "" {
		dictate.PrimaryProfileID = DefaultDictatePrimaryProfileID
	}
	assist := normalizeSelection(cfg.ModelSelection.Assist)
	if assist.PrimaryProfileID == "" {
		assist.PrimaryProfileID = DefaultAssistPrimaryProfileID
	}
	voice := normalizeSelection(cfg.ModelSelection.VoiceAgent)
	if voice.PrimaryProfileID == "" {
		voice.PrimaryProfileID = DefaultVoiceAgentPrimaryProfileID
	}

	return speechkit.ModeSettings{
		Dictation: speechkit.DictationSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           cfg.General.DictateEnabled,
				Hotkey:            cfg.General.DictateHotkey,
				HotkeyBehavior:    cfg.General.DictateHotkeyBehavior,
				PrimaryProfileID:  dictate.PrimaryProfileID,
				FallbackProfileID: dictate.FallbackProfileID,
				ModeSource:        dictate.ResolvedModeSource(),
			},
			DictionaryEnabled: strings.TrimSpace(cfg.Vocabulary.Dictionary) != "",
		},
		Assist: speechkit.AssistSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           cfg.General.AssistEnabled,
				Hotkey:            cfg.General.AssistHotkey,
				HotkeyBehavior:    cfg.General.AssistHotkeyBehavior,
				PrimaryProfileID:  assist.PrimaryProfileID,
				FallbackProfileID: assist.FallbackProfileID,
				ModeSource:        assist.ResolvedModeSource(),
			},
			TTSEnabled:      cfg.TTS.Enabled,
			UtilityRegistry: "default",
		},
		VoiceAgent: speechkit.VoiceAgentSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           cfg.General.VoiceAgentEnabled,
				Hotkey:            cfg.General.VoiceAgentHotkey,
				HotkeyBehavior:    cfg.General.VoiceAgentHotkeyBehavior,
				PrimaryProfileID:  voice.PrimaryProfileID,
				FallbackProfileID: voice.FallbackProfileID,
				ModeSource:        voice.ResolvedModeSource(),
			},
			SessionSummary:   cfg.VoiceAgent.EnableSessionSummary,
			PipelineFallback: cfg.VoiceAgent.PipelineFallback,
			CloseBehavior: NormalizeVoiceAgentCloseBehavior(
				cfg.VoiceAgent.CloseBehavior,
				VoiceAgentCloseBehaviorContinue,
			),
			// Carried through as configured (Normalize fills an empty id with
			// DefaultVoiceAgentProfileID). Which ids exist is the host's
			// behaviour catalog; a Companion persona is not collapsed here.
			AgentProfileID:  strings.TrimSpace(cfg.VoiceAgent.AgentProfileID),
			AgentSequenceID: strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID),
		},
		ServerConnection: serverConnectionFrom(cfg.ServerConnection),
	}
}

// PolicyFrom derives a permissive RuntimePolicy from a [Config]: it enables
// the modes turned on in [general] and allows fallbacks only if any mode pins
// a fallback profile. AllowedProfiles and FixedProfiles are left empty
// (meaning "all"), so a host can lock the policy down further. A nil cfg
// yields the zero RuntimePolicy.
//
// Note: if config enables no modes at all, EnabledModes is empty, which
// SpeechKit treats as "all modes enabled". Set an explicit policy if you need a
// hard mode lockout.
func PolicyFrom(cfg *Config) speechkit.RuntimePolicy {
	if cfg == nil {
		return speechkit.RuntimePolicy{}
	}

	var modes []speechkit.Mode
	if cfg.General.DictateEnabled {
		modes = append(modes, speechkit.ModeDictation)
	}
	if cfg.General.AssistEnabled {
		modes = append(modes, speechkit.ModeAssist)
	}
	if cfg.General.VoiceAgentEnabled {
		modes = append(modes, speechkit.ModeVoiceAgent)
	}

	allowFallbacks := normalizeSelection(cfg.ModelSelection.Dictate).FallbackProfileID != "" ||
		normalizeSelection(cfg.ModelSelection.Assist).FallbackProfileID != "" ||
		normalizeSelection(cfg.ModelSelection.VoiceAgent).FallbackProfileID != ""

	return speechkit.RuntimePolicy{
		EnabledModes:   modes,
		AllowFallbacks: allowFallbacks,
	}
}

// normalizeSelection trims and canonicalises both profile IDs and drops the
// fallback when it is identical to the primary. It mirrors the device app's
// profiles.NormalizeModeSelection without pulling in the desktop adapter.
func normalizeSelection(sel ModeSelection) ModeSelection {
	sel.PrimaryProfileID = speechkit.NormalizeProviderProfileID(sel.PrimaryProfileID)
	sel.FallbackProfileID = speechkit.NormalizeProviderProfileID(sel.FallbackProfileID)
	if sel.PrimaryProfileID != "" && sel.PrimaryProfileID == sel.FallbackProfileID {
		sel.FallbackProfileID = ""
	}
	return sel
}

// serverConnectionFrom maps the [server_connection] table into the public
// surface. The bearer token value is never read from config — only the env var
// name is carried — and the device-only "is the token env var set" booleans and
// per-target token introspection are intentionally omitted.
func serverConnectionFrom(cfg ServerConnection) speechkit.ServerConnectionSetting {
	return speechkit.ServerConnectionSetting{
		Enabled:              cfg.Enabled,
		URL:                  cfg.URL,
		BearerTokenEnv:       cfg.BearerTokenEnv,
		AuthMode:             NormalizeServerConnectionAuthMode(cfg.AuthMode),
		BetaInstallIDEnv:     cfg.BetaInstallIDEnv,
		BetaInstallSecretEnv: cfg.BetaInstallSecretEnv,
		FallbackToLocal:      cfg.FallbackToLocal,
		RequestTimeoutSec:    cfg.RequestTimeoutSec,
	}
}

func normalizeLower(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
