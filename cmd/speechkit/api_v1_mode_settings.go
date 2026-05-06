package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type apiV1ModeSettingsPatch struct {
	Enabled           *bool   `json:"enabled"`
	Hotkey            *string `json:"hotkey"`
	HotkeyBehavior    *string `json:"hotkeyBehavior"`
	PrimaryProfileID  *string `json:"primaryProfileId"`
	FallbackProfileID *string `json:"fallbackProfileId"`
	DictionaryEnabled *bool   `json:"dictionaryEnabled"`
	TTSEnabled        *bool   `json:"ttsEnabled"`
	SessionSummary    *bool   `json:"sessionSummary"`
	PipelineFallback  *bool   `json:"pipelineFallback"`
	CloseBehavior     *string `json:"closeBehavior"`
	AgentProfileID    *string `json:"agentProfileId"`
	AgentSequenceID   *string `json:"agentSequenceId"`
	// ModeSource gates per-mode local-vs-server execution. Accepted
	// values are "local" and "server"; anything else is treated as
	// "local" so rogue clients can't accidentally send remote calls.
	ModeSource *string `json:"modeSource"`
}

type apiV1UserError string

func (e apiV1UserError) Error() string {
	return string(e)
}

type apiV1ModeSettingsSnapshot struct {
	dictateEnabled       bool
	assistEnabled        bool
	voiceAgentEnabled    bool
	dictateHotkey        string
	assistHotkey         string
	voiceAgentHotkey     string
	voiceAgentProfileID  string
	voiceAgentSequenceID string
	audioDeviceID        string
	overlayEnabled       bool
}

func handleAPIV1ModeSettings(w http.ResponseWriter, r *http.Request, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, mode string) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, apiV1SingleModeSetting(cfg, mode))
	case http.MethodPatch:
		var patch apiV1ModeSettingsPatch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := applyAPIV1ModeSettingsPatch(r.Context(), cfgPath, cfg, state, sttRouter, mode, patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, apiV1SingleModeSetting(cfg, mode))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func applyAPIV1ModeSettingsPatch(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, mode string, patch apiV1ModeSettingsPatch) error {
	if cfg == nil {
		return errors.New("config unavailable")
	}
	snapshot := captureAPIV1ModeSettingsSnapshot(cfg)

	if err := applyAPIV1ModeBindingPatch(cfg, mode, patch); err != nil {
		return err
	}
	modelSelectionChanged, err := applyAPIV1ModeSelectionPatch(cfg, mode, patch)
	if err != nil {
		return err
	}
	applyAPIV1ModeSpecificPatch(cfg, mode, patch)
	if modelSelectionChanged {
		cfg.ServerConnection.Enabled = anyServerModeSelected(cfg)
		if cfg.ServerConnection.Enabled {
			config.ApplyManagedDevServerDefaults(cfg)
		}
	}

	if !validateDistinctModeHotkeys(cfg.General.DictateEnabled, cfg.General.AssistEnabled, cfg.General.VoiceAgentEnabled, cfg.General.DictateHotkey, cfg.General.AssistHotkey, cfg.General.VoiceAgentHotkey) {
		return apiV1UserError(msgDuplicateHotkeys)
	}
	cfg.General.ActiveMode = sanitizeActiveModeForBindings(
		cfg.General.ActiveMode,
		cfg.General.AgentMode,
		cfg.General.DictateEnabled,
		cfg.General.AssistEnabled,
		cfg.General.VoiceAgentEnabled,
		cfg.General.DictateHotkey,
		cfg.General.AssistHotkey,
		cfg.General.VoiceAgentHotkey,
	)
	cfg.General.AgentMode = deriveLegacyAgentModeFromBindings(cfg.General.AssistHotkey, cfg.General.VoiceAgentHotkey, cfg.General.ActiveMode, cfg.General.AgentMode)
	cfg.General.AgentHotkey = legacyAgentHotkeyFromModeBindings(cfg.General.AssistHotkey, cfg.General.VoiceAgentHotkey, cfg.General.AgentMode)

	if err := refreshProviderRuntimes(ctx, cfg, state, sttRouter); err != nil {
		return err
	}
	voiceAgentProfileChanged := snapshot.voiceAgentProfileID != voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID)
	voiceAgentSequenceChanged := snapshot.voiceAgentSequenceID != strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID)
	if modelSelectionChanged || voiceAgentProfileChanged || voiceAgentSequenceChanged {
		if err := refreshServerDelegates(cfg, state); err != nil {
			return err
		}
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	if state != nil {
		state.applyRuntimeSettings(
			cfg.General.DictateEnabled,
			cfg.General.AssistEnabled,
			cfg.General.VoiceAgentEnabled,
			cfg.General.DictateHotkey,
			cfg.General.AssistHotkey,
			cfg.General.VoiceAgentHotkey,
			cfg.General.DictateHotkeyBehavior,
			cfg.General.AssistHotkeyBehavior,
			cfg.General.VoiceAgentHotkeyBehavior,
			cfg.General.ActiveMode,
			cfg.Audio.DeviceID,
			runtimeAvailableProviders(ctx, sttRouter),
			cfg.UI.Visualizer,
			cfg.UI.Design,
			cfg.UI.AssistOverlayMode,
			cfg.UI.VoiceAgentOverlayMode,
			cfg.UI.OverlayPosition,
			cfg.Vocabulary.Dictionary,
			cfg.UI.OverlayMovable,
			cfg.UI.OverlayFreeX,
			cfg.UI.OverlayFreeY,
			cfg.UI.OverlayMonitorPositions,
		)
		if voiceAgentProfileChanged || voiceAgentSequenceChanged {
			resetInactiveVoiceAgentSession(state)
		}
		state.applyDesktopSettings(
			snapshot.dictateEnabled,
			snapshot.assistEnabled,
			snapshot.voiceAgentEnabled,
			snapshot.dictateHotkey,
			snapshot.assistHotkey,
			snapshot.voiceAgentHotkey,
			cfg.General.DictateEnabled,
			cfg.General.AssistEnabled,
			cfg.General.VoiceAgentEnabled,
			cfg.General.DictateHotkey,
			cfg.General.AssistHotkey,
			cfg.General.VoiceAgentHotkey,
			snapshot.audioDeviceID,
			cfg.Audio.DeviceID,
			snapshot.overlayEnabled,
		)
	}
	return nil
}

func captureAPIV1ModeSettingsSnapshot(cfg *config.Config) apiV1ModeSettingsSnapshot {
	return apiV1ModeSettingsSnapshot{
		dictateEnabled:       cfg.General.DictateEnabled,
		assistEnabled:        cfg.General.AssistEnabled,
		voiceAgentEnabled:    cfg.General.VoiceAgentEnabled,
		dictateHotkey:        cfg.General.DictateHotkey,
		assistHotkey:         cfg.General.AssistHotkey,
		voiceAgentHotkey:     cfg.General.VoiceAgentHotkey,
		voiceAgentProfileID:  voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID),
		voiceAgentSequenceID: strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID),
		audioDeviceID:        cfg.Audio.DeviceID,
		overlayEnabled:       cfg.UI.OverlayEnabled,
	}
}

func applyAPIV1ModeBindingPatch(cfg *config.Config, mode string, patch apiV1ModeSettingsPatch) error {
	if patch.Enabled != nil {
		switch mode {
		case modeDictate:
			cfg.General.DictateEnabled = *patch.Enabled
		case modeAssist:
			cfg.General.AssistEnabled = *patch.Enabled
		case modeVoiceAgent:
			cfg.General.VoiceAgentEnabled = *patch.Enabled
		}
	}
	if patch.Hotkey != nil {
		hotkey := strings.TrimSpace(*patch.Hotkey)
		if hotkey != "" {
			if _, err := parseModeHotkey(hotkey); err != nil {
				return apiV1UserError(msgUnsupportedModeHotkey)
			}
		}
		switch mode {
		case modeDictate:
			cfg.General.DictateHotkey = hotkey
			cfg.General.Hotkey = hotkey
		case modeAssist:
			cfg.General.AssistHotkey = hotkey
		case modeVoiceAgent:
			cfg.General.VoiceAgentHotkey = hotkey
		}
	}
	if patch.HotkeyBehavior != nil {
		behavior := config.NormalizeHotkeyBehavior(*patch.HotkeyBehavior, config.HotkeyBehaviorPushToTalk)
		switch mode {
		case modeDictate:
			cfg.General.DictateHotkeyBehavior = behavior
			cfg.General.HotkeyMode = behavior
		case modeAssist:
			cfg.General.AssistHotkeyBehavior = behavior
		case modeVoiceAgent:
			cfg.General.VoiceAgentHotkeyBehavior = behavior
		}
	}
	return nil
}

func applyAPIV1ModeSelectionPatch(cfg *config.Config, mode string, patch apiV1ModeSettingsPatch) (bool, error) {
	if patch.PrimaryProfileID == nil && patch.FallbackProfileID == nil && patch.ModeSource == nil {
		return false, nil
	}
	selection := modeSelectionForMode(cfg, mode)
	if patch.PrimaryProfileID != nil {
		selection.PrimaryProfileID = strings.TrimSpace(*patch.PrimaryProfileID)
	}
	if patch.FallbackProfileID != nil {
		selection.FallbackProfileID = strings.TrimSpace(*patch.FallbackProfileID)
	}
	if patch.ModeSource != nil {
		selection.ModeSource = normaliseModeSourcePatch(*patch.ModeSource)
	}
	selection = normalizeModeSelection(selection)
	if err := validateModeSelection(cfg, filteredModelCatalog(), mode, selection); err != nil {
		return false, err
	}
	switch mode {
	case modeDictate:
		cfg.ModelSelection.Dictate = selection
	case modeAssist:
		cfg.ModelSelection.Assist = selection
	case modeVoiceAgent:
		cfg.ModelSelection.VoiceAgent = selection
	}
	return true, nil
}

func applyAPIV1ModeSpecificPatch(cfg *config.Config, mode string, patch apiV1ModeSettingsPatch) {
	if mode == modeDictate && patch.DictionaryEnabled != nil && !*patch.DictionaryEnabled {
		cfg.Vocabulary.Dictionary = ""
	}
	if mode == modeAssist && patch.TTSEnabled != nil {
		cfg.TTS.Enabled = *patch.TTSEnabled
	}
	if mode != modeVoiceAgent {
		return
	}
	if patch.SessionSummary != nil {
		cfg.VoiceAgent.EnableSessionSummary = *patch.SessionSummary
	}
	if patch.PipelineFallback != nil {
		cfg.VoiceAgent.PipelineFallback = *patch.PipelineFallback
	}
	if patch.CloseBehavior != nil {
		cfg.VoiceAgent.CloseBehavior = config.NormalizeVoiceAgentCloseBehavior(*patch.CloseBehavior, config.VoiceAgentCloseBehaviorContinue)
	}
	if patch.AgentProfileID != nil {
		cfg.VoiceAgent.AgentProfileID = voiceagentprofile.NormalizeID(*patch.AgentProfileID)
	}
	if patch.AgentSequenceID != nil {
		cfg.VoiceAgent.AgentSequenceID = strings.TrimSpace(*patch.AgentSequenceID)
	}
}

func apiV1ModeSettingsFromConfig(cfg *config.Config) speechkit.ModeSettings {
	dictateSelection := normalizeModeSelection(cfg.ModelSelection.Dictate)
	if dictateSelection.PrimaryProfileID == "" {
		dictateSelection.PrimaryProfileID = config.DefaultDictatePrimaryProfileID
	}
	assistSelection := normalizeModeSelection(cfg.ModelSelection.Assist)
	if assistSelection.PrimaryProfileID == "" {
		assistSelection.PrimaryProfileID = config.DefaultAssistPrimaryProfileID
	}
	voiceSelection := normalizeModeSelection(cfg.ModelSelection.VoiceAgent)
	if voiceSelection.PrimaryProfileID == "" {
		voiceSelection.PrimaryProfileID = config.DefaultVoiceAgentPrimaryProfileID
	}
	return speechkit.ModeSettings{
		Dictation: speechkit.DictationSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           cfg.General.DictateEnabled,
				Hotkey:            cfg.General.DictateHotkey,
				HotkeyBehavior:    cfg.General.DictateHotkeyBehavior,
				PrimaryProfileID:  dictateSelection.PrimaryProfileID,
				FallbackProfileID: dictateSelection.FallbackProfileID,
				ModeSource:        dictateSelection.ResolvedModeSource(),
			},
			DictionaryEnabled: strings.TrimSpace(cfg.Vocabulary.Dictionary) != "",
		},
		Assist: speechkit.AssistSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           cfg.General.AssistEnabled,
				Hotkey:            cfg.General.AssistHotkey,
				HotkeyBehavior:    cfg.General.AssistHotkeyBehavior,
				PrimaryProfileID:  assistSelection.PrimaryProfileID,
				FallbackProfileID: assistSelection.FallbackProfileID,
				ModeSource:        assistSelection.ResolvedModeSource(),
			},
			TTSEnabled:      cfg.TTS.Enabled,
			UtilityRegistry: "default",
		},
		VoiceAgent: speechkit.VoiceAgentSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           cfg.General.VoiceAgentEnabled,
				Hotkey:            cfg.General.VoiceAgentHotkey,
				HotkeyBehavior:    cfg.General.VoiceAgentHotkeyBehavior,
				PrimaryProfileID:  voiceSelection.PrimaryProfileID,
				FallbackProfileID: voiceSelection.FallbackProfileID,
				ModeSource:        voiceSelection.ResolvedModeSource(),
			},
			SessionSummary:   cfg.VoiceAgent.EnableSessionSummary,
			PipelineFallback: cfg.VoiceAgent.PipelineFallback,
			CloseBehavior:    cfg.VoiceAgent.CloseBehavior,
			AgentProfileID:   voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID),
			AgentSequenceID:  strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID),
		},
		ServerConnection: serverConnectionSettingFromConfig(cfg.ServerConnection),
	}
}

// normaliseModeSourcePatch is the trust boundary for ModeSource patches:
// only the two known values are accepted; anything else collapses to
// "local" so a rogue or buggy frontend can't accidentally route traffic
// to a server that the user didn't approve.
func normaliseModeSourcePatch(in string) string {
	switch strings.TrimSpace(strings.ToLower(in)) {
	case config.ModeSourceServer:
		return config.ModeSourceServer
	default:
		return config.ModeSourceLocal
	}
}

func apiV1SingleModeSetting(cfg *config.Config, mode string) any {
	settings := apiV1ModeSettingsFromConfig(cfg)
	switch mode {
	case modeDictate:
		return settings.Dictation
	case modeAssist:
		return settings.Assist
	case modeVoiceAgent:
		return settings.VoiceAgent
	default:
		return map[string]string{}
	}
}
