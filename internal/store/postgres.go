package store

import "fmt"

// NewPostgresStore creates a PostgreSQL-backed store.
// Requires a valid DSN in StoreConfig.PostgresDSN.
// Implementation will use pgx/v5 -- currently returns an error until Phase 2.
func NewPostgresStore(cfg StoreConfig) (Store, error) {
	if cfg.PostgresDSN == "" {
		return nil, fmt.Errorf("postgres backend requires a DSN (set store.postgres_dsn in config.toml)")
	}
	return nil, fmt.Errorf("postgres backend not yet implemented — use sqlite or register a custom backend")
}
