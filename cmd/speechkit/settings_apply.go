package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

// sha256First16 returns the first 16 hex characters of the SHA-256 hash of s.
// Used to record that a setting value changed without persisting the raw value.
func sha256First16(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

// emitSettingsChanged records a settings.changed audit event. Errors from
// AppendEvent are intentionally discarded — audit failures must never block
// settings updates.
func emitSettingsChanged(ctx context.Context, keyPath, oldVal, newVal string) {
	_ = auditlog.AppendEvent(ctx, auditlog.Record{
		Event: auditlog.EventSettingsChanged,
		Resource: map[string]any{
			"key_path": keyPath,
			"old_hash": sha256First16(oldVal),
			"new_hash": sha256First16(newVal),
		},
	})
}

// marshalSettingsJSON serialises v to a JSON string for hashing.
// Returns an empty string on marshal error (should never happen for config structs).
func marshalSettingsJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// emitSettingsDiff emits one settings.changed audit event per top-level
// config section whose JSON-marshalled value differs between old and new.
// Sub-field changes within a section roll up to one event per section.
// Values themselves are never logged — only SHA-256 hashes are recorded.
func emitSettingsDiff(ctx context.Context, old, newCfg config.Config) {
	sections := []struct {
		keyPath string
		oldVal  any
		newVal  any
	}{
		{"update", old.Update, newCfg.Update},
		{"logging", old.Logging, newCfg.Logging},
		{"audit", old.Audit, newCfg.Audit},
		{"telemetry", old.Telemetry, newCfg.Telemetry},
		{"routing", old.Routing, newCfg.Routing},
		{"voice_agent", old.VoiceAgent, newCfg.VoiceAgent},
		{"providers", old.Providers, newCfg.Providers},
		{"vps", old.VPS, newCfg.VPS},
		{"huggingface", old.HuggingFace, newCfg.HuggingFace},
		{"local", old.Local, newCfg.Local},
		{"local_llm", old.LocalLLM, newCfg.LocalLLM},
		{"tts", old.TTS, newCfg.TTS},
		{"model_selection", old.ModelSelection, newCfg.ModelSelection},
		{"server_connection", old.ServerConnection, newCfg.ServerConnection},
		{"general", old.General, newCfg.General},
		{"audio", old.Audio, newCfg.Audio},
	}
	for _, s := range sections {
		oldJSON := marshalSettingsJSON(s.oldVal)
		newJSON := marshalSettingsJSON(s.newVal)
		if sha256First16(oldJSON) == sha256First16(newJSON) {
			continue
		}
		emitSettingsChanged(ctx, s.keyPath, oldJSON, newJSON)
	}
}

func (s *appState) applyRuntimeSettings(old, newCfg config.Config, dictateEnabled, assistEnabled, voiceAgentEnabled bool, dictateHotkey, assistHotkey, voiceAgentHotkey, dictateHotkeyBehavior, assistHotkeyBehavior, voiceAgentHotkeyBehavior, activeMode, audioDeviceID string, providers []string, visualizerValue, designValue, assistOverlayMode, voiceAgentOverlayMode, overlayPosition, vocabularyDictionary string, overlayMovable bool, overlayFreeX, overlayFreeY int, overlayMonitorPositions map[string]config.OverlayFreePosition) string {
	if s == nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldHotkey := s.hotkey
	s.dictateEnabled = dictateEnabled
	s.assistEnabled = assistEnabled
	s.voiceAgentEnabled = voiceAgentEnabled
	s.dictateHotkey = dictateHotkey
	s.assistHotkey = assistHotkey
	s.voiceAgentHotkey = voiceAgentHotkey
	s.dictateHotkeyBehavior = config.NormalizeHotkeyBehavior(dictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	s.assistHotkeyBehavior = config.NormalizeHotkeyBehavior(assistHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	s.voiceAgentHotkeyBehavior = config.NormalizeHotkeyBehavior(voiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	legacyAgentMode := deriveLegacyAgentModeFromBindings(assistHotkey, voiceAgentHotkey, activeMode, modeAssist)
	s.agentHotkey = legacyAgentHotkeyFromModeBindings(assistHotkey, voiceAgentHotkey, legacyAgentMode)
	s.activeMode = sanitizeActiveModeForBindings(activeMode, "", dictateEnabled, assistEnabled, voiceAgentEnabled, dictateHotkey, assistHotkey, voiceAgentHotkey)
	s.audioDeviceID = audioDeviceID
	s.providers = append([]string(nil), providers...)
	s.overlayVisualizer = visualizerValue
	s.overlayDesign = designValue
	s.assistOverlayMode = config.NormalizeOverlayFeedbackMode(assistOverlayMode, config.OverlayFeedbackModeSmallFeedback)
	s.voiceAgentOverlayMode = config.NormalizeOverlayFeedbackMode(voiceAgentOverlayMode, config.OverlayFeedbackModeSmallFeedback)
	s.overlayPosition = overlayPosition
	s.overlayMovable = overlayMovable
	s.overlayFreeX = overlayFreeX
	s.overlayFreeY = overlayFreeY
	s.overlayMonitorCenters = cloneOverlayMonitorPositions(overlayMonitorPositions)
	s.vocabularyDictionary = vocabularyDictionary
	s.hotkey = s.activeHotkeyLocked()
	s.syncSpeechKitSnapshotLocked()

	// Emit one settings.changed audit event per top-level config section whose
	// hashed JSON differs between old and newCfg. Unchanged sections are skipped.
	// Values themselves are never logged — only SHA-256 hashes are recorded.
	emitSettingsDiff(context.Background(), old, newCfg) //nolint:contextcheck // called under mutex, no request context available

	return oldHotkey
}

func (s *appState) applyDesktopSettings(oldDictateEnabled, oldAssistEnabled, oldVoiceAgentEnabled bool, oldDictateHotkey, oldAssistHotkey, oldVoiceAgentHotkey string, dictateEnabled, assistEnabled, voiceAgentEnabled bool, dictateHotkey, assistHotkey, voiceAgentHotkey, oldAudioDeviceID, audioDeviceID string, overlayEnabled bool) {
	if s == nil {
		return
	}

	s.mu.Lock()
	hkMgr := s.hkManager
	audioSession := s.audioSession
	s.mu.Unlock()

	if hkMgr != nil && (dictateEnabled != oldDictateEnabled || assistEnabled != oldAssistEnabled || voiceAgentEnabled != oldVoiceAgentEnabled || dictateHotkey != oldDictateHotkey || assistHotkey != oldAssistHotkey || voiceAgentHotkey != oldVoiceAgentHotkey) {
		if modeManager, ok := hkMgr.(modeHotkeyReconfigurer); ok {
			modeManager.ReconfigureModes(configuredModeCombos(dictateEnabled, assistEnabled, voiceAgentEnabled, dictateHotkey, assistHotkey, voiceAgentHotkey))
		} else if dictateHotkey != oldDictateHotkey {
			hkMgr.Reconfigure(hotkey.ParseCombo(dictateHotkey))
		}
		s.addLog(fmt.Sprintf("Hotkeys updated: dictate=%s assist=%s voice_agent=%s", dictateHotkey, assistHotkey, voiceAgentHotkey), "info")
	}
	if audioSession != nil && audioDeviceID != oldAudioDeviceID {
		if err := audioSession.ReconfigureDevice(audioDeviceID); err != nil {
			s.addLog(fmt.Sprintf("Audio device update failed: %v", err), "warn")
		} else {
			s.addLog("Audio device updated", "info")
		}
	}
	s.setOverlayEnabled(overlayEnabled)
}
