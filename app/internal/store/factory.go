package store

import (
	"fmt"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// StoreConfig holds configuration for store backend selection.
type StoreConfig struct {
	Backend     string `toml:"backend"` // "sqlite" | "postgres" | registered name
	SQLitePath  string `toml:"sqlite_path"`
	PostgresDSN string `toml:"postgres_dsn"`
	// PostgresRuntimeRole is the runtime-role mode of the Postgres backend:
	// the server connects as a runtime role, never migrates, refuses an older
	// schema and refuses to start unless the role cannot escape row-level
	// security (assertPostgresRuntimeRole). Off, a self-hosted server migrates
	// its own database at start.
	PostgresRuntimeRole bool `toml:"postgres_runtime_role"`
	SaveAudio           bool `toml:"save_audio"`
	AudioRetentionDays  int  `toml:"audio_retention_days"`
	// MeetingRetentionDays discards finished meetings older than this. Zero
	// keeps them forever, which is the default: a meeting is work, not a
	// by-product, so nothing is thrown away unless the user asks for it.
	MeetingRetentionDays int `toml:"meeting_retention_days"`
	// TranscriptRetentionDays discards dictation and Assist transcripts,
	// Voice Agent conversations and dictation recording sessions older than
	// this, with their audio. Zero keeps them forever. Pinned transcripts and
	// pinned sessions are kept either way.
	TranscriptRetentionDays int `toml:"transcript_retention_days"`
	MaxAudioStorageMB       int `toml:"max_audio_storage_mb"`
	TranscriptionModelHints map[string]string
	DefaultScope            speechstorage.Scope
	ScopePolicy             speechstorage.ScopePolicy
	// RetentionScope is the [privacy] retention scope. Under ephemeral the
	// store writes no audio and sweeps a session's raw material once its work
	// is over (see retention_sweep.go). Empty means retain.
	RetentionScope framework.RetentionScope `toml:"-"`
	// RetentionTTLDays is the [privacy] retention TTL: a hard upper bound by
	// age that pinning does not lift. Zero means no TTL.
	RetentionTTLDays int `toml:"-"`
	// OnRetentionSweep receives the counts of every sweep the retention scope
	// or the TTL performs, for the audit log. Optional; it may run on a
	// background goroutine and during store construction.
	OnRetentionSweep func(RetentionSweep) `toml:"-"`
}

// BackendFactory creates a Store from config.
type BackendFactory func(cfg StoreConfig) (Store, error)

var registeredBackends = map[string]BackendFactory{}

// RegisterBackend allows external modules (e.g. kombify) to register custom backends.
// Called from init() in private modules -- SpeechKit itself never knows about kombify.
func RegisterBackend(name string, factory BackendFactory) {
	registeredBackends[name] = factory
}

// New creates a Store backend based on the config.
func New(cfg StoreConfig) (Store, error) {
	if cfg.Backend == "" {
		cfg.Backend = "sqlite"
	}

	switch cfg.Backend {
	case "sqlite":
		return NewSQLiteStore(cfg)
	case "postgres":
		return NewPostgresStore(cfg)
	default:
		if factory, ok := registeredBackends[cfg.Backend]; ok {
			return factory(cfg)
		}
		available := []string{"sqlite", "postgres"}
		for name := range registeredBackends {
			available = append(available, name)
		}
		return nil, fmt.Errorf("unknown store backend %q (available: %v)", cfg.Backend, available)
	}
}
