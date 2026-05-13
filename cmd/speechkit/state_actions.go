package main

import (
	"log/slog"
	"time"

	"github.com/kombifyio/SpeechKit/internal/tray"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type quickNoteContext struct {
	enabled     bool
	captureMode bool
	noteID      int64
}

func (s *appState) setState(state, text string) {
	s.mu.Lock()
	s.currentState = state
	s.overlayText = text
	if state == "recording" || state == "idle" {
		s.overlayFeedbackRole = ""
		s.overlayFeedbackText = ""
		s.overlayFeedbackDone = true
	}
	if state != "recording" {
		s.overlayLevel = 0
	}
	s.overlayPhase = overlayPhase(state, normalizeOverlayLevel(s.overlayLevel))
	appTray := s.appTray
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	s.publishSpeechKitEvent(speechkitStateEvent(state, text))

	s.showActiveOverlayWindow()
	if appTray != nil {
		appTray.SetState(tray.State(state))
	}

	if state == "done" {
		go s.resetIdleAfter("done", s.doneResetDelayValue())
	}
}

func (s *appState) resetIdleAfter(expected string, delay time.Duration) {
	time.Sleep(delay)

	s.mu.Lock()
	current := s.currentState
	s.mu.Unlock()

	if current == expected {
		s.setState("idle", "")
	}
}

func (s *appState) addLog(msg, logType string) {
	entry := logEntry{
		Message:   msg,
		Type:      logType,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	s.mu.Lock()
	s.logEntries = append(s.logEntries, entry)
	if len(s.logEntries) > 200 {
		s.logEntries = s.logEntries[len(s.logEntries)-200:]
	}
	s.mu.Unlock()

	if event, ok := speechkitLogEvent(msg, logType); ok {
		s.publishSpeechKitEvent(event)
	}

	slog.Info(msg)
}

func (s *appState) armQuickNoteRecording(noteID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.quickNoteMode = true
	s.quickCaptureMode = false
	s.quickCaptureAutoStart = true
	s.quickNoteAutoStop = true
	s.quickCaptureNoteID = noteID
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
	s.publishSpeechKitEvent(speechkit.Event{
		Type:      speechkit.EventQuickNoteModeArmed,
		Message:   "quick note recording armed",
		QuickNote: true,
	})
}

func (s *appState) armQuickCapture(noteID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.quickNoteMode = true
	s.quickCaptureMode = true
	s.quickCaptureAutoStart = true
	s.quickNoteAutoStop = true
	s.quickCaptureNoteID = noteID
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
	s.publishSpeechKitEvent(speechkit.Event{
		Type:      speechkit.EventQuickNoteModeArmed,
		Message:   "quick capture armed",
		QuickNote: true,
	})
}

func (s *appState) clearQuickNoteRecording() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.quickNoteMode = false
	s.quickCaptureMode = false
	s.quickCaptureAutoStart = false
	s.quickNoteAutoStop = false
	s.quickCaptureNoteID = 0
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
}

func (s *appState) consumeQuickCaptureAutoStart() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.quickCaptureAutoStart {
		return false
	}
	s.quickCaptureAutoStart = false
	s.syncSpeechKitSnapshotLocked()
	return true
}

func (s *appState) quickCaptureModeActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quickCaptureMode || s.quickNoteAutoStop
}

func (s *appState) currentQuickNoteContext() quickNoteContext {
	if s == nil {
		return quickNoteContext{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return quickNoteContext{
		enabled:     s.quickNoteMode,
		captureMode: s.quickCaptureMode,
		noteID:      s.quickCaptureNoteID,
	}
}

func (s *appState) setQuickCaptureNoteID(noteID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.quickCaptureNoteID = noteID
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()
}

func (s *appState) applyTranscriptionCompletion(completion speechkit.Completion) {
	if s == nil {
		return
	}
	if completion.TranscriptionPersisted {
		s.incrementTranscriptions()
	}
	if completion.QuickNoteCommitted {
		s.publishSpeechKitEvent(speechkit.Event{
			Type:      speechkit.EventQuickNoteUpdated,
			Message:   "quick note updated",
			Text:      completion.Transcript.Text,
			Provider:  completion.Transcript.Provider,
			QuickNote: true,
		})
	}
}
