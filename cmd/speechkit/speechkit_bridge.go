package main

import (
	"context"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func newSpeechKitRuntime(state *appState, hooks speechkit.Hooks) *speechkit.Runtime {
	if state == nil {
		return speechkit.NewRuntime(speechkit.Snapshot{}, hooks)
	}
	state.mu.Lock()
	snapshot := state.speechkitSnapshotLocked()
	state.mu.Unlock()
	return speechkit.NewRuntime(snapshot, hooks)
}

func (s *appState) speechkitSnapshotLocked() speechkit.Snapshot {
	state := s.runtimeStateLocked()
	level := state.overlayLevel
	if state.currentState != "recording" {
		level = 0
	}

	return speechkit.Snapshot{
		Status:                state.currentState,
		Text:                  state.overlayText,
		Level:                 normalizeOverlayLevel(level),
		Hotkey:                state.hotkey,
		ActiveMode:            state.activeMode,
		Providers:             append([]string(nil), state.providers...),
		ActiveProfiles:        cloneStringMap(state.activeProfiles),
		Transcriptions:        state.transcriptions,
		QuickNoteMode:         state.quickNoteMode,
		QuickCaptureMode:      state.quickCaptureMode,
		LastTranscriptionText: state.lastTranscriptionText,
	}
}

func (s *appState) syncSpeechKitSnapshotLocked() {
	if s.engine == nil {
		return
	}
	s.engine.SetState(s.speechkitSnapshotLocked())
}

func (s *appState) syncSpeechKitSnapshot() {
	s.mu.Lock()
	engine := s.engine
	snapshot := s.speechkitSnapshotLocked()
	s.mu.Unlock()
	if engine != nil {
		engine.SetState(snapshot)
	}
}

func (s *appState) setActiveMode(mode string) {
	if s == nil || mode == "" {
		return
	}
	s.mu.Lock()
	s.activeMode = sanitizeActiveModeForBindings(
		mode,
		"",
		s.dictateEnabled,
		s.assistEnabled,
		s.voiceAgentEnabled,
		s.dictateHotkey,
		s.assistHotkey,
		s.voiceAgentHotkey,
	)
	s.hotkey = s.activeHotkeyLocked()
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
}

func (s *appState) setModeEnabled(mode string, enabled bool) {
	if s == nil {
		return
	}

	s.mu.Lock()
	switch mode {
	case modeDictate:
		if strings.TrimSpace(s.dictateHotkey) == "" {
			enabled = false
		}
		s.dictateEnabled = enabled
	case modeAssist:
		if strings.TrimSpace(s.assistHotkey) == "" {
			enabled = false
		}
		s.assistEnabled = enabled
	case modeVoiceAgent:
		if strings.TrimSpace(s.voiceAgentHotkey) == "" {
			enabled = false
		}
		s.voiceAgentEnabled = enabled
	default:
		s.mu.Unlock()
		return
	}
	s.activeMode = sanitizeActiveModeForBindings(
		s.activeMode,
		"",
		s.dictateEnabled,
		s.assistEnabled,
		s.voiceAgentEnabled,
		s.dictateHotkey,
		s.assistHotkey,
		s.voiceAgentHotkey,
	)
	s.hotkey = s.activeHotkeyLocked()
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
}

func (s *appState) setAudioDevice(deviceID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.audioDeviceID = strings.TrimSpace(deviceID)
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
}

func (s *appState) setAudioOutputDevice(ctx context.Context, deviceID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.audioOutputDeviceID = strings.TrimSpace(deviceID)
	streamActive := s.streamPlayer != nil
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	if streamActive {
		s.startVoiceAgentStream(ctx)
	}
}

func (s *appState) activeHotkeyLocked() string {
	return activeModeHotkey(s.runtimeStateLocked())
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func (s *appState) publishSpeechKitEvent(event speechkit.Event) {
	if s == nil || s.engine == nil {
		return
	}
	s.engine.Publish(event)
}

func (s *appState) OnState(status, text string) {
	s.setState(status, text)
}

func (s *appState) OnLog(message, kind string) {
	s.addLog(message, kind)
}

func (s *appState) OnTranscriptCommitted(transcript speechkit.Transcript, quickNote bool) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.lastTranscriptionText = transcript.Text
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	s.publishSpeechKitEvent(speechkit.Event{
		Type:      speechkit.EventTranscriptCommitted,
		Message:   "transcript committed",
		Text:      transcript.Text,
		Provider:  transcript.Provider,
		QuickNote: quickNote,
	})
}

func (s *appState) incrementTranscriptions() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.transcriptions++
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
}

func speechkitStateEvent(state, text string) speechkit.Event {
	event := speechkit.Event{
		Type:    speechkit.EventStateChanged,
		Message: state,
		Text:    text,
	}

	switch state {
	case "recording":
		event.Type = speechkit.EventRecordingStarted
		event.Message = "recording"
	case "processing":
		event.Type = speechkit.EventProcessingStarted
		event.Message = "processing"
	case "done":
		event.Type = speechkit.EventTranscriptionReady
		event.Message = "done"
	}

	return event
}

func speechkitLogEvent(message, logType string) (speechkit.Event, bool) {
	switch logType {
	case "warn":
		return speechkit.Event{
			Type:    speechkit.EventWarningRaised,
			Message: message,
		}, true
	case "error":
		return speechkit.Event{
			Type:    speechkit.EventErrorRaised,
			Message: message,
		}, true
	default:
		return speechkit.Event{}, false
	}
}
