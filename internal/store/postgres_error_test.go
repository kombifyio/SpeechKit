package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

var registerFailingPostgresDriver sync.Once

const failingPostgresDriverName = "speechkit-failing-postgres"

var errFailingPostgres = errors.New("postgres unavailable")

type failingPostgresDriver struct{}

func (failingPostgresDriver) Open(string) (driver.Conn, error) {
	return failingPostgresConn{}, nil
}

type failingPostgresConn struct{}

func (failingPostgresConn) Prepare(string) (driver.Stmt, error) { return nil, errFailingPostgres }
func (failingPostgresConn) Close() error                        { return nil }
func (failingPostgresConn) Begin() (driver.Tx, error)           { return nil, errFailingPostgres }

func (failingPostgresConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return nil, errFailingPostgres
}

func (failingPostgresConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errFailingPostgres
}

func (failingPostgresConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, errFailingPostgres
}

func openFailingPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	registerFailingPostgresDriver.Do(func() {
		sql.Register(failingPostgresDriverName, failingPostgresDriver{})
	})
	db, err := sql.Open(failingPostgresDriverName, "")
	if err != nil {
		t.Fatalf("open failing postgres db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close failing postgres db: %v", err)
		}
	})
	return db
}

func TestPostgresStoreMethodsReturnDatabaseErrors(t *testing.T) {
	ctx := context.Background()
	store := &PostgresStore{
		db:       openFailingPostgresDB(t),
		audioDir: t.TempDir(),
		transcriptionModelHints: map[string]string{
			"hf":          "hf-model",
			"huggingface": "hf-model",
		},
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"SaveTranscription", func() error {
			return store.SaveTranscription(ctx, "hello world", "en", "hf", "", 0, 0, nil)
		}},
		{"SaveTranscriptionWithAudio", func() error {
			return store.SaveTranscriptionWithAudio(ctx, "hello world", "en", "hf", "", 0, 0, AudioAssetInput{})
		}},
		{"GetTranscription", func() error {
			_, err := store.GetTranscription(ctx, 1)
			return err
		}},
		{"ListTranscriptions", func() error {
			_, err := store.ListTranscriptions(ctx, ListOpts{Language: "en", Limit: 3, After: time.Now().Add(-time.Hour)})
			return err
		}},
		{"ReplaceUserDictionaryEntries", func() error {
			return store.ReplaceUserDictionaryEntries(ctx, "en", []UserDictionaryEntry{{Spoken: "speech kit", Canonical: "SpeechKit"}})
		}},
		{"ListUserDictionaryEntries", func() error {
			_, err := store.ListUserDictionaryEntries(ctx, "en")
			return err
		}},
		{"RecordUserDictionaryUsage", func() error {
			return store.RecordUserDictionaryUsage(ctx, "SpeechKit", "en")
		}},
		{"TranscriptionCount", func() error {
			_, err := store.TranscriptionCount(ctx)
			return err
		}},
		{"SaveQuickNote", func() error {
			_, err := store.SaveQuickNote(ctx, "quick note", "en", "hf", 0, 0, nil)
			return err
		}},
		{"GetQuickNote", func() error {
			_, err := store.GetQuickNote(ctx, 1)
			return err
		}},
		{"ListQuickNotes", func() error {
			_, err := store.ListQuickNotes(ctx, ListOpts{Language: "en", Limit: 3, After: time.Now().Add(-time.Hour)})
			return err
		}},
		{"UpdateQuickNote", func() error {
			return store.UpdateQuickNote(ctx, 1, "updated")
		}},
		{"UpdateQuickNoteCapture", func() error {
			return store.UpdateQuickNoteCapture(ctx, 1, "updated", "hf", 0, 0, nil)
		}},
		{"PinQuickNote", func() error {
			return store.PinQuickNote(ctx, 1, true)
		}},
		{"DeleteQuickNote", func() error {
			return store.DeleteQuickNote(ctx, 1)
		}},
		{"QuickNoteCount", func() error {
			_, err := store.QuickNoteCount(ctx)
			return err
		}},
		{"Stats", func() error {
			_, err := store.Stats(ctx)
			return err
		}},
		{"SaveVoiceAgentSession", func() error {
			_, err := store.SaveVoiceAgentSession(ctx, VoiceAgentSession{Transcript: "hello"})
			return err
		}},
		{"ListVoiceAgentSessions", func() error {
			_, err := store.ListVoiceAgentSessions(ctx, ListOpts{Language: "en", Limit: 3, After: time.Now().Add(-time.Hour)})
			return err
		}},
		{"GetVoiceAgentSession", func() error {
			_, err := store.GetVoiceAgentSession(ctx, 1)
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, errFailingPostgres) {
				t.Fatalf("error = %v, want %v", err, errFailingPostgres)
			}
		})
	}
}

func TestPostgresStorePureHelpers(t *testing.T) {
	store := &PostgresStore{
		db:                 openFailingPostgresDB(t),
		audioDir:           t.TempDir(),
		maxStorageMB:       10,
		saveAudio:          true,
		audioRetentionDays: 1,
		transcriptionModelHints: map[string]string{
			"hf":          "hf-model",
			"huggingface": "huggingface-model",
		},
	}

	if store.DB() == nil {
		t.Fatal("DB returned nil")
	}
	if caps := store.SemanticCapabilities(context.Background()); caps.Provider != SemanticProviderNone || caps.FullText || caps.Embeddings || caps.VectorSearch {
		t.Fatalf("SemanticCapabilities = %+v", caps)
	}
	if got := store.transcriptionModelHint("HuggingFace"); got != "huggingface-model" {
		t.Fatalf("transcriptionModelHint = %q", got)
	}
	if got := store.transcriptionModelHint("unknown"); got != "" {
		t.Fatalf("unknown transcriptionModelHint = %q", got)
	}
	path, err := store.persistAudio(AudioAssetInput{Data: []byte("audio"), Extension: ".wav"}, "pg_")
	if err != nil {
		t.Fatalf("persistAudio: %v", err)
	}
	if !strings.Contains(path, "pg_") || !strings.HasSuffix(path, ".wav") {
		t.Fatalf("audio path = %q", path)
	}

	store.enforceStorageLimit()
	store.enforceAudioRetention()
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := defaultAudioDir(); got == "" {
		t.Fatal("defaultAudioDir returned empty path")
	}
}

func TestPostgresHelperCoverageTargets(t *testing.T) {
	for mimeType, want := range map[string]string{
		"audio/mpeg":        ".mp3",
		"audio/mp3":         ".mp3",
		"audio/ogg":         ".ogg",
		"audio/opus":        ".opus",
		"audio/webm":        ".webm",
		"audio/L16;rate=16": ".pcm",
		"audio/pcm":         ".pcm",
		"audio/wav":         ".wav",
	} {
		if got := extensionForMimeType(mimeType); got != want {
			t.Fatalf("extensionForMimeType(%q) = %q, want %q", mimeType, got, want)
		}
	}

	clauses, args := appendPostgresOwnerFilter(nil, nil, ListOpts{
		OwnerUserID:      " user ",
		OwnerOrgID:       " org ",
		IncludeOwnerless: true,
	})
	if len(clauses) != 1 || len(args) != 2 {
		t.Fatalf("owner filter = (%v, %v)", clauses, args)
	}
	if !strings.Contains(clauses[0], "$1") || !strings.Contains(clauses[0], "$2") {
		t.Fatalf("owner clause missing positional params: %s", clauses[0])
	}

	migration := postgresSQLMigration("999_test", "SELECT 1")
	if migration.version != "999_test" {
		t.Fatalf("migration version = %q", migration.version)
	}
	if err := migration.run(context.Background(), openFailingPostgresDB(t)); !errors.Is(err, errFailingPostgres) {
		t.Fatalf("postgres migration error = %v, want %v", err, errFailingPostgres)
	}
}
