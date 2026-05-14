package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

func normalizedScopePolicy(policy speechstorage.ScopePolicy) speechstorage.ScopePolicy {
	if policy == "" {
		return speechstorage.ScopeOptional
	}
	return policy
}

func effectiveStoreScope(ctx context.Context, defaultScope speechstorage.Scope, policy speechstorage.ScopePolicy) (speechstorage.Scope, error) {
	scope := speechstorage.ScopeFromContext(ctx)
	if scope.Key() == speechstorage.NormalizeScope(speechstorage.Scope{}).Key() && defaultScope.Key() != "" {
		scope = speechstorage.NormalizeScope(defaultScope)
	}
	policy = normalizedScopePolicy(policy)
	if err := policy.Validate(scope); err != nil {
		return speechstorage.Scope{}, err
	}
	return speechstorage.NormalizeScope(scope), nil
}

func ensureSQLiteScopeID(ctx context.Context, db *sql.DB, scope speechstorage.Scope) (int64, error) {
	scope = speechstorage.NormalizeScope(scope)
	labelsJSON, err := marshalScopeLabels(scope)
	if err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO storage_scopes
		(scope_key, install_id, device_id, user_id, tenant_id, labels_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		scope.Key(), scope.InstallID, scope.DeviceID, scope.UserID, scope.TenantID, labelsJSON,
	); err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM storage_scopes WHERE scope_key = ?`, scope.Key()).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO store_stats (scope_id) VALUES (?)`, id); err != nil {
		return 0, err
	}
	return id, nil
}

func ensurePostgresScopeID(ctx context.Context, db *sql.DB, scope speechstorage.Scope) (int64, error) {
	scope = speechstorage.NormalizeScope(scope)
	labelsJSON, err := marshalScopeLabels(scope)
	if err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRowContext(ctx, `INSERT INTO storage_scopes
		(scope_key, install_id, device_id, user_id, tenant_id, labels_json)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT(scope_key) DO UPDATE SET scope_key = EXCLUDED.scope_key
		RETURNING id`,
		scope.Key(), scope.InstallID, scope.DeviceID, scope.UserID, scope.TenantID, labelsJSON,
	).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO store_stats (scope_id) VALUES ($1) ON CONFLICT(scope_id) DO NOTHING`, id); err != nil {
		return 0, err
	}
	return id, nil
}

func marshalScopeLabels(scope speechstorage.Scope) (string, error) {
	if len(scope.Labels) == 0 {
		return "{}", nil
	}
	raw, err := json.Marshal(scope.Labels)
	if err != nil {
		return "", fmt.Errorf("marshal scope labels: %w", err)
	}
	return string(raw), nil
}

func (s *SQLiteStore) scopeID(ctx context.Context) (int64, error) {
	scope, err := effectiveStoreScope(ctx, s.defaultScope, s.scopePolicy)
	if err != nil {
		return 0, err
	}
	return ensureSQLiteScopeID(ctx, s.db, scope)
}

func (s *PostgresStore) scopeID(ctx context.Context) (int64, error) {
	scope, err := effectiveStoreScope(ctx, s.defaultScope, s.scopePolicy)
	if err != nil {
		return 0, err
	}
	return ensurePostgresScopeID(ctx, s.db, scope)
}
