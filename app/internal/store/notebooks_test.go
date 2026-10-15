package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func oneNoteNotebook(name, section string) Notebook {
	return Notebook{DisplayName: name, Destination: NotebookDestination{
		Kind: NotebookDestinationOneNote, NotebookID: "nb-1", SectionID: section, DisplayPath: "Work / " + name,
	}}
}

// The notebook index answers what was stored, renamed or relinked, and
// refuses a notebook it could never sync to.
func TestContractNotebookIndexKeepsRenamesAndRelinks(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		notebooks, ok := s.(NotebookStore)
		if !ok {
			t.Skip("backend does not keep a notebook index")
		}
		if _, err := notebooks.CreateNotebook(ctx, Notebook{DisplayName: "No destination"}); !errors.Is(err, ErrNotebookInvalid) {
			t.Fatalf("create without a destination: err = %v, want ErrNotebookInvalid", err)
		}
		created, err := notebooks.CreateNotebook(ctx, oneNoteNotebook(" Team sync ", "sec-1"))
		if err != nil {
			t.Fatalf("CreateNotebook: %v", err)
		}
		relinked := oneNoteNotebook("Weekly sync", "sec-2")
		relinked.ID = created.ID
		if _, err := notebooks.UpdateNotebook(ctx, relinked); err != nil {
			t.Fatalf("UpdateNotebook: %v", err)
		}
		syncedAt := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
		if err := notebooks.MarkNotebookSynced(ctx, created.ID, syncedAt); err != nil {
			t.Fatalf("MarkNotebookSynced: %v", err)
		}

		listed, err := notebooks.ListNotebooks(ctx)
		if err != nil || len(listed) != 1 {
			t.Fatalf("ListNotebooks = %+v, %v; want the one notebook", listed, err)
		}
		got := listed[0]
		if got.DisplayName != "Weekly sync" || got.Destination.SectionID != "sec-2" ||
			got.Destination.Kind != NotebookDestinationOneNote || !got.LastSyncedAt.Equal(syncedAt) {
			t.Fatalf("notebook = %+v, want the renamed, relinked and synced entry", got)
		}
	})
}

// A notebook is an index entry, not the owner of its meetings: deleting it
// keeps every session and only drops their reference.
func TestContractDeletingANotebookKeepsItsSessions(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		notebooks, ok := s.(NotebookStore)
		sessions, ok2 := s.(RecordingSessionStore)
		if !ok || !ok2 {
			t.Skip("backend does not keep a notebook index")
		}
		notebook, err := notebooks.CreateNotebook(ctx, oneNoteNotebook("Team sync", "sec-1"))
		if err != nil {
			t.Fatal(err)
		}
		created, err := sessions.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, NotebookID: notebook.ID})
		if err != nil {
			t.Fatalf("SaveRecordingSession with a notebook: %v", err)
		}
		linked, err := sessions.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting})
		if err != nil {
			t.Fatal(err)
		}
		if err := notebooks.SetRecordingSessionNotebook(ctx, linked, notebook.ID); err != nil {
			t.Fatalf("SetRecordingSessionNotebook: %v", err)
		}

		if err := notebooks.DeleteNotebook(ctx, notebook.ID); err != nil {
			t.Fatalf("DeleteNotebook: %v", err)
		}
		if _, err := notebooks.GetNotebook(ctx, notebook.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("deleted notebook: err = %v, want sql.ErrNoRows", err)
		}
		for _, id := range []int64{created, linked} {
			session, err := sessions.GetRecordingSession(ctx, id)
			if err != nil {
				t.Fatalf("session %d after the notebook was deleted: %v", id, err)
			}
			if session.NotebookID != 0 {
				t.Errorf("session %d still references the deleted notebook %d", id, session.NotebookID)
			}
		}
	})
}

// The ephemeral scope sweeps a meeting's raw material, never the notebook
// index or a session's place in it.
func TestEphemeralSweepKeepsTheNotebookIndex(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "feedback.db")
	writer := openStoreAt(t, dbPath, StoreConfig{RetentionScope: framework.RetentionScopeEphemeral})
	notebook, err := writer.CreateNotebook(ctx, oneNoteNotebook("Team sync", "sec-1"))
	if err != nil {
		t.Fatal(err)
	}
	swept := seedMeeting(t, writer, false)
	stale := seedMeeting(t, writer, false)
	for _, id := range []int64{swept.id, stale.id} {
		if err := writer.SetRecordingSessionNotebook(ctx, id, notebook.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := writer.SweepEndedRecordingSession(ctx, swept.id); err != nil {
		t.Fatalf("SweepEndedRecordingSession: %v", err)
	}
	backdateMeeting(t, writer, stale.id, time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05"))
	_ = writer.Close()

	reopened := openStoreAt(t, dbPath, StoreConfig{RetentionScope: framework.RetentionScopeEphemeral})
	if got := materialOf(t, reopened, stale); got.transcript {
		t.Fatal("the maintenance sweep did not run")
	}
	listed, err := reopened.ListNotebooks(ctx)
	if err != nil || len(listed) != 1 || listed[0].ID != notebook.ID {
		t.Fatalf("notebook index after the sweeps = %+v, %v; want the notebook kept", listed, err)
	}
	for _, id := range []int64{swept.id, stale.id} {
		session, err := reopened.GetRecordingSession(ctx, id)
		if err != nil || session.NotebookID != notebook.ID {
			t.Fatalf("swept session %d = %+v, %v; want it kept in its notebook", id, session, err)
		}
	}
}
