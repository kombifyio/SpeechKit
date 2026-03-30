package main

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type testCommandRecorder struct {
	startErr   error
	stopPCM    []byte
	started    bool
	pcmHandler func([]byte)
}

func (r *testCommandRecorder) Start() error {
	if r.startErr != nil {
		return r.startErr
	}
	r.started = true
	return nil
}

func (r *testCommandRecorder) Stop() ([]byte, error) {
	r.started = false
	return append([]byte(nil), r.stopPCM...), nil
}

func (r *testCommandRecorder) SetPCMHandler(handler func([]byte)) {
	r.pcmHandler = handler
}

type testCommandSubmitter struct {
	jobs []speechkit.TranscriptionJob
}

func (s *testCommandSubmitter) Submit(job speechkit.TranscriptionJob) error {
	s.jobs = append(s.jobs, job.Clone())
	return nil
}

type testCommandObserver struct{}

func (testCommandObserver) OnState(string, string) {}
func (testCommandObserver) OnLog(string, string)   {}

type testCommandStore struct {
	saveQuickNoteCalls int
}

type testCommandQuickNoteService struct {
	openedEditorNoteID int64
	openCaptureCalls   int
	closeCaptureCalls  int
	armedRecordingID   int64
}

func (s *testCommandQuickNoteService) ArmRecording(noteID int64) {
	s.armedRecordingID = noteID
}

func (s *testCommandQuickNoteService) OpenEditor(noteID int64) error {
	s.openedEditorNoteID = noteID
	return nil
}

func (s *testCommandQuickNoteService) OpenCapture(context.Context) (int64, error) {
	s.openCaptureCalls++
	return 321, nil
}

func (s *testCommandQuickNoteService) CloseCapture() error {
	s.closeCaptureCalls++
	return nil
}

func (s *testCommandStore) SaveTranscription(context.Context, string, string, string, string, int64, int64, []byte) error {
	return nil
}

func (s *testCommandStore) GetTranscription(context.Context, int64) (*store.Transcription, error) {
	return nil, nil
}

func (s *testCommandStore) ListTranscriptions(context.Context, store.ListOpts) ([]store.Transcription, error) {
	return nil, nil
}

func (s *testCommandStore) TranscriptionCount(context.Context) (int, error) { return 0, nil }

func (s *testCommandStore) SaveQuickNote(_ context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	s.saveQuickNoteCalls++
	return 99, nil
}

func (s *testCommandStore) GetQuickNote(context.Context, int64) (*store.QuickNote, error) {
	return nil, nil
}

func (s *testCommandStore) ListQuickNotes(context.Context, store.ListOpts) ([]store.QuickNote, error) {
	return nil, nil
}

func (s *testCommandStore) UpdateQuickNote(context.Context, int64, string) error { return nil }

func (s *testCommandStore) UpdateQuickNoteCapture(context.Context, int64, string, string, int64, int64, []byte) error {
	return nil
}

func (s *testCommandStore) PinQuickNote(context.Context, int64, bool) error { return nil }
func (s *testCommandStore) DeleteQuickNote(context.Context, int64) error    { return nil }
func (s *testCommandStore) QuickNoteCount(context.Context) (int, error)     { return 0, nil }
func (s *testCommandStore) Stats(context.Context) (store.Stats, error)      { return store.Stats{}, nil }
func (s *testCommandStore) Close() error                                    { return nil }

func TestDesktopCommandHandlerStartDictation(t *testing.T) {
	recorder := &testCommandRecorder{}
	submitter := &testCommandSubmitter{}
	controller := speechkit.NewRecordingController(recorder, submitter, testCommandObserver{}, nil)
	state := &appState{hotkey: "win+alt"}
	handler := desktopCommandHandler{
		cfg:                 &config.Config{General: config.GeneralConfig{Language: "en"}},
		state:               state,
		recordingController: controller,
	}

	err := handler.Handle(context.Background(), speechkit.Command{
		Type: speechkit.CommandStartDictation,
		Metadata: map[string]string{
			"label": "Recording started",
		},
	})
	if err != nil {
		t.Fatalf("Handle(start) error = %v", err)
	}
	if !controller.IsRecording() {
		t.Fatal("controller.IsRecording() = false, want true")
	}
}

func TestDesktopCommandHandlerStopDictationClearsQuickNoteState(t *testing.T) {
	recorder := &testCommandRecorder{stopPCM: make([]byte, 6400)}
	submitter := &testCommandSubmitter{}
	controller := speechkit.NewRecordingController(recorder, submitter, testCommandObserver{}, nil)
	state := &appState{
		hotkey:             "win+alt",
		quickNoteMode:      true,
		quickCaptureNoteID: 7,
	}
	handler := desktopCommandHandler{
		cfg:                 &config.Config{General: config.GeneralConfig{Language: "en"}},
		state:               state,
		recordingController: controller,
	}

	if err := controller.Start(speechkit.RecordingStartOptions{
		Label:       "Recording started",
		Language:    "en",
		QuickNote:   true,
		QuickNoteID: 7,
	}); err != nil {
		t.Fatalf("controller.Start() error = %v", err)
	}

	err := handler.Handle(context.Background(), speechkit.Command{
		Type: speechkit.CommandStopDictation,
		Metadata: map[string]string{
			"label": "Captured",
		},
	})
	if err != nil {
		t.Fatalf("Handle(stop) error = %v", err)
	}
	if state.quickNoteMode {
		t.Fatal("state.quickNoteMode = true, want false")
	}
	if state.quickCaptureNoteID != 0 {
		t.Fatalf("state.quickCaptureNoteID = %d, want 0", state.quickCaptureNoteID)
	}
	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(submitter.jobs))
	}
}

func TestDesktopCommandHandlerShowDashboard(t *testing.T) {
	called := ""
	handler := desktopCommandHandler{
		cfg:   &config.Config{},
		state: &appState{},
		showDashboard: func(source string) {
			called = source
		},
	}

	err := handler.Handle(context.Background(), speechkit.Command{
		Type: speechkit.CommandShowDashboard,
		Metadata: map[string]string{
			"source": "tray",
		},
	})
	if err != nil {
		t.Fatalf("Handle(show dashboard) error = %v", err)
	}
	if got, want := called, "tray"; got != want {
		t.Fatalf("showDashboard source = %q, want %q", got, want)
	}
}

func TestDesktopCommandHandlerStartDictationReusesExistingQuickNoteID(t *testing.T) {
	recorder := &testCommandRecorder{stopPCM: make([]byte, 6400)}
	submitter := &testCommandSubmitter{}
	controller := speechkit.NewRecordingController(recorder, submitter, testCommandObserver{}, nil)
	feedbackStore := &testCommandStore{}
	state := &appState{
		hotkey:             "win+alt",
		quickNoteMode:      true,
		quickCaptureNoteID: 123,
	}
	handler := desktopCommandHandler{
		cfg:                 &config.Config{General: config.GeneralConfig{Language: "en"}},
		state:               state,
		recordingController: controller,
		feedbackStore:       feedbackStore,
	}

	if err := handler.Handle(context.Background(), speechkit.Command{Type: speechkit.CommandStartDictation}); err != nil {
		t.Fatalf("Handle(start) error = %v", err)
	}
	if err := handler.Handle(context.Background(), speechkit.Command{Type: speechkit.CommandStopDictation}); err != nil {
		t.Fatalf("Handle(stop) error = %v", err)
	}

	if got := feedbackStore.saveQuickNoteCalls; got != 0 {
		t.Fatalf("SaveQuickNote calls = %d, want 0", got)
	}
	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(submitter.jobs))
	}
	if got, want := submitter.jobs[0].Submission.QuickNoteID, int64(123); got != want {
		t.Fatalf("job quick note id = %d, want %d", got, want)
	}
}

func TestDesktopCommandHandlerQuickActionRequiresCoordinator(t *testing.T) {
	handler := desktopCommandHandler{
		cfg:   &config.Config{},
		state: &appState{},
	}

	err := handler.Handle(context.Background(), speechkit.Command{
		Type: speechkit.CommandCopyLastTranscription,
	})
	if err == nil {
		t.Fatal("Handle(copy last) error = nil, want quick actions not configured")
	}
	if got, want := err.Error(), "quick actions not configured"; got != want {
		t.Fatalf("Handle(copy last) error = %q, want %q", got, want)
	}
}

func TestDesktopCommandHandlerOpenQuickNoteDelegatesToQuickNoteService(t *testing.T) {
	service := &testCommandQuickNoteService{}
	handler := desktopCommandHandler{
		cfg:        &config.Config{},
		state:      &appState{},
		quickNotes: service,
	}

	err := handler.Handle(context.Background(), speechkit.Command{
		Type:   speechkit.CommandOpenQuickNote,
		NoteID: 55,
	})
	if err != nil {
		t.Fatalf("Handle(open quick note) error = %v", err)
	}
	if got, want := service.openedEditorNoteID, int64(55); got != want {
		t.Fatalf("service.openedEditorNoteID = %d, want %d", got, want)
	}
}

func TestDesktopCommandHandlerOpenQuickCaptureDelegatesToQuickNoteService(t *testing.T) {
	service := &testCommandQuickNoteService{}
	handler := desktopCommandHandler{
		cfg:        &config.Config{},
		state:      &appState{},
		quickNotes: service,
	}

	err := handler.Handle(context.Background(), speechkit.Command{
		Type: speechkit.CommandOpenQuickCapture,
	})
	if err != nil {
		t.Fatalf("Handle(open quick capture) error = %v", err)
	}
	if got, want := service.openCaptureCalls, 1; got != want {
		t.Fatalf("service.openCaptureCalls = %d, want %d", got, want)
	}
}

func TestDesktopCommandHandlerArmQuickNoteRecordingDelegatesToQuickNoteService(t *testing.T) {
	service := &testCommandQuickNoteService{}
	handler := desktopCommandHandler{
		cfg:        &config.Config{},
		state:      &appState{},
		quickNotes: service,
	}

	err := handler.Handle(context.Background(), speechkit.Command{
		Type:   speechkit.CommandArmQuickNoteRecording,
		NoteID: 77,
	})
	if err != nil {
		t.Fatalf("Handle(arm quick note recording) error = %v", err)
	}
	if got, want := service.armedRecordingID, int64(77); got != want {
		t.Fatalf("service.armedRecordingID = %d, want %d", got, want)
	}
}
