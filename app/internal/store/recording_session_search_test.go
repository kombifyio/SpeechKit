package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSearchTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewSQLiteStore(StoreConfig{SQLitePath: filepath.Join(t.TempDir(), "search.db")})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSearchRecordingSessionsMatchesTitleTranscriptNotesAndWriteUps(t *testing.T) {
	s := newSearchTestStore(t)
	ctx := context.Background()
	started := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)

	budget, _ := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "Budget review Q4", StartedAt: started})
	launch, _ := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "Weekly sync", StartedAt: started.Add(time.Hour)})
	if _, err := s.AppendRecordingSessionSegment(ctx, launch, RecordingSessionSegment{IsFinal: true, Text: "Anna sagte, der Launch rutscht in den Oktober, weil das Zertifikat fehlt."}); err != nil {
		t.Fatalf("AppendRecordingSessionSegment: %v", err)
	}
	notes, _ := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "1:1", StartedAt: started.Add(2 * time.Hour)})
	if err := s.SaveRecordingSessionNotes(ctx, notes, RecordingSessionNotes{ContentMD: "- ask legal about the Zertifikat"}); err != nil {
		t.Fatalf("SaveRecordingSessionNotes: %v", err)
	}
	writeUp, _ := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "Retro", StartedAt: started.Add(3 * time.Hour)})
	enhancementID, _ := s.CreateRecordingSessionEnhancement(ctx, writeUp, RecordingSessionEnhancement{SessionID: writeUp, TemplateSlug: "default", Status: RecordingSessionEnhancementRunning})
	rows, _ := s.ListRecordingSessionEnhancements(ctx, writeUp)
	row := rows[0]
	row.Status = RecordingSessionEnhancementReady
	row.ContentMD = "## Decisions\n\n- Zertifikat bestellen"
	if err := s.UpdateRecordingSessionEnhancement(ctx, enhancementID, row); err != nil {
		t.Fatalf("UpdateRecordingSessionEnhancement: %v", err)
	}
	// A dictation with the word is not a meeting and must not appear.
	if _, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindDictation, Title: "Zertifikat dictation"}); err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}

	hits, err := s.SearchRecordingSessions(ctx, "zertifikat", ListOpts{Kind: "meeting"})
	if err != nil {
		t.Fatalf("SearchRecordingSessions: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("hits = %d (%+v), want the transcript, notes and write-up matches", len(hits), hits)
	}
	sources := map[int64]string{}
	for _, hit := range hits {
		sources[hit.Session.ID] = hit.Source
		if !strings.Contains(strings.ToLower(hit.Snippet), "zertifikat") {
			t.Fatalf("snippet %q lacks the term", hit.Snippet)
		}
	}
	if sources[launch] != "transcript" || sources[notes] != "notes" || sources[writeUp] != "write-up" {
		t.Fatalf("sources = %v", sources)
	}

	titleHits, _ := s.SearchRecordingSessions(ctx, "budget", ListOpts{Kind: "meeting"})
	if len(titleHits) != 1 || titleHits[0].Session.ID != budget || titleHits[0].Source != "title" {
		t.Fatalf("title search = %+v", titleHits)
	}

	// Every term must match: "launch oktober" finds the sync, "launch budget" nothing.
	if both, _ := s.SearchRecordingSessions(ctx, "launch oktober", ListOpts{Kind: "meeting"}); len(both) != 1 || both[0].Session.ID != launch {
		t.Fatalf("two-term search = %+v", both)
	}
	if none, _ := s.SearchRecordingSessions(ctx, "launch budget", ListOpts{Kind: "meeting"}); len(none) != 0 {
		t.Fatalf("terms from different meetings matched: %+v", none)
	}
	if empty, _ := s.SearchRecordingSessions(ctx, " a ", ListOpts{Kind: "meeting"}); len(empty) != 0 {
		t.Fatalf("a one-letter query must not match everything: %+v", empty)
	}
}

func TestSearchRecordingSessionsEscapesLikeWildcards(t *testing.T) {
	s := newSearchTestStore(t)
	ctx := context.Background()
	if _, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "100% coverage"}); err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	if _, err := s.SaveRecordingSession(ctx, RecordingSession{Kind: RecordingSessionKindMeeting, Title: "1000 covers"}); err != nil {
		t.Fatalf("SaveRecordingSession: %v", err)
	}
	hits, err := s.SearchRecordingSessions(ctx, "100%", ListOpts{Kind: "meeting"})
	if err != nil {
		t.Fatalf("SearchRecordingSessions: %v", err)
	}
	if len(hits) != 1 || hits[0].Session.Title != "100% coverage" {
		t.Fatalf("wildcard search = %+v, want the literal percent match only", hits)
	}
}

func TestExcerptAroundCentresTheMatch(t *testing.T) {
	long := strings.Repeat("früher ", 30) + "hier steht das Zertifikat im Text " + strings.Repeat("später ", 30)
	excerpt := excerptAround(long, "zertifikat")
	if !strings.HasPrefix(excerpt, "…") || !strings.HasSuffix(excerpt, "…") || !strings.Contains(excerpt, "Zertifikat") {
		t.Fatalf("excerpt = %q", excerpt)
	}
	if n := len([]rune(excerpt)); n > 160 {
		t.Fatalf("excerpt is %d runes, want a short window", n)
	}
	if short := excerptAround("Budget review", "budget"); short != "Budget review" {
		t.Fatalf("short text excerpt = %q", short)
	}
}
