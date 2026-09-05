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

//go:embed migrations/sqlite/024_customization_word_identity.sql
var sqliteMigration024 string

//go:embed migrations/sqlite/026_meeting_summary_batches.sql
var sqliteMigration026 string

//go:embed migrations/sqlite/027_recording_session_snapshots.sql
var sqliteMigration027 string

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
		snapshotDir:             filepath.Join(filepath.Dir(dbPath), "snapshots"),
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
	// foreign_keys belongs in the DSN, not in a PRAGMA statement. SQLite scopes
	// the setting to a connection and database/sql keeps a pool, so a PRAGMA
	// executed once reached whichever connection served it and every later
	// connection ran with SQLite's default of OFF. Deleting a recording session
	// over such a connection left its segments, notes, write-ups, digests and
	// snapshot rows behind instead of cascading — including after a subject
	// erasure request.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := verifySQLiteForeignKeys(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := runSQLiteMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// verifySQLiteForeignKeys fails startup when the driver did not apply the DSN
// pragma. Cascading deletes are how meeting children and subject erasure stay
// complete, so a silent downgrade to SQLite's OFF default must not be shipped.
func verifySQLiteForeignKeys(ctx context.Context, db *sql.DB) error {
	var enabled int
	if err := db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil {
		return fmt.Errorf("read sqlite foreign_keys pragma: %w", err)
	}
	if enabled != 1 {
		return fmt.Errorf("sqlite foreign_keys = %d, want 1", enabled)
	}
	return nil
}
