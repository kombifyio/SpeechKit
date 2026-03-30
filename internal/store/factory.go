package store

import "fmt"

// StoreConfig holds configuration for store backend selection.
type StoreConfig struct {
	Backend                 string `toml:"backend"` // "sqlite" | "postgres" | registered name
	SQLitePath              string `toml:"sqlite_path"`
	PostgresDSN             string `toml:"postgres_dsn"`
	SaveAudio               bool   `toml:"save_audio"`
	AudioRetentionDays      int    `toml:"audio_retention_days"`
	MaxAudioStorageMB       int    `toml:"max_audio_storage_mb"`
	TranscriptionModelHints map[string]string
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
