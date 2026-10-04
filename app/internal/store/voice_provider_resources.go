package store

import (
	"context"
	"database/sql"
)

// VoiceProviderResource journals native Voice Agent provision intent before
// provider I/O. It contains opaque correlation/cleanup identifiers, no audio,
// transcripts, owner credentials or callback tokens.
type VoiceProviderResource struct {
	Name, VendorID string
	ExpiresAt      int64
	Released       bool
}

// VoiceProviderResourceStore extends the existing durable session database.
// A resource remains pending until the provider acknowledges deletion.
type VoiceProviderResourceStore interface {
	SaveVoiceProviderResource(context.Context, VoiceProviderResource) error
	ListVoiceProviderResources(context.Context) ([]VoiceProviderResource, error)
	DeleteVoiceProviderResource(context.Context, string) error
}

func runVoiceProviderResourcesMigration(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS voice_agent_provider_resources (
		name TEXT PRIMARY KEY, vendor_id TEXT NOT NULL DEFAULT '', expires_at BIGINT NOT NULL, released INTEGER NOT NULL DEFAULT 0)`)
	return err
}

func (s *sqlStore) SaveVoiceProviderResource(ctx context.Context, resource VoiceProviderResource) error {
	released := 0
	if resource.Released {
		released = 1
	}
	_, err := s.db.ExecContext(ctx, s.dialect.rebind(`INSERT INTO voice_agent_provider_resources (name, vendor_id, expires_at, released)
		VALUES (?, ?, ?, ?) ON CONFLICT(name) DO UPDATE SET vendor_id = CASE WHEN excluded.vendor_id = '' THEN voice_agent_provider_resources.vendor_id ELSE excluded.vendor_id END, expires_at = excluded.expires_at, released = CASE WHEN voice_agent_provider_resources.released = 1 THEN 1 ELSE excluded.released END`), resource.Name, resource.VendorID, resource.ExpiresAt, released)
	return err
}

func (s *sqlStore) ListVoiceProviderResources(ctx context.Context) ([]VoiceProviderResource, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, vendor_id, expires_at, released FROM voice_agent_provider_resources ORDER BY expires_at`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var resources []VoiceProviderResource
	for rows.Next() {
		var resource VoiceProviderResource
		var released int
		if err := rows.Scan(&resource.Name, &resource.VendorID, &resource.ExpiresAt, &released); err != nil {
			return nil, err
		}
		resource.Released = released != 0
		resources = append(resources, resource)
	}
	return resources, rows.Err()
}

func (s *sqlStore) DeleteVoiceProviderResource(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.rebind(`DELETE FROM voice_agent_provider_resources WHERE name = ?`), name)
	return err
}
