package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Compile-time guarantees that both default backends implement the
// WakewordActivationStore extension. The Server-Target's
// `app.Store.(store.WakewordActivationStore)` type-assertion in
// internal/server/core/wakewordtraining_wiring.go relies on this.
var _ WakewordActivationStore = (*SQLiteStore)(nil)
var _ WakewordActivationStore = (*PostgresStore)(nil)

// ErrInvalidWakewordActivation signals that a SaveWakewordActivation
// call was passed a row missing one of the required fields (id,
// owner_user_id, owner_org_id, audio_path). Returned before any
// database touch so the REST handler can surface a clear 400.
var ErrInvalidWakewordActivation = errors.New("store: wakeword activation missing required fields (id, owner_user_id, owner_org_id, audio_path)")

// ─── SQLite ─────────────────────────────────────────────────────────────────

func (s *SQLiteStore) SaveWakewordActivation(ctx context.Context, a WakewordActivation) (*WakewordActivation, error) {
	if err := validateWakewordActivation(&a); err != nil {
		return nil, err
	}
	if a.UploadedAt.IsZero() {
		a.UploadedAt = time.Now().UTC()
	}
	if a.MetadataJSON == "" {
		a.MetadataJSON = "{}"
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO wakeword_activations (
			id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
			score, captured_at, uploaded_at, label, audio_path, audio_bytes,
			sample_rate, pre_roll_ms, post_roll_ms, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.OwnerUserID, a.OwnerOrgID, a.PhraseID, a.Phrase, a.Backend,
		a.Score, a.CapturedAt, a.UploadedAt, a.Label, a.AudioPath, a.AudioBytes,
		a.SampleRate, a.PreRollMs, a.PostRollMs, a.MetadataJSON,
	); err != nil {
		return nil, fmt.Errorf("store: insert wakeword_activation: %w", err)
	}
	return &a, nil
}

func (s *SQLiteStore) GetWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (*WakewordActivation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
			score, captured_at, uploaded_at, label, audio_path, audio_bytes,
			sample_rate, pre_roll_ms, post_roll_ms, metadata_json
		 FROM wakeword_activations
		 WHERE id = ? AND owner_user_id = ? AND owner_org_id = ?`,
		id, ownerUserID, ownerOrgID,
	)
	return scanWakewordActivation(row)
}

func (s *SQLiteStore) ListWakewordActivations(ctx context.Context, ownerUserID, ownerOrgID string, opts ListOpts) ([]WakewordActivation, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
			score, captured_at, uploaded_at, label, audio_path, audio_bytes,
			sample_rate, pre_roll_ms, post_roll_ms, metadata_json
		 FROM wakeword_activations
		 WHERE owner_user_id = ? AND owner_org_id = ?
		 ORDER BY captured_at DESC, id DESC
		 LIMIT ?`,
		ownerUserID, ownerOrgID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list wakeword_activations: %w", err)
	}
	defer rows.Close() //nolint:errcheck // close error on read-only iterator is not actionable
	return scanWakewordActivationsRows(rows)
}

func (s *SQLiteStore) UpdateWakewordActivationLabel(ctx context.Context, id, ownerUserID, ownerOrgID, label string) error {
	if !ValidWakewordLabel(label) {
		return fmt.Errorf("store: invalid wakeword label %q", label)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE wakeword_activations SET label = ?
		 WHERE id = ? AND owner_user_id = ? AND owner_org_id = ?`,
		label, id, ownerUserID, ownerOrgID,
	)
	if err != nil {
		return fmt.Errorf("store: update wakeword_activation label: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteStore) DeleteWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback is harmless after commit

	var audioPath string
	err = tx.QueryRowContext(ctx,
		`SELECT audio_path FROM wakeword_activations
		 WHERE id = ? AND owner_user_id = ? AND owner_org_id = ?`,
		id, ownerUserID, ownerOrgID,
	).Scan(&audioPath)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM wakeword_activations
		 WHERE id = ? AND owner_user_id = ? AND owner_org_id = ?`,
		id, ownerUserID, ownerOrgID,
	); err != nil {
		return "", fmt.Errorf("store: delete wakeword_activation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return audioPath, nil
}

func (s *SQLiteStore) CountWakewordActivationsForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wakeword_activations WHERE owner_user_id = ? AND owner_org_id = ?`,
		ownerUserID, ownerOrgID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *SQLiteStore) SumWakewordActivationBytesForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT SUM(audio_bytes) FROM wakeword_activations WHERE owner_user_id = ? AND owner_org_id = ?`,
		ownerUserID, ownerOrgID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}

// ─── Postgres ───────────────────────────────────────────────────────────────

func (s *PostgresStore) SaveWakewordActivation(ctx context.Context, a WakewordActivation) (*WakewordActivation, error) {
	if err := validateWakewordActivation(&a); err != nil {
		return nil, err
	}
	if a.UploadedAt.IsZero() {
		a.UploadedAt = time.Now().UTC()
	}
	if a.MetadataJSON == "" {
		a.MetadataJSON = "{}"
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO wakeword_activations (
			id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
			score, captured_at, uploaded_at, label, audio_path, audio_bytes,
			sample_rate, pre_roll_ms, post_roll_ms, metadata_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::jsonb)`,
		a.ID, a.OwnerUserID, a.OwnerOrgID, a.PhraseID, a.Phrase, a.Backend,
		a.Score, a.CapturedAt, a.UploadedAt, a.Label, a.AudioPath, a.AudioBytes,
		a.SampleRate, a.PreRollMs, a.PostRollMs, a.MetadataJSON,
	); err != nil {
		return nil, fmt.Errorf("store: insert wakeword_activation: %w", err)
	}
	return &a, nil
}

func (s *PostgresStore) GetWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (*WakewordActivation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
			score, captured_at, uploaded_at, label, audio_path, audio_bytes,
			sample_rate, pre_roll_ms, post_roll_ms, metadata_json::text
		 FROM wakeword_activations
		 WHERE id = $1 AND owner_user_id = $2 AND owner_org_id = $3`,
		id, ownerUserID, ownerOrgID,
	)
	return scanWakewordActivation(row)
}

func (s *PostgresStore) ListWakewordActivations(ctx context.Context, ownerUserID, ownerOrgID string, opts ListOpts) ([]WakewordActivation, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
			score, captured_at, uploaded_at, label, audio_path, audio_bytes,
			sample_rate, pre_roll_ms, post_roll_ms, metadata_json::text
		 FROM wakeword_activations
		 WHERE owner_user_id = $1 AND owner_org_id = $2
		 ORDER BY captured_at DESC, id DESC
		 LIMIT $3`,
		ownerUserID, ownerOrgID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list wakeword_activations: %w", err)
	}
	defer rows.Close() //nolint:errcheck // close error on read-only iterator is not actionable
	return scanWakewordActivationsRows(rows)
}

func (s *PostgresStore) UpdateWakewordActivationLabel(ctx context.Context, id, ownerUserID, ownerOrgID, label string) error {
	if !ValidWakewordLabel(label) {
		return fmt.Errorf("store: invalid wakeword label %q", label)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE wakeword_activations SET label = $4
		 WHERE id = $1 AND owner_user_id = $2 AND owner_org_id = $3`,
		id, ownerUserID, ownerOrgID, label,
	)
	if err != nil {
		return fmt.Errorf("store: update wakeword_activation label: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PostgresStore) DeleteWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (string, error) {
	var audioPath string
	err := s.db.QueryRowContext(ctx,
		`DELETE FROM wakeword_activations
		 WHERE id = $1 AND owner_user_id = $2 AND owner_org_id = $3
		 RETURNING audio_path`,
		id, ownerUserID, ownerOrgID,
	).Scan(&audioPath)
	if err != nil {
		return "", err
	}
	return audioPath, nil
}

func (s *PostgresStore) CountWakewordActivationsForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM wakeword_activations WHERE owner_user_id = $1 AND owner_org_id = $2`,
		ownerUserID, ownerOrgID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *PostgresStore) SumWakewordActivationBytesForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT SUM(audio_bytes) FROM wakeword_activations WHERE owner_user_id = $1 AND owner_org_id = $2`,
		ownerUserID, ownerOrgID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	if !n.Valid {
		return 0, nil
	}
	return n.Int64, nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

// validateWakewordActivation checks that the required fields are
// populated before the row reaches the database. Returns
// ErrInvalidWakewordActivation when any of id / owner_user_id /
// owner_org_id / audio_path are blank.
func validateWakewordActivation(a *WakewordActivation) error {
	if a == nil {
		return ErrInvalidWakewordActivation
	}
	if strings.TrimSpace(a.ID) == "" ||
		strings.TrimSpace(a.OwnerUserID) == "" ||
		strings.TrimSpace(a.OwnerOrgID) == "" ||
		strings.TrimSpace(a.AudioPath) == "" {
		return ErrInvalidWakewordActivation
	}
	if a.SampleRate == 0 {
		a.SampleRate = 16000
	}
	if !ValidWakewordLabel(a.Label) {
		// Tolerate unknown labels by clearing them — labels are
		// metadata, not key fields, so the row is still useful
		// without one.
		a.Label = WakewordLabelUnknown
	}
	return nil
}

// wakewordActivationRow is the abstract row interface satisfied by
// both *sql.Row and *sql.Rows. Lets scanWakewordActivation work for
// both Get (one row) and List (many rows) paths.
type wakewordActivationRow interface {
	Scan(dest ...any) error
}

func scanWakewordActivation(row wakewordActivationRow) (*WakewordActivation, error) {
	var a WakewordActivation
	if err := row.Scan(
		&a.ID, &a.OwnerUserID, &a.OwnerOrgID, &a.PhraseID, &a.Phrase, &a.Backend,
		&a.Score, &a.CapturedAt, &a.UploadedAt, &a.Label, &a.AudioPath, &a.AudioBytes,
		&a.SampleRate, &a.PreRollMs, &a.PostRollMs, &a.MetadataJSON,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

func scanWakewordActivationsRows(rows *sql.Rows) ([]WakewordActivation, error) {
	var out []WakewordActivation
	for rows.Next() {
		a, err := scanWakewordActivation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}
