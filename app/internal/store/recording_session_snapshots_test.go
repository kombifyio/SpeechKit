package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newSnapshotTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "snapshots.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newSnapshotTestSession(t *testing.T, s *SQLiteStore) int64 {
	t.Helper()
	id, err := s.SaveRecordingSession(context.Background(), RecordingSession{
		Kind:      RecordingSessionKindMeeting,
		Status:    RecordingSessionStatusActive,
		Language:  "de",
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	return id
}

func TestRecordingSessionSnapshotsRoundTrip(t *testing.T) {
	s := newSnapshotTestStore(t)
	ctx := context.Background()
	sessionID := newSnapshotTestSession(t, s)

	saved, err := s.SaveRecordingSessionSnapshot(ctx, sessionID, RecordingSessionSnapshotInput{
		CapturedMs: 90_000,
		Data:       []byte("fake-png-bytes"),
		Width:      1920,
		Height:     1080,
		Monitor:    `\\.\DISPLAY1`,
		Note:       "  Architekturdiagramm  ",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSessionSnapshot: %v", err)
	}
	if saved.ID <= 0 {
		t.Fatalf("expected snapshot id, got %d", saved.ID)
	}
	if saved.MimeType != "image/png" {
		t.Fatalf("expected default mime image/png, got %q", saved.MimeType)
	}
	if saved.Note != "Architekturdiagramm" {
		t.Fatalf("expected trimmed note, got %q", saved.Note)
	}
	if _, err := os.Stat(saved.Path); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
	if got, _ := os.ReadFile(saved.Path); string(got) != "fake-png-bytes" {
		t.Fatalf("snapshot file content mismatch: %q", got)
	}

	// A second snapshot earlier in the timeline must list first.
	if _, err := s.SaveRecordingSessionSnapshot(ctx, sessionID, RecordingSessionSnapshotInput{
		CapturedMs: 30_000,
		Data:       []byte("earlier"),
	}); err != nil {
		t.Fatalf("SaveRecordingSessionSnapshot second: %v", err)
	}
	list, err := s.ListRecordingSessionSnapshots(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListRecordingSessionSnapshots: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(list))
	}
	if list[0].CapturedMs != 30_000 || list[1].CapturedMs != 90_000 {
		t.Fatalf("expected timeline order, got %d then %d", list[0].CapturedMs, list[1].CapturedMs)
	}

	got, err := s.GetRecordingSessionSnapshot(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetRecordingSessionSnapshot: %v", err)
	}
	if got.Path != saved.Path || got.Width != 1920 || got.Height != 1080 || got.Monitor != `\\.\DISPLAY1` {
		t.Fatalf("snapshot round trip mismatch: %+v", got)
	}

	// The session detail carries its snapshots.
	session, err := s.GetRecordingSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	if len(session.Snapshots) != 2 {
		t.Fatalf("expected session detail to load 2 snapshots, got %d", len(session.Snapshots))
	}
}

func TestRecordingSessionSnapshotSaveRejectsEmptyImage(t *testing.T) {
	s := newSnapshotTestStore(t)
	sessionID := newSnapshotTestSession(t, s)
	if _, err := s.SaveRecordingSessionSnapshot(context.Background(), sessionID, RecordingSessionSnapshotInput{}); err == nil {
		t.Fatal("expected error for empty image data")
	}
}

func TestRecordingSessionSnapshotSaveRejectsUnknownSession(t *testing.T) {
	s := newSnapshotTestStore(t)
	if _, err := s.SaveRecordingSessionSnapshot(context.Background(), 9999, RecordingSessionSnapshotInput{Data: []byte("x")}); err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestDeleteRecordingSessionSnapshotRemovesFile(t *testing.T) {
	s := newSnapshotTestStore(t)
	ctx := context.Background()
	sessionID := newSnapshotTestSession(t, s)

	saved, err := s.SaveRecordingSessionSnapshot(ctx, sessionID, RecordingSessionSnapshotInput{Data: []byte("x")})
	if err != nil {
		t.Fatalf("SaveRecordingSessionSnapshot: %v", err)
	}
	if err := s.DeleteRecordingSessionSnapshot(ctx, saved.ID); err != nil {
		t.Fatalf("DeleteRecordingSessionSnapshot: %v", err)
	}
	if _, err := os.Stat(saved.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected snapshot file removed, stat err = %v", err)
	}
	if _, err := s.GetRecordingSessionSnapshot(ctx, saved.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}
	if err := s.DeleteRecordingSessionSnapshot(ctx, saved.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows for double delete, got %v", err)
	}
}

func TestDeleteRecordingSessionRemovesSnapshotFiles(t *testing.T) {
	s := newSnapshotTestStore(t)
	ctx := context.Background()
	sessionID := newSnapshotTestSession(t, s)

	saved, err := s.SaveRecordingSessionSnapshot(ctx, sessionID, RecordingSessionSnapshotInput{Data: []byte("x")})
	if err != nil {
		t.Fatalf("SaveRecordingSessionSnapshot: %v", err)
	}
	if err := s.DeleteRecordingSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteRecordingSession: %v", err)
	}
	if _, err := os.Stat(saved.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected snapshot file removed with session, stat err = %v", err)
	}
	list, err := s.loadRecordingSessionSnapshots(ctx, sessionID)
	if err != nil {
		t.Fatalf("loadRecordingSessionSnapshots: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected snapshot rows cascaded away, got %d", len(list))
	}
}

func TestMeetingRetentionRemovesSnapshotFiles(t *testing.T) {
	s := newSnapshotTestStore(t)
	ctx := context.Background()
	s.meetingRetentionDays = 30

	old := time.Now().Add(-60 * 24 * time.Hour).UTC()
	sessionID, err := s.SaveRecordingSession(ctx, RecordingSession{
		Kind:      RecordingSessionKindMeeting,
		Status:    RecordingSessionStatusFinished,
		Language:  "de",
		StartedAt: old,
		EndedAt:   old.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	saved, err := s.SaveRecordingSessionSnapshot(ctx, sessionID, RecordingSessionSnapshotInput{Data: []byte("x")})
	if err != nil {
		t.Fatalf("SaveRecordingSessionSnapshot: %v", err)
	}

	s.enforceMeetingRetention()

	if _, err := os.Stat(saved.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected retention to remove snapshot file, stat err = %v", err)
	}
	if _, err := s.GetRecordingSession(ctx, sessionID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected session discarded by retention, got %v", err)
	}
}
