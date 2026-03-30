package main

import "github.com/kombifyio/SpeechKit/pkg/speechkit"

type quickNoteContext struct {
	enabled     bool
	captureMode bool
	noteID      int64
}

func (s *appState) armQuickNoteRecording(noteID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.quickNoteMode = true
	s.quickCaptureMode = false
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
	return s.quickCaptureMode
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
