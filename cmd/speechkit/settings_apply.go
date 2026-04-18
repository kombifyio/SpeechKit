package main

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

func (s *appState) applyRuntimeSettings(dictateEnabled, assistEnabled, voiceAgentEnabled bool, dictateHotkey, assistHotkey, voiceAgentHotkey, dictateHotkeyBehavior, assistHotkeyBehavior, voiceAgentHotkeyBehavior, activeMode, audioDeviceID string, providers []string, visualizerValue, designValue, overlayPosition, vocabularyDictionary string, overlayMovable bool, overlayFreeX, overlayFreeY int, overlayMonitorPositions map[string]config.OverlayFreePosition) string {
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
	s.overlayPosition = overlayPosition
	s.overlayMovable = overlayMovable
	s.overlayFreeX = overlayFreeX
	s.overlayFreeY = overlayFreeY
	s.overlayMonitorCenters = cloneOverlayMonitorPositions(overlayMonitorPositions)
	s.vocabularyDictionary = vocabularyDictionary
	s.hotkey = s.activeHotkeyLocked()
	s.syncSpeechKitSnapshotLocked()
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
