package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordingSessionNotesKeepTheirTimestampsAcrossSaves(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "notes.db"), MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	sessionID, err := s.SaveRecordingSession(ctx, RecordingSession{
		Kind:  RecordingSessionKindMeeting,
		Title: "Notes meeting",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}

	empty, err := s.GetRecordingSessionNotes(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetRecordingSessionNotes on a fresh meeting: %v", err)
	}
	if empty == nil || empty.ContentMD != "" || len(empty.Blocks) != 0 {
		t.Fatalf("a meeting nobody typed into should have empty notes, got %+v", empty)
	}

	// The note pane autosaves while the user types, so the second save replaces
	// the first rather than adding to it.
	if err := s.SaveRecordingSessionNotes(ctx, sessionID, RecordingSessionNotes{
		ContentMD: "- pricing",
		Blocks:    []RecordingSessionNoteBlock{{ID: "a1", Text: "pricing", TsMs: 12000}},
	}); err != nil {
		t.Fatalf("SaveRecordingSessionNotes: %v", err)
	}
	if err := s.SaveRecordingSessionNotes(ctx, sessionID, RecordingSessionNotes{
		ContentMD: "- pricing\n- follow up with legal",
		Blocks: []RecordingSessionNoteBlock{
			{ID: "a1", Text: "pricing", TsMs: 12000},
			{ID: "a2", Text: "follow up with legal", TsMs: 45000},
		},
	}); err != nil {
		t.Fatalf("SaveRecordingSessionNotes (update): %v", err)
	}

	notes, err := s.GetRecordingSessionNotes(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetRecordingSessionNotes: %v", err)
	}
	if len(notes.Blocks) != 2 {
		t.Fatalf("blocks = %d, want 2 — the update should replace, not append", len(notes.Blocks))
	}
	if notes.Blocks[1].TsMs != 45000 || notes.Blocks[1].ID != "a2" {
		t.Fatalf("the note lost the moment it was written: %+v", notes.Blocks[1])
	}

	session, err := s.GetRecordingSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	if session.Notes == nil || len(session.Notes.Blocks) != 2 {
		t.Fatalf("the session detail did not carry the notes: %+v", session.Notes)
	}
}

// Retention exists so a machine does not accumulate a transcript of every call
// it ever heard. It must not take the meeting someone deliberately kept, and it
// must not touch a meeting still being recorded however long it has run.
func TestMeetingRetentionKeepsPinnedAndUnfinishedMeetings(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "retention.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MeetingRetentionDays: 30})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	old := time.Now().Add(-90 * 24 * time.Hour)

	newMeeting := func(title string) int64 {
		t.Helper()
		id, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: title})
		if err != nil {
			t.Fatalf("SaveRecordingSession(%s): %v", title, err)
		}
		return id
	}
	finish := func(id int64) {
		t.Helper()
		if err := s.FinishRecordingSession(ctx, id, "", old); err != nil {
			t.Fatalf("FinishRecordingSession: %v", err)
		}
	}

	expired := newMeeting("expired")
	finish(expired)
	pinned := newMeeting("pinned")
	finish(pinned)
	if err := s.SetRecordingSessionPinned(ctx, pinned, true); err != nil {
		t.Fatalf("SetRecordingSessionPinned: %v", err)
	}
	stillRunning := newMeeting("still running")
	dictation, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindDictation, Title: "dictation"})
	if err != nil {
		t.Fatalf("SaveRecordingSession(dictation): %v", err)
	}
	finish(dictation)

	s.enforceMeetingRetention()

	if _, err := s.GetRecordingSession(ctx, expired); err == nil {
		t.Fatal("an expired meeting survived the retention sweep")
	}
	for name, id := range map[string]int64{"pinned": pinned, "still running": stillRunning, "dictation": dictation} {
		if _, err := s.GetRecordingSession(ctx, id); err != nil {
			t.Fatalf("the %s session was swept away: %v", name, err)
		}
	}
}

func TestMeetingRetentionOffByDefaultKeepsEverything(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "keep.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	id, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "ancient"})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	if err := s.FinishRecordingSession(ctx, id, "", time.Now().Add(-5*365*24*time.Hour)); err != nil {
		t.Fatalf("FinishRecordingSession: %v", err)
	}

	s.enforceMeetingRetention()

	if _, err := s.GetRecordingSession(ctx, id); err != nil {
		t.Fatalf("a meeting was discarded although retention is off: %v", err)
	}
}

// Finishing a meeting passes no summary — the write-up arrives later from the
// enhancement job. Finish must therefore leave an already-generated summary
// (and its status) alone instead of overwriting it with "" and stamping the
// empty result "ready".
func TestFinishRecordingSessionKeepsExistingSummary(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "finish.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	id, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "standup"})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	if err := s.UpdateRecordingSessionSummary(ctx, id, "Wichtige Beschlüsse"); err != nil {
		t.Fatalf("UpdateRecordingSessionSummary: %v", err)
	}

	if err := s.FinishRecordingSession(ctx, id, "", time.Now()); err != nil {
		t.Fatalf("FinishRecordingSession: %v", err)
	}
	got, err := s.GetRecordingSession(ctx, id)
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	if got.Summary != "Wichtige Beschlüsse" {
		t.Fatalf("finishing without a summary erased the existing one: %q", got.Summary)
	}
	if got.SummaryStatus != RecordingSessionSummaryReady {
		t.Fatalf("summary status = %q, want ready", got.SummaryStatus)
	}

	// A summary handed to Finish still wins.
	if err := s.FinishRecordingSession(ctx, id, "Neue Fassung", time.Now()); err != nil {
		t.Fatalf("FinishRecordingSession with summary: %v", err)
	}
	got, err = s.GetRecordingSession(ctx, id)
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	if got.Summary != "Neue Fassung" {
		t.Fatalf("a provided summary did not replace the old one: %q", got.Summary)
	}
}

// The meetings dashboard asks for meetings only; the filter must be applied by
// the store so a library where dictations outnumber meetings does not push the
// meetings past the row limit.
func TestListRecordingSessionsFiltersByKind(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "kinds.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindDictation, Title: "note 1"}); err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	meetingID, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "weekly"})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	if _, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindDictation, Title: "note 2"}); err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}

	// Limit 1 with the kind filter must still surface the meeting even though
	// a newer dictation exists — that is what the in-memory filter got wrong.
	meetings, err := s.ListRecordingSessions(ctx, ListOpts{Kind: "meeting", Limit: 1})
	if err != nil {
		t.Fatalf("ListRecordingSessions(kind=meeting): %v", err)
	}
	if len(meetings) != 1 || meetings[0].ID != meetingID {
		t.Fatalf("kind filter returned %+v, want just the meeting", meetings)
	}

	all, err := s.ListRecordingSessions(ctx, ListOpts{})
	if err != nil {
		t.Fatalf("ListRecordingSessions: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered list = %d sessions, want 3", len(all))
	}
}

func TestRecordingSessionStoreLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "recording_sessions.db")
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: dbPath, MaxAudioStorageMB: 100})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	defer s.Close()

	ctx := context.Background()
	sessionID, err := s.SaveRecordingSession(ctx, RecordingSession{
		ExternalID:     "meeting-1",
		Kind:           RecordingSessionKindMeeting,
		Title:          "Planning meeting",
		Language:       "de-DE",
		Provider:       "deepgram",
		Model:          "nova-3",
		InputSource:    "system_loopback",
		ProcessingMode: "segment_batch",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	segmentID, err := s.AppendRecordingSessionSegment(ctx, sessionID, RecordingSessionSegment{
		SegmentIndex:   0,
		ProviderItemID: "deepgram:1",
		Text:           "Hallo zusammen",
		IsFinal:        true,
		StartedMs:      0,
		EndedMs:        1200,
	})
	if err != nil {
		t.Fatalf("AppendRecordingSessionSegment: %v", err)
	}
	updatedSegmentID, err := s.AppendRecordingSessionSegment(ctx, sessionID, RecordingSessionSegment{
		SegmentIndex:   0,
		ProviderItemID: "deepgram:retry-1",
		Text:           "Hallo zusammen korrigiert",
		IsFinal:        true,
		StartedMs:      0,
		EndedMs:        1300,
	})
	if err != nil {
		t.Fatalf("AppendRecordingSessionSegment retry: %v", err)
	}
	if updatedSegmentID != segmentID {
		t.Fatalf("retry segment id = %d, want original %d", updatedSegmentID, segmentID)
	}
	if err := s.UpdateRecordingSessionCaptureStatus(ctx, sessionID, RecordingSessionCaptureRecording, time.Now()); err != nil {
		t.Fatalf("UpdateRecordingSessionCaptureStatus recording: %v", err)
	}
	if err := s.UpdateRecordingSessionCaptureStatus(ctx, sessionID, RecordingSessionCapturePaused, time.Now()); err != nil {
		t.Fatalf("UpdateRecordingSessionCaptureStatus paused: %v", err)
	}
	if err := s.UpdateRecordingSessionSummaryStatus(ctx, sessionID, RecordingSessionSummaryRunning, "", time.Now()); err != nil {
		t.Fatalf("UpdateRecordingSessionSummaryStatus running: %v", err)
	}
	if err := s.UpdateRecordingSessionSummary(ctx, sessionID, "Vorab Zusammenfassung"); err != nil {
		t.Fatalf("UpdateRecordingSessionSummary: %v", err)
	}
	if err := s.FinishRecordingSession(ctx, sessionID, "Kurze Zusammenfassung", time.Now()); err != nil {
		t.Fatalf("FinishRecordingSession: %v", err)
	}
	listed, err := s.ListRecordingSessions(ctx, ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListRecordingSessions: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != sessionID {
		t.Fatalf("listed recording sessions = %+v", listed)
	}

	got, err := s.GetRecordingSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	if got.Kind != RecordingSessionKindMeeting || got.Status != RecordingSessionStatusFinished {
		t.Fatalf("recording session status = %s/%s", got.Kind, got.Status)
	}
	if got.CaptureStatus != RecordingSessionCaptureStopped || got.CaptureStartedAt.IsZero() || got.CapturePausedAt.IsZero() || got.CaptureStoppedAt.IsZero() {
		t.Fatalf("recording session capture fields = %+v", got)
	}
	if got.SummaryStatus != RecordingSessionSummaryReady || got.SummaryUpdatedAt.IsZero() || got.SummaryError != "" {
		t.Fatalf("recording session summary fields = %+v", got)
	}
	if got.Language != "de-DE" || got.InputSource != "system_loopback" || got.ProcessingMode != "segment_batch" {
		t.Fatalf("recording session metadata = %+v", got)
	}
	if len(got.Segments) != 1 || got.Segments[0].Text != "Hallo zusammen korrigiert" || got.Segments[0].ProviderItemID != "deepgram:retry-1" || !got.Segments[0].IsFinal {
		t.Fatalf("recording session segments = %+v", got.Segments)
	}
	if got.Summary != "Kurze Zusammenfassung" || got.EndedAt.IsZero() {
		t.Fatalf("recording session finish fields = %+v", got)
	}
	if err := s.DeleteRecordingSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteRecordingSession: %v", err)
	}
	if _, err := s.GetRecordingSession(ctx, sessionID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRecordingSession after delete err = %v, want sql.ErrNoRows", err)
	}
}

func TestFinishRecordingSessionWithoutSummaryPreservesSummaryState(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "recording_sessions.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	sessionID, err := s.SaveRecordingSession(ctx, RecordingSession{
		Kind:  RecordingSessionKindMeeting,
		Title: "Planning meeting",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	if err := s.UpdateRecordingSessionSummary(ctx, sessionID, "Existing summary"); err != nil {
		t.Fatalf("UpdateRecordingSessionSummary: %v", err)
	}

	if err := s.FinishRecordingSession(ctx, sessionID, "", time.Now()); err != nil {
		t.Fatalf("FinishRecordingSession: %v", err)
	}
	got, err := s.GetRecordingSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	if got.Summary != "Existing summary" || got.SummaryStatus != RecordingSessionSummaryReady {
		t.Fatalf("finish without summary changed summary state: %+v", got)
	}
}

func TestListRecordingSessionsFiltersBeforeApplyingLimit(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "recording_sessions.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	meetingID, err := s.SaveRecordingSession(ctx, RecordingSession{
		Kind:  RecordingSessionKindMeeting,
		Title: "Planning meeting",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession meeting: %v", err)
	}
	if _, err := s.SaveRecordingSession(ctx, RecordingSession{
		Kind:  RecordingSessionKindDictation,
		Title: "Newer dictation",
	}); err != nil {
		t.Fatalf("SaveRecordingSession dictation: %v", err)
	}

	got, err := s.ListRecordingSessions(ctx, ListOpts{
		Limit: 1,
		Kind:  string(RecordingSessionKindMeeting),
	})
	if err != nil {
		t.Fatalf("ListRecordingSessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != meetingID {
		t.Fatalf("filtered recording sessions = %+v, want meeting %d", got, meetingID)
	}
}
