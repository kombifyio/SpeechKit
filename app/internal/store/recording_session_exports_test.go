package store

import (
	"context"
	"testing"
	"time"
)

// Export orchestration depends on: one record per meeting and target that a
// later push overwrites, and no record outliving its meeting.
func TestContractRecordingSessionExportsKeepOneRecordPerTarget(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		sessions, ok := s.(RecordingSessionStore)
		exports, ok2 := s.(RecordingSessionExportStore)
		if !ok || !ok2 {
			t.Skip("backend does not persist recording session exports")
		}
		newSession := func() int64 {
			id, err := sessions.SaveRecordingSession(ctx, RecordingSession{
				Kind: RecordingSessionKindMeeting, Status: RecordingSessionStatusFinished, StartedAt: time.Now().UTC(),
			})
			if err != nil {
				t.Fatalf("SaveRecordingSession: %v", err)
			}
			return id
		}
		meeting, other := newSession(), newSession()

		if _, err := exports.UpsertRecordingSessionExport(ctx, RecordingSessionExport{
			SessionID: meeting, TargetID: "onenote", DestinationID: "section-1", ExternalID: "page-1",
			Status: RecordingSessionExportFailed, LastErrorKind: "throttled",
		}); err != nil {
			t.Fatalf("Upsert failed push: %v", err)
		}
		pushedAt := time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)
		synced, err := exports.UpsertRecordingSessionExport(ctx, RecordingSessionExport{
			SessionID: meeting, TargetID: "onenote", DestinationID: "section-2", ExternalID: "page-1",
			URL: "https://onenote.example/page-1", Fingerprint: "sha256:ab", Status: RecordingSessionExportSynced,
			PushedAt: pushedAt,
		})
		if err != nil {
			t.Fatalf("Upsert synced push: %v", err)
		}
		if _, err := exports.UpsertRecordingSessionExport(ctx, RecordingSessionExport{SessionID: other, TargetID: "onenote"}); err != nil {
			t.Fatal(err)
		}

		listed, err := exports.ListRecordingSessionExports(ctx, meeting)
		if err != nil || len(listed) != 1 {
			t.Fatalf("list = %+v, %v; want the one onenote record", listed, err)
		}
		got := listed[0]
		if got.ID != synced.ID || got.Status != RecordingSessionExportSynced || got.LastErrorKind != "" ||
			got.DestinationID != "section-2" || got.Fingerprint != "sha256:ab" || !got.PushedAt.Equal(pushedAt) {
			t.Fatalf("record = %+v, want the synced push", got)
		}

		if err := sessions.DeleteRecordingSession(ctx, meeting); err != nil {
			t.Fatal(err)
		}
		if _, err := exports.ListRecordingSessionExports(ctx, meeting); err == nil {
			t.Error("the deleted meeting still answers export records")
		}
		if rest, _ := exports.ListRecordingSessionExports(ctx, other); len(rest) != 1 {
			t.Errorf("deleting one meeting touched another's export: %+v", rest)
		}
	})
}
