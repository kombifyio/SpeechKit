package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/store"
)

type fakeQuickNoteHost struct {
	editorNoteID     int64
	captureNoteID    int64
	closeCaptureCall int
}

func (h *fakeQuickNoteHost) OpenEditor(noteID int64) error {
	h.editorNoteID = noteID
	return nil
}

func (h *fakeQuickNoteHost) OpenCapture(noteID int64) error {
	h.captureNoteID = noteID
	return nil
}

func (h *fakeQuickNoteHost) CloseCapture() error {
	h.closeCaptureCall++
	return nil
}

type fakeQuickNoteStore struct {
	savedQuickNoteID   int64
	savedQuickNoteText string
}

func (s *fakeQuickNoteStore) SaveTranscription(context.Context, string, string, string, string, int64, int64, []byte) error {
	return nil
}

func (s *fakeQuickNoteStore) GetTranscription(context.Context, int64) (*store.Transcription, error) {
	return nil, nil
}

func (s *fakeQuickNoteStore) ListTranscriptions(context.Context, store.ListOpts) ([]store.Transcription, error) {
	return nil, nil
}

func (s *fakeQuickNoteStore) TranscriptionCount(context.Context) (int, error) {
	return 0, nil
}

func (s *fakeQuickNoteStore) SaveQuickNote(_ context.Context, text, _, _ string, _, _ int64, _ []byte) (int64, error) {
	s.savedQuickNoteText = text
	if s.savedQuickNoteID == 0 {
		s.savedQuickNoteID = 101
	}
	return s.savedQuickNoteID, nil
}

func (s *fakeQuickNoteStore) GetQuickNote(context.Context, int64) (*store.QuickNote, error) {
	return &store.QuickNote{}, nil
}

func (s *fakeQuickNoteStore) ListQuickNotes(context.Context, store.ListOpts) ([]store.QuickNote, error) {
	return nil, nil
}

func (s *fakeQuickNoteStore) UpdateQuickNote(context.Context, int64, string) error {
	return nil
}

func (s *fakeQuickNoteStore) UpdateQuickNoteCapture(context.Context, int64, string, string, int64, int64, []byte) error {
	return nil
}

func (s *fakeQuickNoteStore) PinQuickNote(context.Context, int64, bool) error {
	return nil
}

func (s *fakeQuickNoteStore) DeleteQuickNote(context.Context, int64) error {
	return nil
}

func (s *fakeQuickNoteStore) QuickNoteCount(context.Context) (int, error) {
	return 0, nil
}

func (s *fakeQuickNoteStore) Stats(context.Context) (store.Stats, error) {
	return store.Stats{}, nil
}

func (s *fakeQuickNoteStore) Close() error {
	return nil
}

func TestDesktopQuickNoteServiceOpenCaptureArmsRuntimeState(t *testing.T) {
	state := &appState{}
	host := &fakeQuickNoteHost{}
	feedbackStore := &fakeQuickNoteStore{savedQuickNoteID: 33}
	service := desktopQuickNoteService{
		cfg:           &config.Config{General: config.GeneralConfig{Language: "en"}},
		state:         state,
		feedbackStore: feedbackStore,
		host:          host,
	}

	noteID, err := service.OpenCapture(context.Background())
	if err != nil {
		t.Fatalf("OpenCapture() error = %v", err)
	}
	if got, want := noteID, int64(33); got != want {
		t.Fatalf("noteID = %d, want %d", got, want)
	}
	if got, want := host.captureNoteID, int64(33); got != want {
		t.Fatalf("host.captureNoteID = %d, want %d", got, want)
	}

	runtime := state.runtimeStateLocked()
	if !runtime.quickCaptureMode {
		t.Fatal("runtime.quickCaptureMode = false, want true")
	}
	if !runtime.quickCaptureAutoStart {
		t.Fatal("runtime.quickCaptureAutoStart = false, want true")
	}
	if got, want := runtime.quickCaptureNoteID, int64(33); got != want {
		t.Fatalf("runtime.quickCaptureNoteID = %d, want %d", got, want)
	}
}

func TestDesktopQuickNoteServiceCloseCaptureClearsRuntimeState(t *testing.T) {
	state := &appState{}
	state.armQuickCapture(44)
	host := &fakeQuickNoteHost{}
	service := desktopQuickNoteService{
		state: state,
		host:  host,
	}

	err := service.CloseCapture()
	if err != nil {
		t.Fatalf("CloseCapture() error = %v", err)
	}
	if got, want := host.closeCaptureCall, 1; got != want {
		t.Fatalf("host.closeCaptureCall = %d, want %d", got, want)
	}

	runtime := state.runtimeStateLocked()
	if runtime.quickCaptureMode {
		t.Fatal("runtime.quickCaptureMode = true, want false")
	}
	if runtime.quickCaptureAutoStart {
		t.Fatal("runtime.quickCaptureAutoStart = true, want false")
	}
	if got, want := runtime.quickCaptureNoteID, int64(0); got != want {
		t.Fatalf("runtime.quickCaptureNoteID = %d, want %d", got, want)
	}
}

func TestDesktopQuickNoteServiceOpenEditorUsesHost(t *testing.T) {
	host := &fakeQuickNoteHost{}
	service := desktopQuickNoteService{host: host}

	err := service.OpenEditor(55)
	if err != nil {
		t.Fatalf("OpenEditor() error = %v", err)
	}
	if got, want := host.editorNoteID, int64(55); got != want {
		t.Fatalf("host.editorNoteID = %d, want %d", got, want)
	}
}

func TestDesktopQuickNoteServiceArmRecordingSetsMode(t *testing.T) {
	state := &appState{}
	service := desktopQuickNoteService{state: state}

	service.ArmRecording(77)

	runtime := state.runtimeStateLocked()
	if !runtime.quickNoteMode {
		t.Fatal("runtime.quickNoteMode = false, want true")
	}
	if runtime.quickCaptureMode {
		t.Fatal("runtime.quickCaptureMode = true, want false")
	}
	if got, want := runtime.quickCaptureNoteID, int64(77); got != want {
		t.Fatalf("runtime.quickCaptureNoteID = %d, want %d", got, want)
	}
}

func TestDesktopQuickNoteServiceOpenCaptureWithoutStoreStillOpensHost(t *testing.T) {
	state := &appState{}
	host := &fakeQuickNoteHost{}
	service := desktopQuickNoteService{
		cfg:   &config.Config{General: config.GeneralConfig{Language: "en"}},
		state: state,
		host:  host,
	}

	noteID, err := service.OpenCapture(context.Background())
	if err != nil {
		t.Fatalf("OpenCapture() error = %v", err)
	}
	if got, want := noteID, int64(0); got != want {
		t.Fatalf("noteID = %d, want %d", got, want)
	}
	if got, want := host.captureNoteID, int64(0); got != want {
		t.Fatalf("host.captureNoteID = %d, want %d", got, want)
	}
}

func TestFakeQuickNoteStoreRoundTripTimestampTypes(t *testing.T) {
	var _ = store.QuickNote{CreatedAt: time.Time{}, UpdatedAt: time.Time{}}
}

func TestDesktopQuickNoteServiceCloseCaptureDeletesEmptyPlaceholderNote(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	feedbackStore, err := store.New(store.StoreConfig{
		Backend:           "sqlite",
		SQLitePath:        dbPath,
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer feedbackStore.Close()

	state := &appState{}
	host := &fakeQuickNoteHost{}
	service := desktopQuickNoteService{
		cfg:           &config.Config{General: config.GeneralConfig{Language: "en"}},
		state:         state,
		feedbackStore: feedbackStore,
		host:          host,
	}

	noteID, err := service.OpenCapture(context.Background())
	if err != nil {
		t.Fatalf("OpenCapture() error = %v", err)
	}
	if noteID == 0 {
		t.Fatal("expected placeholder note ID")
	}

	if err := service.CloseCapture(); err != nil {
		t.Fatalf("CloseCapture() error = %v", err)
	}

	count, err := feedbackStore.QuickNoteCount(context.Background())
	if err != nil {
		t.Fatalf("QuickNoteCount() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("QuickNoteCount = %d, want 0 after closing empty capture", count)
	}
}
