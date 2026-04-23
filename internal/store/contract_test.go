package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Compile-time interface compliance guards. If a backend's method signatures
// drift from the shared interfaces declared in types.go, this file fails
// to compile — surfacing the regression at `go build` time rather than at
// runtime when a user switches the `backend` config option.
var (
	_ Store                      = (*SQLiteStore)(nil)
	_ UserDictionaryStore        = (*SQLiteStore)(nil)
	_ VoiceAgentSessionStore     = (*SQLiteStore)(nil)
	_ SemanticCapabilityProvider = (*SQLiteStore)(nil)

	_ Store                      = (*PostgresStore)(nil)
	_ UserDictionaryStore        = (*PostgresStore)(nil)
	_ VoiceAgentSessionStore     = (*PostgresStore)(nil)
	_ SemanticCapabilityProvider = (*PostgresStore)(nil)
)

// backendFixture describes a concrete Store backend under contract test.
// Each entry provides a factory plus a `prepare` hook that lets the backend
// reset persistent state (e.g. TRUNCATE for postgres) between test runs.
type backendFixture struct {
	name    string
	newFn   func(t *testing.T) (Store, func())
	skipMsg string // non-empty means skip via t.Skip
}

// contractBackends returns the set of backends to run contract tests against.
// - SQLite is always included (in-memory per test, via t.TempDir()).
// - Postgres is included only when SPEECHKIT_POSTGRES_TEST_DSN is set.
func contractBackends() []backendFixture {
	fixtures := []backendFixture{
		{
			name: "sqlite",
			newFn: func(t *testing.T) (Store, func()) {
				t.Helper()
				dbPath := filepath.Join(t.TempDir(), "contract.db")
				s, err := New(StoreConfig{
					Backend:           "sqlite",
					SQLitePath:        dbPath,
					SaveAudio:         true,
					MaxAudioStorageMB: 100,
				})
				if err != nil {
					t.Fatalf("sqlite: New: %v", err)
				}
				return s, func() { _ = s.Close() }
			},
		},
	}

	dsn := strings.TrimSpace(os.Getenv("SPEECHKIT_POSTGRES_TEST_DSN"))
	if dsn != "" {
		fixtures = append(fixtures, backendFixture{
			name: "postgres",
			newFn: func(t *testing.T) (Store, func()) {
				t.Helper()
				t.Setenv("APPDATA", t.TempDir())
				s, err := New(StoreConfig{
					Backend:           "postgres",
					PostgresDSN:       dsn,
					SaveAudio:         true,
					MaxAudioStorageMB: 100,
				})
				if err != nil {
					t.Fatalf("postgres: New: %v", err)
				}
				pg, ok := s.(*PostgresStore)
				if !ok {
					t.Fatalf("postgres: got %T, want *PostgresStore", s)
				}
				if _, err := pg.db.Exec(`TRUNCATE TABLE quick_notes, transcriptions, voice_agent_sessions, user_dictionary RESTART IDENTITY`); err != nil {
					t.Fatalf("postgres: truncate: %v", err)
				}
				return s, func() { _ = s.Close() }
			},
		})
	} else {
		fixtures = append(fixtures, backendFixture{
			name:    "postgres",
			skipMsg: "set SPEECHKIT_POSTGRES_TEST_DSN to run postgres contract",
		})
	}
	return fixtures
}

// eachBackend runs `fn` once per backend fixture as a subtest, skipping
// backends that require environment setup not present in this run.
func eachBackend(t *testing.T, fn func(t *testing.T, s Store)) {
	t.Helper()
	for _, fx := range contractBackends() {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			if fx.skipMsg != "" {
				t.Skip(fx.skipMsg)
			}
			s, cleanup := fx.newFn(t)
			defer cleanup()
			fn(t, s)
		})
	}
}

// TestContractTranscriptionRoundTrip asserts that a SaveTranscription followed
// by GetTranscription and ListTranscriptions returns the same text and
// metadata across every backend.
func TestContractTranscriptionRoundTrip(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if err := s.SaveTranscription(ctx, "contract text", "de", "hf", "openai/whisper-large-v3", 2400, 450, nil); err != nil {
			t.Fatalf("SaveTranscription: %v", err)
		}
		count, err := s.TranscriptionCount(ctx)
		if err != nil {
			t.Fatalf("TranscriptionCount: %v", err)
		}
		if count != 1 {
			t.Errorf("TranscriptionCount = %d, want 1", count)
		}
		list, err := s.ListTranscriptions(ctx, ListOpts{Limit: 10})
		if err != nil {
			t.Fatalf("ListTranscriptions: %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("len(List) = %d, want 1", len(list))
		}
		if list[0].Text != "contract text" {
			t.Errorf("Text = %q, want %q", list[0].Text, "contract text")
		}
		if list[0].Language != "de" {
			t.Errorf("Language = %q, want %q", list[0].Language, "de")
		}
		got, err := s.GetTranscription(ctx, list[0].ID)
		if err != nil {
			t.Fatalf("GetTranscription: %v", err)
		}
		if got.Text != list[0].Text {
			t.Errorf("GetTranscription.Text = %q, want %q", got.Text, list[0].Text)
		}
	})
}

// TestContractQuickNoteLifecycle walks the full quick-note CRUD surface on
// every backend. Drift in any backend (e.g. a typo in an UPDATE returning
// RowsAffected vs row-count check) will surface here.
func TestContractQuickNoteLifecycle(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		id, err := s.SaveQuickNote(ctx, "initial", "en", "manual", 900, 120, nil)
		if err != nil {
			t.Fatalf("SaveQuickNote: %v", err)
		}
		if err := s.UpdateQuickNote(ctx, id, "updated"); err != nil {
			t.Fatalf("UpdateQuickNote: %v", err)
		}
		got, err := s.GetQuickNote(ctx, id)
		if err != nil {
			t.Fatalf("GetQuickNote: %v", err)
		}
		if got.Text != "updated" {
			t.Errorf("Text after update = %q, want %q", got.Text, "updated")
		}
		if err := s.PinQuickNote(ctx, id, true); err != nil {
			t.Fatalf("PinQuickNote: %v", err)
		}
		got, err = s.GetQuickNote(ctx, id)
		if err != nil {
			t.Fatalf("GetQuickNote (after pin): %v", err)
		}
		if !got.Pinned {
			t.Error("Pinned = false, want true after PinQuickNote(true)")
		}
		if err := s.DeleteQuickNote(ctx, id); err != nil {
			t.Fatalf("DeleteQuickNote: %v", err)
		}
		if _, err := s.GetQuickNote(ctx, id); err == nil {
			t.Error("GetQuickNote after delete succeeded, want error")
		}
	})
}

// TestContractStatsReflectsWrites ensures Stats() observes writes consistently
// across backends. Important because the Stats SQL is the most divergent
// area between sqlite.go and postgres.go (different aggregate syntax).
func TestContractStatsReflectsWrites(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		before, err := s.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats(initial): %v", err)
		}
		if before.Transcriptions != 0 || before.QuickNotes != 0 {
			t.Errorf("initial Stats non-zero: %+v", before)
		}
		if err := s.SaveTranscription(ctx, "stats one", "de", "p", "m", 1000, 100, nil); err != nil {
			t.Fatalf("SaveTranscription: %v", err)
		}
		if _, err := s.SaveQuickNote(ctx, "note one", "de", "manual", 500, 50, nil); err != nil {
			t.Fatalf("SaveQuickNote: %v", err)
		}
		after, err := s.Stats(ctx)
		if err != nil {
			t.Fatalf("Stats(after): %v", err)
		}
		if after.Transcriptions != 1 {
			t.Errorf("Stats.Transcriptions = %d, want 1", after.Transcriptions)
		}
		if after.QuickNotes != 1 {
			t.Errorf("Stats.QuickNotes = %d, want 1", after.QuickNotes)
		}
		if after.TotalWords < 2 {
			t.Errorf("Stats.TotalWords = %d, want at least 2", after.TotalWords)
		}
	})
}

// TestContractListPaginationOrdering verifies the ListOpts.Limit + descending
// chronological order contract holds in both backends.
func TestContractListPaginationOrdering(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for i, text := range []string{"first", "second", "third"} {
			if err := s.SaveTranscription(ctx, text, "en", "p", "m", int64(i*100), 50, nil); err != nil {
				t.Fatalf("SaveTranscription(%s): %v", text, err)
			}
			// Small sleep so CreatedAt differs between rows on systems with
			// coarse clock resolution. Both backends order by CreatedAt DESC.
			time.Sleep(2 * time.Millisecond)
		}
		list, err := s.ListTranscriptions(ctx, ListOpts{Limit: 2})
		if err != nil {
			t.Fatalf("ListTranscriptions: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("Limit=2 returned %d rows", len(list))
		}
		if list[0].Text != "third" {
			t.Errorf("[0].Text = %q, want %q (most-recent first)", list[0].Text, "third")
		}
		if list[1].Text != "second" {
			t.Errorf("[1].Text = %q, want %q", list[1].Text, "second")
		}
	})
}

// TestContractSemanticCapabilitiesReported asserts that every backend exposes
// SemanticCapabilities without panicking, even if the capability set is empty.
func TestContractSemanticCapabilitiesReported(t *testing.T) {
	eachBackend(t, func(t *testing.T, s Store) {
		provider, ok := s.(SemanticCapabilityProvider)
		if !ok {
			t.Skip("backend does not implement SemanticCapabilityProvider")
		}
		caps := provider.SemanticCapabilities(context.Background())
		if string(caps.Provider) == "" {
			t.Error("SemanticCapabilities.Provider must not be empty (use SemanticProviderNone for 'no support')")
		}
	})
}

// TestContractCloseIdempotent checks that calling Close twice does not panic
// or return a surprising error on any backend.
func TestContractCloseIdempotent(t *testing.T) {
	for _, fx := range contractBackends() {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			if fx.skipMsg != "" {
				t.Skip(fx.skipMsg)
			}
			s, _ := fx.newFn(t)
			if err := s.Close(); err != nil {
				t.Fatalf("first Close: %v", err)
			}
			// Second Close must not panic. Error is acceptable — backends may
			// report "already closed", but they must not crash the process.
			_ = s.Close()
		})
	}
}
