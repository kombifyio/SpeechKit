package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// ephemeralMeeting is a finished meeting with every kind of material the
// retention rules distinguish: transcript, digest, imported Teams transcript,
// Copilot recap, screen capture, the user's notes and a finished write-up.
type ephemeralMeeting struct {
	id           int64
	snapshotPath string
}

func seedMeeting(t *testing.T, s *SQLiteStore, pinned bool) ephemeralMeeting {
	t.Helper()
	ctx := context.Background()
	id, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "Planning"})
	if err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	segmentID, err := s.AppendRecordingSessionSegment(ctx, id, RecordingSessionSegment{SegmentIndex: -1, IsFinal: true, Text: "we ship on Friday"})
	if err != nil {
		t.Fatalf("AppendRecordingSessionSegment: %v", err)
	}
	if _, err := s.UpsertMeetingSummaryBatch(ctx, MeetingSummaryBatch{
		SessionID: id, BatchKey: fmt.Sprintf("batch-%d", id), StartSegmentID: segmentID, EndSegmentID: segmentID,
		SourceFingerprint: "fp", Status: MeetingSummaryBatchReady, DigestJSON: `{"points":["ship"]}`,
	}); err != nil {
		t.Fatalf("UpsertMeetingSummaryBatch: %v", err)
	}
	for _, item := range []RecordingSessionImport{
		{SessionID: id, Kind: RecordingSessionImportTeamsTranscript, Status: RecordingSessionImportImported},
		{SessionID: id, Kind: RecordingSessionImportCopilotRecap, Status: RecordingSessionImportImported, ContentJSON: `{"notes":["recap"]}`},
	} {
		if _, err := s.UpsertRecordingSessionImport(ctx, item); err != nil {
			t.Fatalf("UpsertRecordingSessionImport: %v", err)
		}
	}
	snapshot, err := s.SaveRecordingSessionSnapshot(ctx, id, RecordingSessionSnapshotInput{Data: []byte("PNG")})
	if err != nil {
		t.Fatalf("SaveRecordingSessionSnapshot: %v", err)
	}
	if err := s.SaveRecordingSessionNotes(ctx, id, RecordingSessionNotes{ContentMD: "- ask about Q3"}); err != nil {
		t.Fatalf("SaveRecordingSessionNotes: %v", err)
	}
	if _, err := s.CreateRecordingSessionEnhancement(ctx, id, RecordingSessionEnhancement{
		Status: RecordingSessionEnhancementReady, ContentMD: "# Planning",
	}); err != nil {
		t.Fatalf("CreateRecordingSessionEnhancement: %v", err)
	}
	if pinned {
		if err := s.SetRecordingSessionPinned(ctx, id, true); err != nil {
			t.Fatalf("SetRecordingSessionPinned: %v", err)
		}
	}
	if err := s.FinishRecordingSession(ctx, id, "", time.Now()); err != nil {
		t.Fatalf("FinishRecordingSession: %v", err)
	}
	return ephemeralMeeting{id: id, snapshotPath: snapshot.Path}
}

// meetingMaterial is what of a meeting is still stored.
type meetingMaterial struct {
	session, transcript, digest, teamsImport, recap, snapshotRow, snapshotFile, notes, review bool
}

func materialOf(t *testing.T, s *SQLiteStore, m ephemeralMeeting) meetingMaterial {
	t.Helper()
	ctx := context.Background()
	var got meetingMaterial
	_, statErr := os.Stat(m.snapshotPath)
	got.snapshotFile = statErr == nil
	session, err := s.GetRecordingSession(ctx, m.id)
	if errors.Is(err, sql.ErrNoRows) {
		return got
	}
	if err != nil {
		t.Fatalf("GetRecordingSession: %v", err)
	}
	got.session = true
	got.transcript = len(session.Segments) > 0
	batches, err := s.ListMeetingSummaryBatches(ctx, m.id)
	if err != nil {
		t.Fatalf("ListMeetingSummaryBatches: %v", err)
	}
	got.digest = len(batches) > 0
	imports, err := s.ListRecordingSessionImports(ctx, m.id)
	if err != nil {
		t.Fatalf("ListRecordingSessionImports: %v", err)
	}
	for _, item := range imports {
		got.teamsImport = got.teamsImport || item.Kind == RecordingSessionImportTeamsTranscript
		got.recap = got.recap || (item.Kind == RecordingSessionImportCopilotRecap && item.ContentJSON != "")
	}
	snapshots, err := s.ListRecordingSessionSnapshots(ctx, m.id)
	if err != nil {
		t.Fatalf("ListRecordingSessionSnapshots: %v", err)
	}
	got.snapshotRow = len(snapshots) > 0
	notes, err := s.GetRecordingSessionNotes(ctx, m.id)
	got.notes = err == nil && notes != nil && notes.ContentMD != ""
	reviews, err := s.ListRecordingSessionEnhancements(ctx, m.id)
	if err != nil {
		t.Fatalf("ListRecordingSessionEnhancements: %v", err)
	}
	got.review = len(reviews) > 0
	return got
}

func openStoreAt(t *testing.T, dbPath string, cfg StoreConfig) *SQLiteStore {
	t.Helper()
	cfg.SQLitePath = dbPath
	return openRetentionTestStore(t, cfg)
}

func backdateMeeting(t *testing.T, s *SQLiteStore, id int64, ended string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE recording_sessions SET ended_at = ?, updated_at = ? WHERE id = ?`, ended, ended, id); err != nil {
		t.Fatalf("backdate meeting: %v", err)
	}
}

// When a meeting ends under the ephemeral scope its raw material goes — the
// transcript, the digests, the imported Teams transcript and the screen
// captures, file included — and what the user keeps stays: the session, the
// notes, the write-up and the Copilot recap.
func TestEphemeralMeetingEndSweepsRawMaterialAndKeepsNotesAndReviews(t *testing.T) {
	s := openStoreAt(t, filepath.Join(t.TempDir(), "feedback.db"), StoreConfig{RetentionScope: framework.RetentionScopeEphemeral})
	meeting := seedMeeting(t, s, false)

	sweep, err := s.SweepEndedRecordingSession(context.Background(), meeting.id)
	if err != nil {
		t.Fatalf("SweepEndedRecordingSession: %v", err)
	}
	if sweep.Empty() || sweep.Reason != RetentionSweepReasonScope {
		t.Fatalf("sweep report = %+v, want a scope sweep with counts", sweep)
	}
	got := materialOf(t, s, meeting)
	if got.transcript || got.digest || got.teamsImport || got.snapshotRow || got.snapshotFile {
		t.Errorf("raw material survived the sweep: %+v", got)
	}
	if !got.session || !got.notes || !got.review || !got.recap {
		t.Errorf("the sweep removed what the user keeps: %+v", got)
	}
}

// The write-up reads the transcript and a Teams import still appends to it,
// so neither a running meeting nor pending work is swept; the completion of
// the last piece of work is.
func TestEphemeralSweepWaitsForTheWriteUpAndTheImport(t *testing.T) {
	ctx := context.Background()
	s := openStoreAt(t, filepath.Join(t.TempDir(), "feedback.db"), StoreConfig{RetentionScope: framework.RetentionScopeEphemeral})

	active, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SweepEndedRecordingSession(ctx, active); !errors.Is(err, ErrRetentionSweepDeferred) {
		t.Fatalf("sweeping a running meeting: err = %v, want ErrRetentionSweepDeferred", err)
	}

	meeting := seedMeeting(t, s, false)
	writeUp, err := s.CreateRecordingSessionEnhancement(ctx, meeting.id, RecordingSessionEnhancement{Status: RecordingSessionEnhancementPending})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertRecordingSessionImport(ctx, RecordingSessionImport{
		SessionID: meeting.id, Kind: RecordingSessionImportTeamsTranscript, Status: RecordingSessionImportWaiting,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SweepEndedRecordingSession(ctx, meeting.id); !errors.Is(err, ErrRetentionSweepDeferred) {
		t.Fatalf("sweep with a write-up and an import pending: err = %v, want ErrRetentionSweepDeferred", err)
	}
	if err := s.UpdateRecordingSessionEnhancement(ctx, writeUp, RecordingSessionEnhancement{Status: RecordingSessionEnhancementReady}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SweepEndedRecordingSession(ctx, meeting.id); !errors.Is(err, ErrRetentionSweepDeferred) {
		t.Fatalf("sweep with the import still waiting: err = %v, want ErrRetentionSweepDeferred", err)
	}
	if got := materialOf(t, s, meeting); !got.transcript {
		t.Fatal("a deferred sweep removed the transcript")
	}
	if _, err := s.UpsertRecordingSessionImport(ctx, RecordingSessionImport{
		SessionID: meeting.id, Kind: RecordingSessionImportTeamsTranscript, Status: RecordingSessionImportImported,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SweepEndedRecordingSession(ctx, meeting.id); err != nil {
		t.Fatalf("sweep after the work completed: %v", err)
	}
	if got := materialOf(t, s, meeting); got.transcript {
		t.Fatal("the transcript survived once all work on the meeting was over")
	}
}

// A meeting that ended before a crash or a quit never reached its sweep; the
// store catches up when it opens.
func TestEphemeralStartupSweepsMeetingsThatEndedBeforeACrash(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedback.db")
	writer := openStoreAt(t, dbPath, StoreConfig{RetentionScope: framework.RetentionScopeEphemeral})
	meeting := seedMeeting(t, writer, false)
	backdateMeeting(t, writer, meeting.id, time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05"))
	_ = writer.Close()

	reopened := openStoreAt(t, dbPath, StoreConfig{RetentionScope: framework.RetentionScopeEphemeral})
	got := materialOf(t, reopened, meeting)
	if got.transcript || got.snapshotFile {
		t.Errorf("raw material of an ended meeting survived the startup sweep: %+v", got)
	}
	if !got.notes || !got.review {
		t.Errorf("the startup sweep removed what the user keeps: %+v", got)
	}
}

// Under the retain scope the scope sweeps nothing, neither on demand nor when
// the store opens.
func TestRetainScopeSweepsNothing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "feedback.db")
	writer := openStoreAt(t, dbPath, StoreConfig{})
	meeting := seedMeeting(t, writer, false)
	backdateMeeting(t, writer, meeting.id, "2000-01-01 00:00:00")
	if _, err := writer.SweepEndedRecordingSession(context.Background(), meeting.id); err != nil {
		t.Fatalf("SweepEndedRecordingSession: %v", err)
	}
	_ = writer.Close()

	reopened := openStoreAt(t, dbPath, StoreConfig{RetentionScope: framework.RetentionScopeRetain, AudioRetentionDays: 1})
	want := meetingMaterial{session: true, transcript: true, digest: true, teamsImport: true, recap: true, snapshotRow: true, snapshotFile: true, notes: true, review: true}
	if got := materialOf(t, reopened, meeting); got != want {
		t.Fatalf("the retain scope removed material: %+v", got)
	}
}

// The TTL is a hard bound that pinning does not lift. Under retain it removes
// the raw material of old meetings and keeps their notes and write-ups; under
// ephemeral the old meeting and old dictation history go entirely. Material
// inside the bound stays either way.
func TestRetentionTTLRemovesOldMaterialIncludingPinned(t *testing.T) {
	for _, tc := range []struct {
		scope     framework.RetentionScope
		wantOld   meetingMaterial
		keepsDict bool
	}{
		{framework.RetentionScopeRetain, meetingMaterial{session: true, recap: true, notes: true, review: true}, true},
		{framework.RetentionScopeEphemeral, meetingMaterial{}, false},
	} {
		t.Run(string(tc.scope), func(t *testing.T) {
			ctx := context.Background()
			dbPath := filepath.Join(t.TempDir(), "feedback.db")
			writer := openStoreAt(t, dbPath, StoreConfig{})
			old := seedMeeting(t, writer, true)
			fresh := seedMeeting(t, writer, true)
			backdateMeeting(t, writer, old.id, "2000-01-01 00:00:00")
			if err := writer.SaveTranscription(ctx, "old dictation", "en", "local", "", 500, 10, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := writer.db.Exec(`UPDATE transcriptions SET created_at = '2000-01-01 00:00:00', pinned = 1`); err != nil {
				t.Fatal(err)
			}
			_ = writer.Close()

			reopened := openStoreAt(t, dbPath, StoreConfig{RetentionScope: tc.scope, RetentionTTLDays: 30})
			if got := materialOf(t, reopened, old); got != tc.wantOld {
				t.Errorf("pinned meeting past the TTL: got %+v, want %+v", got, tc.wantOld)
			}
			if got := materialOf(t, reopened, fresh); !got.transcript || !got.notes || !got.review {
				t.Errorf("a meeting inside the TTL lost material: %+v", got)
			}
			rows, err := reopened.ListTranscriptions(ctx, ListOpts{Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			if kept := len(rows) > 0; kept != tc.keepsDict {
				t.Errorf("pinned dictation history past the TTL kept = %v, want %v", kept, tc.keepsDict)
			}
		})
	}
}

// Under the ephemeral scope audio is never written to disk, even with
// save_audio on.
func TestEphemeralScopeWritesNoAudio(t *testing.T) {
	s := openStoreAt(t, filepath.Join(t.TempDir(), "feedback.db"), StoreConfig{SaveAudio: true, RetentionScope: framework.RetentionScopeEphemeral})
	if err := s.SaveTranscription(context.Background(), "dictated", "en", "local", "", 500, 10, []byte("RIFFdata")); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}
	if paths := savedAudioPaths(t, s); len(paths) > 0 {
		t.Fatalf("audio was stored under the ephemeral scope: %v", paths)
	}
}
