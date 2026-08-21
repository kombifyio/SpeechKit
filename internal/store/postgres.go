package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx as database/sql driver
	"github.com/kombifyio/SpeechKit/internal/runtimepath"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

//go:embed migrations/postgres/001_init.sql
var postgresMigration001 string

//go:embed migrations/postgres/002_voice_agent_sessions.sql
var postgresMigration002 string

//go:embed migrations/postgres/003_personas.sql
var postgresMigration003 string

//go:embed migrations/postgres/004_word_counts.sql
var postgresMigration004 string

//go:embed migrations/postgres/005_voice_agent_normalized.sql
var postgresMigration005 string

//go:embed migrations/postgres/006_audio_assets.sql
var postgresMigration006 string

//go:embed migrations/postgres/007_indexes.sql
var postgresMigration007 string

//go:embed migrations/postgres/008_storage_scopes.sql
var postgresMigration008 string

//go:embed migrations/postgres/009_storage_v3_model.sql
var postgresMigration009 string

//go:embed migrations/postgres/010_wakeword_activations.sql
var postgresMigration010 string

//go:embed migrations/postgres/011_storage_scope_sequence.sql
var postgresMigration011 string

//go:embed migrations/postgres/012_transcription_speakers.sql
var postgresMigration012 string

//go:embed migrations/postgres/013_customization.sql
var postgresMigration013 string

//go:embed migrations/postgres/014_recording_sessions.sql
var postgresMigration014 string

//go:embed migrations/postgres/015_recording_session_state.sql
var postgresMigration015 string

//go:embed migrations/postgres/016_recording_session_channels.sql
var postgresMigration016 string

//go:embed migrations/postgres/017_recording_session_notes.sql
var postgresMigration017 string

//go:embed migrations/postgres/018_recording_session_enhancements.sql
var postgresMigration018 string

//go:embed migrations/postgres/019_meeting_retention.sql
var postgresMigration019 string

// PostgresStore implements Store using PostgreSQL for metadata and the local
// filesystem for optional raw WAV persistence. All query logic lives in the
// embedded *sqlStore; this type only owns connection setup and migrations.
type PostgresStore struct {
	*sqlStore
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore creates a PostgreSQL-backed store.
func NewPostgresStore(cfg StoreConfig) (*PostgresStore, error) {
	if strings.TrimSpace(cfg.PostgresDSN) == "" {
		return nil, fmt.Errorf("postgres backend requires a DSN (set store.postgres_dsn in config.toml)")
	}

	db, err := sql.Open("pgx", cfg.PostgresDSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := runPostgresMigrations(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}

	store := &PostgresStore{sqlStore: &sqlStore{
		db:                      db,
		dialect:                 dialectPostgres,
		audioDir:                defaultAudioDir(),
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

func defaultAudioDir() string {
	return filepath.Join(runtimepath.DataDir(), "audio")
}
