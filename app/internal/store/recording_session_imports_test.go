package store

import (
	"context"
	"testing"
	"time"
)

func TestRecordingSessionImportsRoundTripAndComeDue(t *testing.T) {
	s := newSnapshotTestStore(t)
	ctx := context.Background()
	sessionID := newSnapshotTestSession(t, s)
	later := newSnapshotTestSession(t, s)
	now := time.Now().UTC().Truncate(time.Second)

	stored, err := s.UpsertRecordingSessionImport(ctx, RecordingSessionImport{
		SessionID:     sessionID,
		Kind:          RecordingSessionImportTeamsTranscript,
		NextAttemptAt: now.Add(-time.Minute),
		DeadlineAt:    now.Add(45 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if stored.ID == 0 || stored.Status != RecordingSessionImportWaiting || !stored.DeadlineAt.Equal(now.Add(45*time.Minute)) {
		t.Fatalf("stored = %+v", stored)
	}
	if _, err := s.UpsertRecordingSessionImport(ctx, RecordingSessionImport{
		SessionID: later, Kind: RecordingSessionImportTeamsTranscript, NextAttemptAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	due, err := s.ListDueRecordingSessionImports(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(due) != 1 || due[0].SessionID != sessionID {
		t.Fatalf("due = %+v, want only the first meeting", due)
	}

	stored.Status = RecordingSessionImportImported
	stored.Attempts = 2
	stored.ExternalMeetingID = "MSo1"
	stored.ExternalItemID = "T1"
	stored.Subject = "Weekly"
	updated, err := s.UpsertRecordingSessionImport(ctx, stored)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != stored.ID || updated.Status != RecordingSessionImportImported || updated.ExternalItemID != "T1" || updated.Attempts != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	if due, _ := s.ListDueRecordingSessionImports(ctx, now.Add(time.Hour), 10); len(due) != 1 || due[0].SessionID != later {
		t.Fatalf("due after import = %+v, want only the other meeting", due)
	}

	listed, err := s.ListRecordingSessionImports(ctx, sessionID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list = %+v, %v", listed, err)
	}

	if err := s.DeleteRecordingSession(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	if listed, _ := s.ListRecordingSessionImports(ctx, later); len(listed) != 1 {
		t.Fatalf("deleting one meeting touched another's import: %+v", listed)
	}
}

// An import due this very second is due; SQLite compares these times as text.
func TestRecordingSessionImportsAreDueAtTheExactSecond(t *testing.T) {
	s := newSnapshotTestStore(t)
	ctx := context.Background()
	sessionID := newSnapshotTestSession(t, s)
	now := time.Date(2026, 9, 16, 10, 32, 0, 0, time.UTC)
	if _, err := s.UpsertRecordingSessionImport(ctx, RecordingSessionImport{
		SessionID: sessionID, Kind: RecordingSessionImportTeamsTranscript, NextAttemptAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	due, err := s.ListDueRecordingSessionImports(ctx, now, 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %+v, %v", due, err)
	}
	if !due[0].NextAttemptAt.Equal(now) {
		t.Fatalf("next attempt read back as %s", due[0].NextAttemptAt)
	}
	if due, _ := s.ListDueRecordingSessionImports(ctx, now.Add(-time.Second), 10); len(due) != 0 {
		t.Fatalf("an import a second in the future is not due: %+v", due)
	}
}
