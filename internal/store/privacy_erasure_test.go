package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// SQLite scopes foreign_keys to a connection and database/sql keeps a pool, so
// setting it with one PRAGMA statement left every later connection on the
// default of OFF. A delete served by such a connection silently skipped the
// cascade. The setting belongs in the DSN, which applies it to each connection
// the pool opens.
func TestSQLiteEnforcesForeignKeysOnEveryPooledConnection(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()

	// Hold several connections at once so the pool has to open new ones rather
	// than hand back the first.
	const connections = 4
	held := make([]*sql.Conn, 0, connections)
	t.Cleanup(func() {
		for _, conn := range held {
			_ = conn.Close()
		}
	})
	for index := 0; index < connections; index++ {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("open connection %d: %v", index, err)
		}
		held = append(held, conn)
		var enabled int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
			t.Fatalf("read foreign_keys on connection %d: %v", index, err)
		}
		if enabled != 1 {
			t.Fatalf("connection %d has foreign_keys = %d, want 1", index, enabled)
		}
	}
}

// An erasure request must take the meeting apart completely: the transcript,
// the notes the user typed, the model's write-ups, the digests derived from
// them, and the screen captures — rows and image files alike. Screenshots are
// pictures of the user's desktop, and they used to survive the request.
func TestDeleteScopeErasesMeetingChildrenAndSnapshotFiles(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()
	alice := speechstorage.Scope{InstallID: "test-install", UserID: "alice"}
	bob := speechstorage.Scope{InstallID: "test-install", UserID: "bob"}

	aliceSession, alicePath := seedMeetingForPrivacyTest(t, s, alice, "alice meeting")
	bobSession, bobPath := seedMeetingForPrivacyTest(t, s, bob, "bob meeting")

	result, err := s.DeleteScope(ctx, alice)
	if err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}

	if len(result.SnapshotFilePaths) != 1 || result.SnapshotFilePaths[0] != alicePath {
		t.Fatalf("SnapshotFilePaths = %v, want exactly %q", result.SnapshotFilePaths, alicePath)
	}

	countFor := func(table string, sessionID int64) int {
		t.Helper()
		var count int
		if err := s.db.QueryRowContext(ctx, s.dialect.rebind(
			`SELECT COUNT(*) FROM `+table+` WHERE session_id = ?`), sessionID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return count
	}
	for _, table := range []string{
		"recording_session_segments",
		"recording_session_notes",
		"recording_session_snapshots",
		"recording_session_enhancements",
		"meeting_summary_batches",
	} {
		if got := countFor(table, aliceSession); got != 0 {
			t.Errorf("%s still holds %d rows for the erased meeting", table, got)
		}
		if got := countFor(table, bobSession); got != 1 {
			t.Errorf("%s holds %d rows for the untouched meeting, want 1", table, got)
		}
	}

	// The store only reports the paths; the caller unlinks them. Both files are
	// still on disk here, and only the reported one may be removed.
	if _, err := os.Stat(bobPath); err != nil {
		t.Errorf("the untouched meeting's snapshot file is gone: %v", err)
	}
	for _, path := range result.SnapshotFilePaths {
		if err := os.Remove(path); err != nil {
			t.Errorf("reported snapshot path could not be unlinked: %v", err)
		}
	}
	if _, err := os.Stat(alicePath); !os.IsNotExist(err) {
		t.Errorf("erased snapshot file still present: %v", err)
	}
	if filepath.Dir(alicePath) == "" {
		t.Error("snapshot path is not absolute")
	}
}

// Words and Replacements are written by the user, so an erasure request owns
// them the same way it owns a transcript.
func TestDeleteScopeErasesCustomizationRows(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()
	alice := speechstorage.Scope{InstallID: "test-install", UserID: "alice"}

	scopeID, err := s.scopeIDForScope(ctx, alice)
	if err != nil {
		t.Fatalf("scopeIDForScope: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, s.dialect.rebind(
		`INSERT INTO customization_words (id, scope_id, term, language, source, enabled)
		 VALUES ('word-1', ?, 'Kombify', 'en', 'settings', 1)`), scopeID); err != nil {
		t.Fatalf("insert customization word: %v", err)
	}

	if _, err := s.DeleteScope(ctx, alice); err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}

	var count int
	if err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT COUNT(*) FROM customization_words WHERE scope_id = ?`), scopeID).Scan(&count); err != nil {
		t.Fatalf("count customization_words: %v", err)
	}
	if count != 0 {
		t.Fatalf("customization_words still holds %d rows after erasure", count)
	}
}

// seedMeetingForPrivacyTest stores one meeting with everything an erasure or
// an export has to cover: a transcript segment, the user's notes, a write-up,
// a summary batch and a screen capture on disk. It returns the session ID and
// the snapshot file path.
func seedMeetingForPrivacyTest(t *testing.T, s *SQLiteStore, scope speechstorage.Scope, title string) (int64, string) {
	t.Helper()
	sessionID, err := s.SaveRecordingSession(scopeCtx(scope), RecordingSession{
		Kind: RecordingSessionKindMeeting, Title: title, Language: "en",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession(%s): %v", title, err)
	}
	segmentID, err := s.AppendRecordingSessionSegment(scopeCtx(scope), sessionID, RecordingSessionSegment{
		SegmentIndex: 0, Text: "we agreed to ship on Friday", IsFinal: true,
	})
	if err != nil {
		t.Fatalf("AppendRecordingSessionSegment(%s): %v", title, err)
	}
	if err := s.SaveRecordingSessionNotes(scopeCtx(scope), sessionID, RecordingSessionNotes{
		SessionID: sessionID, ContentMD: "my own note",
	}); err != nil {
		t.Fatalf("SaveRecordingSessionNotes(%s): %v", title, err)
	}
	if _, err := s.CreateRecordingSessionEnhancement(scopeCtx(scope), sessionID, RecordingSessionEnhancement{
		SessionID: sessionID, TemplateSlug: "default_meeting",
		Status: RecordingSessionEnhancementReady, ContentMD: "# Write-up",
	}); err != nil {
		t.Fatalf("CreateRecordingSessionEnhancement(%s): %v", title, err)
	}
	if _, err := s.UpsertMeetingSummaryBatch(scopeCtx(scope), MeetingSummaryBatch{
		SessionID: sessionID, BatchKey: title + "-batch", Level: 0,
		StartSegmentID: segmentID, EndSegmentID: segmentID,
		SourceFingerprint: "fp", Status: MeetingSummaryBatchReady,
		DigestJSON: `{"topics":["shipping"]}`,
	}); err != nil {
		t.Fatalf("UpsertMeetingSummaryBatch(%s): %v", title, err)
	}
	snapshot, err := s.SaveRecordingSessionSnapshot(scopeCtx(scope), sessionID, RecordingSessionSnapshotInput{
		CapturedMs: 1000, Data: []byte("not really a png"), MimeType: "image/png",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSessionSnapshot(%s): %v", title, err)
	}
	if _, err := os.Stat(snapshot.Path); err != nil {
		t.Fatalf("snapshot file was not written for %s: %v", title, err)
	}
	return sessionID, snapshot.Path
}
