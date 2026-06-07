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
// WakewordActivationStore extension (via the embedded *sqlStore). The
// Server-Target's `app.Store.(store.WakewordActivationStore)` type-assertion in
// internal/server/core/wakewordtraining_wiring.go relies on this.
var _ WakewordActivationStore = (*SQLiteStore)(nil)
var _ WakewordActivationStore = (*PostgresStore)(nil)

// ErrInvalidWakewordActivation signals that a SaveWakewordActivation
// call was passed a row missing one of the required fields (id,
// owner_user_id, owner_org_id, audio_path). Returned before any
// database touch so the REST handler can surface a clear 400.
var ErrInvalidWakewordActivation = errors.New("store: wakeword activation missing required fields (id, owner_user_id, owner_org_id, audio_path)")

func (s *sqlStore) SaveWakewordActivation(ctx context.Context, a WakewordActivation) (*WakewordActivation, error) {
	if err := validateWakewordActivation(&a); err != nil {
		return nil, err
	}
	if a.UploadedAt.IsZero() {
		a.UploadedAt = time.Now().UTC()
	}
	if a.MetadataJSON == "" {
		a.MetadataJSON = "{}"
	}
	if _, err := s.db.ExecContext(ctx, s.dialect.rebind(
		`INSERT INTO wakeword_activations (
			id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
			score, captured_at, uploaded_at, label, audio_path, audio_bytes,
			sample_rate, pre_roll_ms, post_roll_ms, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?`+s.dialect.jsonbInsert()+`)`),
		a.ID, a.OwnerUserID, a.OwnerOrgID, a.PhraseID, a.Phrase, a.Backend,
		a.Score, a.CapturedAt, a.UploadedAt, a.Label, a.AudioPath, a.AudioBytes,
		a.SampleRate, a.PreRollMs, a.PostRollMs, a.MetadataJSON,
	); err != nil {
		return nil, fmt.Errorf("store: insert wakeword_activation: %w", err)
	}
	return &a, nil
}

func (s *sqlStore) wakewordSelectColumns() string {
	return `id, owner_user_id, owner_org_id, phrase_id, phrase, backend,
		score, captured_at, uploaded_at, label, audio_path, audio_bytes,
		sample_rate, pre_roll_ms, post_roll_ms, ` + s.dialect.jsonbText("metadata_json")
}

func (s *sqlStore) GetWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (*WakewordActivation, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT `+s.wakewordSelectColumns()+`
		 FROM wakeword_activations
		 WHERE id = ? AND owner_user_id = ? AND owner_org_id = ?`),
		id, ownerUserID, ownerOrgID,
	)
	return scanWakewordActivation(row)
}

func (s *sqlStore) ListWakewordActivations(ctx context.Context, ownerUserID, ownerOrgID string, opts ListOpts) ([]WakewordActivation, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT `+s.wakewordSelectColumns()+`
		 FROM wakeword_activations
		 WHERE owner_user_id = ? AND owner_org_id = ?
		 ORDER BY captured_at DESC, id DESC
		 LIMIT ?`),
		ownerUserID, ownerOrgID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list wakeword_activations: %w", err)
	}
	defer rows.Close() //nolint:errcheck // close error on read-only iterator is not actionable
	return scanWakewordActivationsRows(rows)
}

func (s *sqlStore) UpdateWakewordActivationLabel(ctx context.Context, id, ownerUserID, ownerOrgID, label string) error {
	if !ValidWakewordLabel(label) {
		return fmt.Errorf("store: invalid wakeword label %q", label)
	}
	res, err := s.db.ExecContext(ctx, s.dialect.rebind(
		`UPDATE wakeword_activations SET label = ?
		 WHERE id = ? AND owner_user_id = ? AND owner_org_id = ?`),
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

// DeleteWakewordActivation removes the row and returns its audio_path via a
// single DELETE ... RETURNING — supported by PostgreSQL and SQLite 3.35+
// (modernc.org/sqlite), so both backends share one statement.
func (s *sqlStore) DeleteWakewordActivation(ctx context.Context, id, ownerUserID, ownerOrgID string) (string, error) {
	var audioPath string
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`DELETE FROM wakeword_activations
		 WHERE id = ? AND owner_user_id = ? AND owner_org_id = ?
		 RETURNING audio_path`),
		id, ownerUserID, ownerOrgID,
	).Scan(&audioPath)
	if err != nil {
		return "", err
	}
	return audioPath, nil
}

func (s *sqlStore) CountWakewordActivationsForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT COUNT(*) FROM wakeword_activations WHERE owner_user_id = ? AND owner_org_id = ?`),
		ownerUserID, ownerOrgID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *sqlStore) SumWakewordActivationBytesForUser(ctx context.Context, ownerUserID, ownerOrgID string) (int64, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, s.dialect.rebind(
		`SELECT SUM(audio_bytes) FROM wakeword_activations WHERE owner_user_id = ? AND owner_org_id = ?`),
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
