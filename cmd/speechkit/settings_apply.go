package main

import (
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

func (s *appState) applyRuntimeSettings(dictateHotkey, agentHotkey, activeMode, audioDeviceID string, providers []string, visualizerValue, designValue, overlayPosition string) string {
	if s == nil {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	oldHotkey := s.hotkey
	s.dictateHotkey = dictateHotkey
	s.agentHotkey = agentHotkey
	if activeMode == "agent" || activeMode == "dictate" {
		s.activeMode = activeMode
	}
	s.audioDeviceID = audioDeviceID
	s.providers = append([]string(nil), providers...)
	s.overlayVisualizer = visualizerValue
	s.overlayDesign = designValue
	s.overlayPosition = overlayPosition
	s.hotkey = s.activeHotkeyLocked()
	s.syncSpeechKitSnapshotLocked()
	return oldHotkey
}

func (s *appState) applyDesktopSettings(oldDictateHotkey, oldAgentHotkey, dictateHotkey, agentHotkey, oldAudioDeviceID, audioDeviceID string, overlayEnabled bool) {
	if s == nil {
		return
	}

	s.mu.Lock()
	hkMgr := s.hkManager
	audioSession := s.audioSession
	s.mu.Unlock()

	if hkMgr != nil && (dictateHotkey != oldDictateHotkey || agentHotkey != oldAgentHotkey) {
		if modeManager, ok := hkMgr.(modeHotkeyReconfigurer); ok {
			modeManager.ReconfigureModes(hotkey.ParseCombo(dictateHotkey), hotkey.ParseCombo(agentHotkey))
		} else if dictateHotkey != oldDictateHotkey {
			hkMgr.Reconfigure(hotkey.ParseCombo(dictateHotkey))
		}
		s.addLog(fmt.Sprintf("Hotkeys updated: dictate=%s agent=%s", dictateHotkey, agentHotkey), "info")
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
