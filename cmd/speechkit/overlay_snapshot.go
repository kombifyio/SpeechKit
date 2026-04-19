package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/secrets"
)

// This file groups appState state-observation methods: audio-level updates,
// overlay/settings snapshots for the JS bridge, and persistence of the
// overlay's free-movement center per monitor.

func (s *appState) doneResetDelayValue() time.Duration {
	if s.doneResetDelay > 0 {
		return s.doneResetDelay
	}
	return defaultDoneResetDelay
}

func (s *appState) setLevel(level float64) {
	var logMessage string

	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}

	s.mu.Lock()
	if s.currentState != "recording" {
		level = 0
	}
	if level < s.overlayLevel {
		level = s.overlayLevel * 0.82
	}
	s.overlayLevel = level
	visualLevel := normalizeOverlayLevel(level)
	phase := overlayPhase(s.currentState, visualLevel)
	if phase != s.overlayPhase {
		logMessage = fmt.Sprintf(
			"Overlay audio: phase=%s raw=%.3f visual=%.3f",
			phase, level, visualLevel,
		)
	}
	s.overlayPhase = phase
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	if logMessage != "" {
		s.addLog(logMessage, "info")
	}
}

func (s *appState) overlaySnapshot() overlaySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	dictateHotkey := s.dictateHotkey
	if dictateHotkey == "" {
		dictateHotkey = s.hotkey
	}
	assistHotkey := s.assistHotkey
	voiceAgentHotkey := s.voiceAgentHotkey
	dictateHotkeyBehavior := config.NormalizeHotkeyBehavior(s.dictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	assistHotkeyBehavior := config.NormalizeHotkeyBehavior(s.assistHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	voiceAgentHotkeyBehavior := config.NormalizeHotkeyBehavior(s.voiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	agentHotkey := legacyAgentHotkeyFromModeBindings(assistHotkey, voiceAgentHotkey, modeAssist)
	modeEnabled, availableModes := modeAvailabilityFromState(
		s.dictateEnabled,
		s.assistEnabled,
		s.voiceAgentEnabled,
		dictateHotkey,
		assistHotkey,
		voiceAgentHotkey,
	)
	activeMode := sanitizeActiveModeForBindings(
		s.activeMode,
		"",
		s.dictateEnabled,
		s.assistEnabled,
		s.voiceAgentEnabled,
		dictateHotkey,
		assistHotkey,
		voiceAgentHotkey,
	)
	audioDeviceID := s.audioDeviceID
	audioOutputDeviceID := s.audioOutputDeviceID
	activeProfiles := cloneStringMap(s.activeProfiles)
	level := s.overlayLevel
	if s.currentState != "recording" {
		level = 0
	}
	level = normalizeOverlayLevel(level)
	phase := s.overlayPhase
	if phase == "" {
		phase = overlayPhase(s.currentState, level)
	}

	return overlaySnapshot{
		State:                    s.currentState,
		Phase:                    phase,
		Text:                     s.overlayText,
		Level:                    level,
		Visible:                  s.overlayEnabled,
		Visualizer:               s.overlayVisualizer,
		Design:                   s.overlayDesign,
		Hotkey:                   s.hotkey,
		DictateHotkey:            dictateHotkey,
		AssistHotkey:             assistHotkey,
		VoiceAgentHotkey:         voiceAgentHotkey,
		DictateHotkeyBehavior:    dictateHotkeyBehavior,
		AssistHotkeyBehavior:     assistHotkeyBehavior,
		VoiceAgentHotkeyBehavior: voiceAgentHotkeyBehavior,
		ModeEnabled:              modeEnabled,
		AvailableModes:           availableModes,
		AgentHotkey:              agentHotkey,
		ActiveMode:               activeMode,
		Position:                 s.overlayPosition,
		Movable:                  s.overlayMovable,
		PositionFreeX:            s.overlayFreeX,
		PositionFreeY:            s.overlayFreeY,
		LastTranscription:        s.lastTranscriptionText,
		QuickNoteMode:            s.quickNoteMode,
		AudioDeviceID:            audioDeviceID,
		SelectedAudioDeviceID:    audioDeviceID,
		AudioOutputDeviceID:      audioOutputDeviceID,
		SelectedOutputDeviceID:   audioOutputDeviceID,
		ActiveProfiles:           activeProfiles,
	}
}

func (s *appState) settingsSnapshot(cfg *config.Config) settingsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	dictateHotkey := s.dictateHotkey
	if dictateHotkey == "" {
		if cfg.General.DictateHotkey != "" {
			dictateHotkey = cfg.General.DictateHotkey
		} else {
			dictateHotkey = s.hotkey
		}
	}
	assistHotkey := strings.TrimSpace(s.assistHotkey)
	if assistHotkey == "" {
		assistHotkey = strings.TrimSpace(cfg.General.AssistHotkey)
	}
	voiceAgentHotkey := strings.TrimSpace(s.voiceAgentHotkey)
	if voiceAgentHotkey == "" {
		voiceAgentHotkey = strings.TrimSpace(cfg.General.VoiceAgentHotkey)
	}
	dictateHotkeyBehavior := config.NormalizeHotkeyBehavior(
		s.dictateHotkeyBehavior,
		config.NormalizeHotkeyBehavior(cfg.General.DictateHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	assistHotkeyBehavior := config.NormalizeHotkeyBehavior(
		s.assistHotkeyBehavior,
		config.NormalizeHotkeyBehavior(cfg.General.AssistHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	voiceAgentHotkeyBehavior := config.NormalizeHotkeyBehavior(
		s.voiceAgentHotkeyBehavior,
		config.NormalizeHotkeyBehavior(cfg.General.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk),
	)
	voiceAgentCloseBehavior := config.NormalizeVoiceAgentCloseBehavior(
		cfg.VoiceAgent.CloseBehavior,
		config.VoiceAgentCloseBehaviorContinue,
	)
	voiceAgentRefinementPrompt := strings.TrimSpace(cfg.VoiceAgent.RefinementPrompt)
	agentMode := normalizeAgentMode(cfg.General.AgentMode)
	agentHotkey := legacyAgentHotkeyFromModeBindings(assistHotkey, voiceAgentHotkey, agentMode)
	dictateEnabled := s.dictateEnabled
	assistEnabled := s.assistEnabled
	voiceAgentEnabled := s.voiceAgentEnabled
	modeEnabled, availableModes := modeAvailabilityFromState(
		dictateEnabled,
		assistEnabled,
		voiceAgentEnabled,
		dictateHotkey,
		assistHotkey,
		voiceAgentHotkey,
	)
	activeMode := sanitizeActiveModeForBindings(
		s.activeMode,
		agentMode,
		dictateEnabled,
		assistEnabled,
		voiceAgentEnabled,
		dictateHotkey,
		assistHotkey,
		voiceAgentHotkey,
	)
	if activeMode == modeNone {
		activeMode = sanitizeActiveModeForBindings(
			cfg.General.ActiveMode,
			agentMode,
			cfg.General.DictateEnabled,
			cfg.General.AssistEnabled,
			cfg.General.VoiceAgentEnabled,
			cfg.General.DictateHotkey,
			cfg.General.AssistHotkey,
			cfg.General.VoiceAgentHotkey,
		)
	}
	audioDeviceID := s.audioDeviceID
	if audioDeviceID == "" {
		audioDeviceID = cfg.Audio.DeviceID
	}
	audioOutputDeviceID := s.audioOutputDeviceID
	if audioOutputDeviceID == "" {
		audioOutputDeviceID = cfg.Audio.OutputDeviceID
	}
	storeBackend := cfg.Store.Backend
	if storeBackend == "" {
		storeBackend = "sqlite"
	}
	hfAvailable := config.ManagedHuggingFaceAvailableInBuild()
	tokenStatus := secrets.TokenStatus{ActiveSource: secrets.TokenSourceNone}
	if hfAvailable {
		var err error
		tokenStatus, err = config.HuggingFaceTokenStatus(cfg)
		if err != nil {
			tokenStatus.ActiveSource = "none"
		}
	}
	catalog := filteredModelCatalog()
	return settingsSnapshot{
		OverlayEnabled:             s.overlayEnabled,
		OverlayPosition:            s.overlayPosition,
		OverlayMovable:             s.overlayMovable,
		OverlayFreeX:               s.overlayFreeX,
		OverlayFreeY:               s.overlayFreeY,
		StoreBackend:               storeBackend,
		SQLitePath:                 cfg.Store.SQLitePath,
		PostgresConfigured:         strings.TrimSpace(cfg.Store.PostgresDSN) != "",
		PostgresDSN:                cfg.Store.PostgresDSN,
		MaxAudioStorageMB:          cfg.Store.MaxAudioStorageMB,
		HFAvailable:                hfAvailable,
		HFEnabled:                  hfAvailable && cfg.HuggingFace.Enabled,
		HFHasUserToken:             tokenStatus.HasUserToken,
		HFHasInstallToken:          tokenStatus.HasInstallToken,
		HFTokenSource:              string(tokenStatus.ActiveSource),
		Hotkey:                     dictateHotkey,
		DictateHotkey:              dictateHotkey,
		AssistHotkey:               assistHotkey,
		VoiceAgentHotkey:           voiceAgentHotkey,
		DictateHotkeyBehavior:      dictateHotkeyBehavior,
		AssistHotkeyBehavior:       assistHotkeyBehavior,
		VoiceAgentHotkeyBehavior:   voiceAgentHotkeyBehavior,
		VoiceAgentCloseBehavior:    voiceAgentCloseBehavior,
		VoiceAgentRefinementPrompt: voiceAgentRefinementPrompt,
		AutoStartOnLaunch:          cfg.General.AutoStartOnLaunch,
		VoiceAgentAutoStart:        cfg.General.AutoStartOnLaunch,
		ModeEnabled:                modeEnabled,
		AvailableModes:             availableModes,
		AgentHotkey:                agentHotkey,
		AgentMode:                  agentMode,
		ActiveMode:                 activeMode,
		HFModel:                    cfg.HuggingFace.Model,
		Visualizer:                 s.overlayVisualizer,
		Design:                     cfg.UI.Design,
		Language:                   cfg.General.Language,
		VocabularyDictionary:       cfg.Vocabulary.Dictionary,
		SaveAudio:                  cfg.Store.SaveAudio,
		AudioRetentionDays:         cfg.Store.AudioRetentionDays,
		AudioDeviceID:              audioDeviceID,
		SelectedAudioDeviceID:      audioDeviceID,
		AudioOutputDeviceID:        audioOutputDeviceID,
		SelectedOutputDeviceID:     audioOutputDeviceID,
		Profiles:                   catalog.Profiles,
		ActiveProfiles:             cloneStringMap(s.activeProfiles),
		ModelSelections:            configuredModeModelSelections(cfg, catalog),
		ProviderCredentials:        providerCredentialStates(cfg),
	}
}

func (s *appState) overlayFreeCenterState() (int, int, map[string]config.OverlayFreePosition) {
	if s == nil {
		return 0, 0, map[string]config.OverlayFreePosition{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.overlayFreeX, s.overlayFreeY, cloneOverlayMonitorPositions(s.overlayMonitorCenters)
}

func (s *appState) syncResolvedOverlayFreeCenter(key string, centerX, centerY int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	if s.overlayMonitorKey != key {
		s.overlayMonitorKey = key
		changed = true
	}
	if !s.overlayMovable {
		if changed {
			s.syncSpeechKitSnapshotLocked()
		}
		return
	}
	if s.overlayFreeX != centerX {
		s.overlayFreeX = centerX
		changed = true
	}
	if s.overlayFreeY != centerY {
		s.overlayFreeY = centerY
		changed = true
	}
	if s.overlayMonitorCenters == nil {
		s.overlayMonitorCenters = make(map[string]config.OverlayFreePosition)
	}
	if trimmedKey := strings.TrimSpace(key); trimmedKey != "" {
		saved := config.OverlayFreePosition{X: centerX, Y: centerY}
		if current, ok := s.overlayMonitorCenters[trimmedKey]; !ok || current != saved {
			s.overlayMonitorCenters[trimmedKey] = saved
			changed = true
		}
	}
	if changed {
		s.syncSpeechKitSnapshotLocked()
	}
}

func (s *appState) updateOverlayFreeCenter(centerX, centerY int) bool {
	if s == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.overlayMovable {
		return false
	}

	s.overlayFreeX = centerX
	s.overlayFreeY = centerY
	if s.overlayMonitorCenters == nil {
		s.overlayMonitorCenters = make(map[string]config.OverlayFreePosition)
	}
	if key := strings.TrimSpace(s.overlayMonitorKey); key != "" {
		s.overlayMonitorCenters[key] = config.OverlayFreePosition{X: centerX, Y: centerY}
	}
	s.syncSpeechKitSnapshotLocked()
	return true
}

func (s *appState) updateOverlayFreeCenterFromPanel(x, y int) bool {
	return s.updateOverlayFreeCenter(x+pillPanelWidth/2, y+pillPanelHeight/2)
}

func (s *appState) moveOverlayFreeCenter(centerX, centerY int) bool {
	if !s.updateOverlayFreeCenter(centerX, centerY) {
		return false
	}
	s.positionOverlay()
	return true
}
