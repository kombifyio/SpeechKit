package main

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

func (s *appState) applyRuntimeSettings(dictateHotkey, assistHotkey, voiceAgentHotkey, activeMode, audioDeviceID string, providers []string, visualizerValue, designValue, overlayPosition, vocabularyDictionary string, overlayMovable bool, overlayFreeX, overlayFreeY int, overlayMonitorPositions map[string]config.OverlayFreePosition) string {
	if s == nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldHotkey := s.hotkey
	s.dictateHotkey = dictateHotkey
	s.assistHotkey = assistHotkey
	s.voiceAgentHotkey = voiceAgentHotkey
	s.agentHotkey = legacyAgentHotkeyFromModeBindings(assistHotkey, voiceAgentHotkey, normalizeAgentMode(""))
	s.activeMode = normalizeRuntimeMode(activeMode, "")
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

func (s *appState) applyDesktopSettings(oldDictateHotkey, oldAssistHotkey, oldVoiceAgentHotkey, dictateHotkey, assistHotkey, voiceAgentHotkey, oldAudioDeviceID, audioDeviceID string, overlayEnabled bool) {
	if s == nil {
		return
	}

	s.mu.Lock()
	hkMgr := s.hkManager
	audioSession := s.audioSession
	s.mu.Unlock()

	if hkMgr != nil && (dictateHotkey != oldDictateHotkey || assistHotkey != oldAssistHotkey || voiceAgentHotkey != oldVoiceAgentHotkey) {
		if modeManager, ok := hkMgr.(modeHotkeyReconfigurer); ok {
			modeManager.ReconfigureModes(configuredModeCombos(dictateHotkey, assistHotkey, voiceAgentHotkey))
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
