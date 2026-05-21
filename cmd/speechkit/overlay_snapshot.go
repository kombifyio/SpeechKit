package main

import (
	"fmt"
	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/profiles"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/secrets"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
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
	dictateHotkeyBehavior := config.NormalizeHotkeyBehavior(s.dictateHotkeyBehavior, config.HotkeyBehaviorHoldToTalk)
	assistHotkeyBehavior := config.NormalizeHotkeyBehavior(s.assistHotkeyBehavior, config.HotkeyBehaviorHoldToTalk)
	voiceAgentHotkeyBehavior := config.NormalizeHotkeyBehavior(s.voiceAgentHotkeyBehavior, config.HotkeyBehaviorHoldToTalk)
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
		AssistOverlayMode:        config.NormalizeOverlayFeedbackMode(s.assistOverlayMode, config.OverlayFeedbackModeSmallFeedback),
		VoiceAgentOverlayMode:    config.NormalizeOverlayFeedbackMode(s.voiceAgentOverlayMode, config.OverlayFeedbackModeSmallFeedback),
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
		Position:                 normalizeOverlayPosition(s.overlayPosition),
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
		config.NormalizeHotkeyBehavior(cfg.General.DictateHotkeyBehavior, config.HotkeyBehaviorHoldToTalk),
	)
	assistHotkeyBehavior := config.NormalizeHotkeyBehavior(
		s.assistHotkeyBehavior,
		config.NormalizeHotkeyBehavior(cfg.General.AssistHotkeyBehavior, config.HotkeyBehaviorHoldToTalk),
	)
	voiceAgentHotkeyBehavior := config.NormalizeHotkeyBehavior(
		s.voiceAgentHotkeyBehavior,
		config.NormalizeHotkeyBehavior(cfg.General.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorHoldToTalk),
	)
	voiceAgentCloseBehavior := config.NormalizeVoiceAgentCloseBehavior(
		cfg.VoiceAgent.CloseBehavior,
		config.VoiceAgentCloseBehaviorContinue,
	)
	voiceAgentRefinementPrompt := strings.TrimSpace(cfg.VoiceAgent.RefinementPrompt)
	voiceAgentProfileID := voiceagentprofile.NormalizeID(cfg.VoiceAgent.AgentProfileID)
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
	wakewordSnap := buildWakewordSettingsSnapshot(cfg, s.wakewordRuntime != nil, s.wakewordStatus)
	return settingsSnapshot{
		OverlayEnabled:             s.overlayEnabled,
		OverlayPosition:            normalizeOverlayPosition(s.overlayPosition),
		OverlayMovable:             s.overlayMovable,
		OverlayFreeX:               s.overlayFreeX,
		OverlayFreeY:               s.overlayFreeY,
		StoreBackend:               storeBackend,
		SQLitePath:                 cfg.Store.SQLitePath,
		PostgresConfigured:         strings.TrimSpace(cfg.Store.PostgresDSN) != "",
		PostgresDSN:                cfg.Store.PostgresDSN,
		MaxAudioStorageMB:          cfg.Store.MaxAudioStorageMB,
		ModelDownloadDir:           strings.TrimSpace(cfg.General.ModelDownloadDir),
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
		VoiceAgentProfileID:        voiceAgentProfileID,
		VoiceAgentSequenceID:       strings.TrimSpace(cfg.VoiceAgent.AgentSequenceID),
		VoiceAgentProfiles:         voiceAgentProfileSnapshots(),
		VoiceAgentRefinementPrompt: voiceAgentRefinementPrompt,
		VoiceAgentSessionSummary:   cfg.VoiceAgent.EnableSessionSummary,
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
		AssistOverlayMode:          config.NormalizeOverlayFeedbackMode(s.assistOverlayMode, config.NormalizeOverlayFeedbackMode(cfg.UI.AssistOverlayMode, config.OverlayFeedbackModeSmallFeedback)),
		VoiceAgentOverlayMode:      config.NormalizeOverlayFeedbackMode(s.voiceAgentOverlayMode, config.NormalizeOverlayFeedbackMode(cfg.UI.VoiceAgentOverlayMode, config.OverlayFeedbackModeSmallFeedback)),
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
		ModelSelections:            profiles.ConfiguredModeModelSelections(cfg, catalog),
		ProviderCredentials:        providerCredentialStates(cfg),
		ProviderIntegrations:       providerIntegrationStates(cfg),
		Wakeword:                   wakewordSnap,
	}
}

// buildWakewordSettingsSnapshot assembles the wake-word block surfaced in
// /settings/state. The phrase catalog is always included so the dropdown
// in the React panel can render even when wake-word is disabled.
func buildWakewordSettingsSnapshot(cfg *config.Config, active bool, status string) wakewordSettingsSnapshot {
	entries := wakeword.DefaultCatalog()
	catalog := make([]wakewordCatalogEntry, 0, len(entries))
	for _, e := range entries {
		catalog = append(catalog, wakewordCatalogEntry{
			ID:           e.ID,
			DisplayName:  e.DisplayName,
			KeywordLabel: e.KeywordLabel,
			Notes:        e.Notes,
		})
	}
	defaultMode := config.NormalizeWakewordDefaultMode(cfg.Wakeword.DefaultMode)
	backend := config.NormalizeWakewordBackend(cfg.Wakeword.Backend)
	return wakewordSettingsSnapshot{
		Enabled:              cfg.Wakeword.Enabled,
		Backend:              backend,
		PhraseID:             strings.TrimSpace(cfg.Wakeword.PhraseID),
		DefaultMode:          defaultMode,
		Threshold:            cfg.Wakeword.Threshold,
		MinConsecutiveFrames: cfg.Wakeword.MinConsecutiveFrames,
		CooldownMs:           cfg.Wakeword.CooldownMs,
		DebugMode:            cfg.Wakeword.DebugMode,
		Active:               active,
		StatusMessage:        status,
		PhraseCatalog:        catalog,
		BackendOptions:       buildWakewordBackendOptions(cfg),
	}
}

func buildWakewordBackendOptions(cfg *config.Config) []wakewordBackendOption {
	sherpaAvailable, _ := sherpaWakewordBackendStatus()
	liveKitAvailable := len(liveKitOpenWakeWordMissingAssets(cfg)) == 0
	return []wakewordBackendOption{
		{
			ID:          config.WakewordBackendSherpaKWS,
			Label:       "Sherpa KWS sidecar",
			Available:   sherpaAvailable,
			Implemented: true,
			Recommended: true,
		},
		{
			ID:          config.WakewordBackendLiveKitOpenWakeWord,
			Label:       "LiveKit/openWakeWord",
			Available:   liveKitAvailable,
			Implemented: true,
		},
		{
			ID:          config.WakewordBackendSTTPhrase,
			Label:       "STT phrase match",
			Available:   true,
			Implemented: true,
		},
	}
}

func sherpaWakewordBackendStatus() (bool, string) {
	exe, err := os.Executable()
	if err != nil {
		return false, "Cannot resolve the running executable path."
	}
	exeDir := filepath.Dir(exe)
	for label, path := range map[string]string{
		"sidecar":  filepath.Join(exeDir, defaultWakewordSidecarBinary),
		"modelDir": filepath.Join(exeDir, defaultWakewordModelDirName),
		"keywords": filepath.Join(exeDir, defaultWakewordModelDirName, "keywords.txt"),
	} {
		if !pathIsRegularFileOrDir(path) {
			return false, fmt.Sprintf("Missing bundled %s at %s.", label, path)
		}
	}
	return true, "Bundled sidecar and KWS model are present."
}

func liveKitOpenWakeWordMissingAssets(cfg *config.Config) []string {
	paths := liveKitOpenWakeWordAssetPaths(cfg)
	missing := make([]string, 0, len(paths))
	for _, label := range []string{"melspec", "embedding", "phrase"} {
		path := paths[label]
		if !downloads.FileIsPresent(path) {
			missing = append(missing, fmt.Sprintf("%s (%s)", label, path))
		}
	}
	return missing
}

func liveKitOpenWakeWordAssetPaths(cfg *config.Config) map[string]string {
	phraseID := "hey_quby"
	if cfg != nil {
		if id := strings.TrimSpace(cfg.Wakeword.PhraseID); id != "" {
			phraseID = strings.ToLower(id)
		}
	}
	modelPath := resolveOpenWakeWordArtifactPath(cfg, phraseID+".onnx")
	melspecPath := resolveOpenWakeWordArtifactPath(cfg, "melspectrogram.onnx")
	embeddingPath := resolveOpenWakeWordArtifactPath(cfg, "embedding_model.onnx")
	if cfg != nil {
		if v := strings.TrimSpace(cfg.Wakeword.ModelPath); v != "" && !strings.HasSuffix(strings.ToLower(v), ".txt") {
			modelPath = v
		}
		if v := strings.TrimSpace(cfg.Wakeword.MelspecModelPath); v != "" {
			melspecPath = v
		}
		if v := strings.TrimSpace(cfg.Wakeword.EmbeddingModelPath); v != "" {
			embeddingPath = v
		}
	}
	return map[string]string{
		"phrase":    modelPath,
		"melspec":   melspecPath,
		"embedding": embeddingPath,
	}
}

func resolveOpenWakeWordArtifactPath(cfg *config.Config, filename string) string {
	userPath := filepath.Join(downloads.ResolveWakewordModelsDir(cfg), filename)
	if downloads.FileIsPresent(userPath) {
		return userPath
	}
	exe, err := os.Executable()
	if err == nil {
		bundledPath := filepath.Join(filepath.Dir(exe), "models", "wakeword", filename)
		if downloads.FileIsPresent(bundledPath) {
			return bundledPath
		}
	}
	return userPath
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
	if s == nil {
		return false
	}
	s.mu.Lock()
	runtime := s.runtimeStateLocked()
	s.mu.Unlock()
	metrics := pillPanelMetricsForRuntime(runtime)
	return s.updateOverlayFreeCenter(x+metrics.Width/2, y+metrics.Height/2)
}

func (s *appState) moveOverlayFreeCenter(centerX, centerY int) bool {
	if !s.updateOverlayFreeCenter(centerX, centerY) {
		return false
	}
	s.positionOverlay()
	return true
}
