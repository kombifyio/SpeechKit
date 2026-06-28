package store

import (
	"context"
	"path/filepath"
	"testing"

	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// newSQLiteStoreForPrivacyTest opens a fresh in-process SQLite store in the
// test's temp directory.
func newSQLiteStoreForPrivacyTest(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "privacy_test.db")
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath:        dbPath,
		SaveAudio:         false,
		MaxAudioStorageMB: 0,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// scopeCtx attaches a scope to a background context using the storage package
// helpers so the existing SaveTranscription path picks it up.
func scopeCtx(scope speechstorage.Scope) context.Context {
	return speechstorage.WithScope(context.Background(), scope)
}

func TestExportScopeReturnsAllScopedRows(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()

	alice := speechstorage.Scope{InstallID: "test-install", UserID: "alice"}

	// Seed a transcription under alice's scope.
	if err := s.SaveTranscription(scopeCtx(alice), "hello alice", "en", "test-provider", "test-model", 1000, 50, nil); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	// Seed a quick note under alice's scope.
	if _, err := s.SaveQuickNote(scopeCtx(alice), "alice note", "en", "test-provider", 500, 30, nil); err != nil {
		t.Fatalf("SaveQuickNote: %v", err)
	}
	recordingID, err := s.SaveRecordingSession(scopeCtx(alice), RecordingSession{
		Kind:           RecordingSessionKindMeeting,
		Title:          "alice meeting",
		Language:       "en",
		InputSource:    "system_loopback",
		ProcessingMode: "segment_batch",
	})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	if _, err := s.AppendRecordingSessionSegment(scopeCtx(alice), recordingID, RecordingSessionSegment{SegmentIndex: 0, Text: "alice segment", IsFinal: true}); err != nil {
		t.Fatalf("AppendRecordingSessionSegment: %v", err)
	}

	export, err := s.ExportScope(ctx, alice)
	if err != nil {
		t.Fatalf("ExportScope: %v", err)
	}
	if len(export.Transcriptions) != 1 {
		t.Errorf("Transcriptions: want 1, got %d", len(export.Transcriptions))
	}
	if export.Transcriptions[0].Text != "hello alice" {
		t.Errorf("Transcription text: want %q, got %q", "hello alice", export.Transcriptions[0].Text)
	}
	if len(export.QuickNotes) != 1 {
		t.Errorf("QuickNotes: want 1, got %d", len(export.QuickNotes))
	}
	if export.QuickNotes[0].Text != "alice note" {
		t.Errorf("QuickNote text: want %q, got %q", "alice note", export.QuickNotes[0].Text)
	}
	if len(export.RecordingSessions) != 1 || export.RecordingSessions[0].Title != "alice meeting" {
		t.Errorf("RecordingSessions: got %+v, want alice meeting", export.RecordingSessions)
	}
	if len(export.RecordingSessions[0].Segments) != 1 || export.RecordingSessions[0].Segments[0].Text != "alice segment" {
		t.Errorf("RecordingSession segments: %+v", export.RecordingSessions[0].Segments)
	}
}

func TestScopePrivacyStoreContractAcrossBackends(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		privacy, ok := s.(ScopePrivacyStore)
		if !ok {
			t.Fatalf("%T does not implement ScopePrivacyStore", s)
		}

		alice := speechstorage.Scope{InstallID: "contract-install", UserID: "alice"}
		bob := speechstorage.Scope{InstallID: "contract-install", UserID: "bob"}
		if err := s.SaveTranscription(scopeCtx(alice), "alice contract text", "en", "p", "m", 1000, 50, []byte{0, 1, 2, 3}); err != nil {
			t.Fatalf("SaveTranscription alice: %v", err)
		}
		if _, err := s.SaveQuickNote(scopeCtx(alice), "alice contract note", "en", "p", 500, 30, nil); err != nil {
			t.Fatalf("SaveQuickNote alice: %v", err)
		}
		if err := s.SaveTranscription(scopeCtx(bob), "bob contract text", "en", "p", "m", 1000, 50, nil); err != nil {
			t.Fatalf("SaveTranscription bob: %v", err)
		}

		export, err := privacy.ExportScope(context.Background(), alice)
		if err != nil {
			t.Fatalf("ExportScope alice: %v", err)
		}
		if len(export.Transcriptions) != 1 || export.Transcriptions[0].Text != "alice contract text" {
			t.Fatalf("alice transcriptions = %+v", export.Transcriptions)
		}
		if len(export.QuickNotes) != 1 || export.QuickNotes[0].Text != "alice contract note" {
			t.Fatalf("alice quick notes = %+v", export.QuickNotes)
		}
		if len(export.AudioAssetPaths) != 1 {
			t.Fatalf("alice audio paths = %+v, want one saved audio path", export.AudioAssetPaths)
		}

		deleted, err := privacy.DeleteScope(context.Background(), alice)
		if err != nil {
			t.Fatalf("DeleteScope alice: %v", err)
		}
		if deleted.RowsDeleted < 2 {
			t.Fatalf("RowsDeleted = %d, want at least transcription + quick note rows", deleted.RowsDeleted)
		}
		if len(deleted.AudioFilePaths) != 1 {
			t.Fatalf("AudioFilePaths = %+v, want one saved audio path", deleted.AudioFilePaths)
		}

		aliceAfter, err := privacy.ExportScope(context.Background(), alice)
		if err != nil {
			t.Fatalf("ExportScope alice after delete: %v", err)
		}
		if len(aliceAfter.Transcriptions) != 0 || len(aliceAfter.QuickNotes) != 0 {
			t.Fatalf("alice data remained after delete: transcriptions=%+v quick_notes=%+v", aliceAfter.Transcriptions, aliceAfter.QuickNotes)
		}
		bobAfter, err := privacy.ExportScope(context.Background(), bob)
		if err != nil {
			t.Fatalf("ExportScope bob after delete: %v", err)
		}
		if len(bobAfter.Transcriptions) != 1 || bobAfter.Transcriptions[0].Text != "bob contract text" {
			t.Fatalf("bob data not preserved: %+v", bobAfter.Transcriptions)
		}
	})
}

func TestExportScopeDoesNotLeakOtherScopeData(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()

	alice := speechstorage.Scope{InstallID: "test-install", UserID: "alice"}
	bob := speechstorage.Scope{InstallID: "test-install", UserID: "bob"}

	if err := s.SaveTranscription(scopeCtx(alice), "alice data", "en", "p", "m", 1000, 50, nil); err != nil {
		t.Fatalf("SaveTranscription alice: %v", err)
	}
	if err := s.SaveTranscription(scopeCtx(bob), "bob data", "en", "p", "m", 1000, 50, nil); err != nil {
		t.Fatalf("SaveTranscription bob: %v", err)
	}

	aliceExport, err := s.ExportScope(ctx, alice)
	if err != nil {
		t.Fatalf("ExportScope alice: %v", err)
	}
	for _, tr := range aliceExport.Transcriptions {
		if tr.Text != "alice data" {
			t.Errorf("alice export contains non-alice data: %q", tr.Text)
		}
	}
	if len(aliceExport.Transcriptions) != 1 {
		t.Errorf("alice export: want 1 transcription, got %d", len(aliceExport.Transcriptions))
	}
}

func TestDeleteScopeRemovesAllScopedRowsAndOnlyThose(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()

	alice := speechstorage.Scope{InstallID: "test-install", UserID: "alice"}
	bob := speechstorage.Scope{InstallID: "test-install", UserID: "bob"}

	if err := s.SaveTranscription(scopeCtx(alice), "alice-data", "en", "p", "m", 1000, 50, nil); err != nil {
		t.Fatalf("SaveTranscription alice: %v", err)
	}
	if _, err := s.SaveQuickNote(scopeCtx(alice), "alice note", "en", "p", 500, 30, nil); err != nil {
		t.Fatalf("SaveQuickNote alice: %v", err)
	}
	recordingID, err := s.SaveRecordingSession(scopeCtx(alice), RecordingSession{Kind: RecordingSessionKindMeeting, Title: "alice meeting", Language: "en"})
	if err != nil {
		t.Fatalf("SaveRecordingSession alice: %v", err)
	}
	if _, err := s.AppendRecordingSessionSegment(scopeCtx(alice), recordingID, RecordingSessionSegment{SegmentIndex: 0, Text: "alice segment", IsFinal: true}); err != nil {
		t.Fatalf("AppendRecordingSessionSegment alice: %v", err)
	}
	if err := s.SaveTranscription(scopeCtx(bob), "bob-data", "en", "p", "m", 1000, 50, nil); err != nil {
		t.Fatalf("SaveTranscription bob: %v", err)
	}

	result, err := s.DeleteScope(ctx, alice)
	if err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}
	if result.RowsDeleted < 2 {
		t.Errorf("want at least 2 rows deleted (1 transcription + 1 quick note), got %d", result.RowsDeleted)
	}

	// Alice's data must be gone.
	aliceExport, err := s.ExportScope(ctx, alice)
	if err != nil {
		t.Fatalf("ExportScope alice after delete: %v", err)
	}
	if len(aliceExport.Transcriptions) != 0 {
		t.Errorf("alice transcriptions should be gone: %d remain", len(aliceExport.Transcriptions))
	}
	if len(aliceExport.QuickNotes) != 0 {
		t.Errorf("alice quick notes should be gone: %d remain", len(aliceExport.QuickNotes))
	}
	if len(aliceExport.RecordingSessions) != 0 {
		t.Errorf("alice recording sessions should be gone: %d remain", len(aliceExport.RecordingSessions))
	}

	// Bob's data must remain.
	bobExport, err := s.ExportScope(ctx, bob)
	if err != nil {
		t.Fatalf("ExportScope bob after delete: %v", err)
	}
	if len(bobExport.Transcriptions) != 1 {
		t.Errorf("bob transcriptions should remain: want 1, got %d", len(bobExport.Transcriptions))
	}
	if bobExport.Transcriptions[0].Text != "bob-data" {
		t.Errorf("bob transcription text: want %q, got %q", "bob-data", bobExport.Transcriptions[0].Text)
	}
}

func TestDeleteScopeOnEmptyScopeReturnsZero(t *testing.T) {
	s := newSQLiteStoreForPrivacyTest(t)
	ctx := context.Background()

	scope := speechstorage.Scope{InstallID: "empty-install", UserID: "nobody"}
	result, err := s.DeleteScope(ctx, scope)
	if err != nil {
		t.Fatalf("DeleteScope on empty scope: %v", err)
	}
	if result.RowsDeleted != 0 {
		t.Errorf("want 0 rows deleted on empty scope, got %d", result.RowsDeleted)
	}
	if len(result.AudioFilePaths) != 0 {
		t.Errorf("want 0 audio paths on empty scope, got %d", len(result.AudioFilePaths))
	}
}

func TestExportScopeAudioPathsCollected(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audio_test.db")
	audioDir := t.TempDir()
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath:        dbPath,
		SaveAudio:         true,
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	// Override the audio dir to our test temp dir so the file is written there.
	// SaveTranscription writes to filepath.Join(filepath.Dir(dbPath), "audio").
	// We inject a fake audio path directly via SQL to avoid needing the actual
	// audio write path.
	ctx := context.Background()
	scope := speechstorage.Scope{InstallID: "audio-test-install"}
	scopeID, err := ensureSQLiteScopeID(ctx, s.db, scope)
	if err != nil {
		t.Fatalf("ensureSQLiteScopeID: %v", err)
	}

	fakePath := filepath.Join(audioDir, "test_audio.wav")
	// Insert a transcription row.
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO transcriptions (scope_id, text, language, provider, model, duration_ms, latency_ms, word_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID, "audio-test", "en", "p", "m", 1000, 50, 1)
	if err != nil {
		t.Fatalf("insert transcription: %v", err)
	}
	tID, _ := res.LastInsertId()

	// Insert audio_assets row for this scope with the fake path.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audio_assets (scope_id, owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID, "transcription", tID, string(AudioStorageLocalFile), fakePath, "audio/wav", 100, 1000,
	); err != nil {
		t.Fatalf("insert audio_assets: %v", err)
	}

	export, err := s.ExportScope(ctx, scope)
	if err != nil {
		t.Fatalf("ExportScope: %v", err)
	}
	if len(export.AudioAssetPaths) != 1 {
		t.Errorf("AudioAssetPaths: want 1, got %d", len(export.AudioAssetPaths))
	}
	if export.AudioAssetPaths[0] != fakePath {
		t.Errorf("AudioAssetPaths[0]: want %q, got %q", fakePath, export.AudioAssetPaths[0])
	}
}

func TestDeleteScopeReturnsAudioFilePaths(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audio_delete_test.db")
	audioDir := t.TempDir()
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath:        dbPath,
		SaveAudio:         true,
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	scope := speechstorage.Scope{InstallID: "delete-audio-install", UserID: "carol"}
	otherScope := speechstorage.Scope{InstallID: "delete-audio-install", UserID: "dave"}

	scopeID, err := ensureSQLiteScopeID(ctx, s.db, scope)
	if err != nil {
		t.Fatalf("ensureSQLiteScopeID: %v", err)
	}
	otherScopeID, err := ensureSQLiteScopeID(ctx, s.db, otherScope)
	if err != nil {
		t.Fatalf("ensureSQLiteScopeID other: %v", err)
	}

	carolPath := filepath.Join(audioDir, "carol_audio.wav")
	davePath := filepath.Join(audioDir, "dave_audio.wav")

	// Insert a transcription + audio_asset for carol.
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO transcriptions (scope_id, text, language, provider, model, duration_ms, latency_ms, word_count)
		 VALUES (?, 'carol-data', 'en', 'p', 'm', 1000, 50, 1)`, scopeID)
	if err != nil {
		t.Fatalf("insert transcription carol: %v", err)
	}
	tID, _ := res.LastInsertId()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audio_assets (scope_id, owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
		 VALUES (?, 'transcription', ?, 'local-file', ?, 'audio/wav', 100, 1000)`,
		scopeID, tID, carolPath,
	); err != nil {
		t.Fatalf("insert audio_assets carol: %v", err)
	}

	// Insert a transcription + audio_asset for dave (must NOT appear in carol's DeleteResult).
	res2, err := s.db.ExecContext(ctx,
		`INSERT INTO transcriptions (scope_id, text, language, provider, model, duration_ms, latency_ms, word_count)
		 VALUES (?, 'dave-data', 'en', 'p', 'm', 1000, 50, 1)`, otherScopeID)
	if err != nil {
		t.Fatalf("insert transcription dave: %v", err)
	}
	tID2, _ := res2.LastInsertId()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audio_assets (scope_id, owner_kind, owner_id, storage_kind, path, mime_type, size_bytes, duration_ms)
		 VALUES (?, 'transcription', ?, 'local-file', ?, 'audio/wav', 100, 1000)`,
		otherScopeID, tID2, davePath,
	); err != nil {
		t.Fatalf("insert audio_assets dave: %v", err)
	}

	result, err := s.DeleteScope(ctx, scope)
	if err != nil {
		t.Fatalf("DeleteScope: %v", err)
	}

	if len(result.AudioFilePaths) != 1 {
		t.Errorf("AudioFilePaths: want 1, got %d: %v", len(result.AudioFilePaths), result.AudioFilePaths)
	} else if result.AudioFilePaths[0] != carolPath {
		t.Errorf("AudioFilePaths[0]: want %q, got %q", carolPath, result.AudioFilePaths[0])
	}
	if result.RowsDeleted < 1 {
		t.Errorf("RowsDeleted: want >= 1, got %d", result.RowsDeleted)
	}

	// dave's audio_asset must still be in the DB.
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audio_assets WHERE path = ?`, davePath,
	).Scan(&count); err != nil {
		t.Fatalf("check dave audio_asset: %v", err)
	}
	if count != 1 {
		t.Errorf("dave audio_asset should still exist, got count=%d", count)
	}
}
