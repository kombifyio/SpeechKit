package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kombifyio/SpeechKit/internal/runtimepath"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
	_ "modernc.org/sqlite" // register modernc sqlite as database/sql driver
)

//go:embed migrations/sqlite/001_init.sql
var sqliteMigration001 string

//go:embed migrations/sqlite/005_user_dictionary.sql
var sqliteMigration005 string

//go:embed migrations/sqlite/006_voice_agent_sessions.sql
var sqliteMigration006 string

//go:embed migrations/sqlite/007_personas.sql
var sqliteMigration007 string

//go:embed migrations/sqlite/010_voice_agent_normalized.sql
var sqliteMigration010 string

//go:embed migrations/sqlite/011_audio_assets.sql
var sqliteMigration011 string

//go:embed migrations/sqlite/012_indexes.sql
var sqliteMigration012 string

//go:embed migrations/sqlite/013_storage_scopes.sql
var sqliteMigration013 string

//go:embed migrations/sqlite/014_storage_v3_model.sql
var sqliteMigration014 string

//go:embed migrations/sqlite/015_wakeword_activations.sql
var sqliteMigration015 string

//go:embed migrations/sqlite/017_customization.sql
var sqliteMigration017 string

//go:embed migrations/sqlite/018_recording_sessions.sql
var sqliteMigration018 string

//go:embed migrations/sqlite/021_recording_session_notes.sql
var sqliteMigration021 string

//go:embed migrations/sqlite/022_recording_session_enhancements.sql
var sqliteMigration022 string

// SQLiteStore implements Store using a local SQLite database via
// modernc.org/sqlite (pure Go, no CGo required). All query logic lives in the
// embedded *sqlStore; this type only owns connection setup and migrations.
type SQLiteStore struct {
	*sqlStore
}

// Compile-time interface check.
var _ Store = (*SQLiteStore)(nil)

// NewSQLiteStore opens or creates a SQLite feedback database.
func NewSQLiteStore(cfg StoreConfig) (*SQLiteStore, error) {
	dbPath := cfg.SQLitePath
	if dbPath == "" {
		dbPath = filepath.Join(runtimepath.DataDir(), "feedback.db")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sqlOpenAndMigrateSQLite(dbPath)
	if err != nil {
		return nil, err
	}

	store := &SQLiteStore{sqlStore: &sqlStore{
		db:                      db,
		dialect:                 dialectSQLite,
		audioDir:                filepath.Join(filepath.Dir(dbPath), "audio"),
		maxStorageMB:            cfg.MaxAudioStorageMB,
		saveAudio:               cfg.SaveAudio,
		audioRetentionDays:      cfg.AudioRetentionDays,
		meetingRetentionDays:    cfg.MeetingRetentionDays,
		transcriptionModelHints: normalizeTranscriptionModelHints(cfg.TranscriptionModelHints),
		defaultScope:            speechstorage.NormalizeScope(cfg.DefaultScope),
		scopePolicy:             normalizedScopePolicy(cfg.ScopePolicy),
	}}
	if store.saveAudio && store.audioRetentionDays > 0 {
		store.enforceAudioRetention()
	}
	if store.saveAudio && store.maxStorageMB > 0 {
		store.enforceStorageLimit()
	}
	return store, nil
}

func sqlOpenAndMigrateSQLite(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if err := runSQLiteMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
