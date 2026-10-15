package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// notebooks.go keeps the device's notebook index: one row per notebook, each
// linked to one destination (a OneNote section for now). Recording sessions
// point at a row through recording_sessions.notebook_id. The index is the
// user's own organisation of their meetings, like a setting: no retention
// sweep touches it, and deleting a row keeps the sessions that referenced it.

var _ NotebookStore = (*SQLiteStore)(nil)
var _ NotebookStore = (*PostgresStore)(nil)

const notebookSelectSQL = `SELECT id, display_name, destination_kind, notebook_id, section_id,
		site_id, display_path, last_synced_at, created_at, updated_at
	FROM notebooks`

// CreateNotebook implements NotebookStore.
func (s *sqlStore) CreateNotebook(ctx context.Context, notebook Notebook) (Notebook, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return Notebook{}, err
	}
	notebook, err = normalizeNotebook(notebook)
	if err != nil {
		return Notebook{}, err
	}
	id, err := s.dialect.insertReturningID(ctx, s.db,
		`INSERT INTO notebooks (scope_id, display_name, destination_kind, notebook_id, section_id, site_id, display_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		scopeID, notebook.DisplayName, string(notebook.Destination.Kind),
		notebook.Destination.NotebookID, notebook.Destination.SectionID, notebook.Destination.SiteID,
		notebook.Destination.DisplayPath,
	)
	if err != nil {
		return Notebook{}, fmt.Errorf("create notebook: %w", err)
	}
	created, err := s.GetNotebook(ctx, id)
	if err != nil {
		return Notebook{}, err
	}
	return *created, nil
}

// GetNotebook implements NotebookStore.
func (s *sqlStore) GetNotebook(ctx context.Context, id int64) (*Notebook, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	return scanNotebook(s.db.QueryRowContext(ctx, s.dialect.rebind(notebookSelectSQL+` WHERE id = ? AND scope_id = ?`), id, scopeID))
}

// ListNotebooks implements NotebookStore. The newest notebook comes first.
func (s *sqlStore) ListNotebooks(ctx context.Context) ([]Notebook, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	return s.listNotebooksByScopeID(ctx, scopeID)
}

func (s *sqlStore) listNotebooksByScopeID(ctx context.Context, scopeID int64) ([]Notebook, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(notebookSelectSQL+` WHERE scope_id = ? ORDER BY created_at DESC, id DESC`), scopeID)
	if err != nil {
		return nil, fmt.Errorf("list notebooks: %w", err)
	}
	defer rows.Close() //nolint:errcheck // rows.Err reports iteration failures
	out := make([]Notebook, 0)
	for rows.Next() {
		notebook, err := scanNotebook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *notebook)
	}
	return out, rows.Err()
}

// UpdateNotebook implements NotebookStore.
func (s *sqlStore) UpdateNotebook(ctx context.Context, notebook Notebook) (Notebook, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return Notebook{}, err
	}
	id := notebook.ID
	notebook, err = normalizeNotebook(notebook)
	if err != nil {
		return Notebook{}, err
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE notebooks
		 SET display_name = ?, destination_kind = ?, notebook_id = ?, section_id = ?, site_id = ?, display_path = ?, updated_at = %s
		 WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		notebook.DisplayName, string(notebook.Destination.Kind), notebook.Destination.NotebookID,
		notebook.Destination.SectionID, notebook.Destination.SiteID, notebook.Destination.DisplayPath, id, scopeID,
	)
	if err != nil {
		return Notebook{}, fmt.Errorf("update notebook: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return Notebook{}, sql.ErrNoRows
	}
	updated, err := s.GetNotebook(ctx, id)
	if err != nil {
		return Notebook{}, err
	}
	return *updated, nil
}

// MarkNotebookSynced implements NotebookStore.
func (s *sqlStore) MarkNotebookSynced(ctx context.Context, id int64, at time.Time) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	if at.IsZero() {
		at = time.Now()
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE notebooks SET last_synced_at = ?, updated_at = %s WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		s.dialect.timeArg(at), id, scopeID)
	if err != nil {
		return fmt.Errorf("mark notebook synced: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteNotebook implements NotebookStore. The references are cleared in the
// same transaction rather than left to ON DELETE SET NULL alone, so the
// sessions keep existing without a dangling link even where a connection does
// not enforce foreign keys.
func (s *sqlStore) DeleteNotebook(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	if err := s.ensureNotebookInScope(ctx, id, scopeID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin notebook delete: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err := tx.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions SET notebook_id = NULL, updated_at = %s WHERE notebook_id = ?`, s.dialect.now())), id); err != nil {
		return fmt.Errorf("unlink notebook sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, s.dialect.rebind(`DELETE FROM notebooks WHERE id = ? AND scope_id = ?`), id, scopeID); err != nil {
		return fmt.Errorf("delete notebook: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notebook delete: %w", err)
	}
	return nil
}

// SetRecordingSessionNotebook implements NotebookStore.
func (s *sqlStore) SetRecordingSessionNotebook(ctx context.Context, sessionID, notebookID int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	if notebookID < 0 {
		return fmt.Errorf("%w: negative notebook id", ErrNotebookInvalid)
	}
	if notebookID > 0 {
		if err := s.ensureNotebookInScope(ctx, notebookID, scopeID); err != nil {
			return err
		}
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE recording_sessions SET notebook_id = ?, updated_at = %s WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		nullableInt64(notebookID), sessionID, scopeID)
	if err != nil {
		return fmt.Errorf("link recording session notebook: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *sqlStore) ensureNotebookInScope(ctx context.Context, id, scopeID int64) error {
	var found int64
	return s.db.QueryRowContext(ctx, s.dialect.rebind(`SELECT id FROM notebooks WHERE id = ? AND scope_id = ?`), id, scopeID).Scan(&found)
}

// normalizeNotebook trims the fields and rejects a notebook without a name or
// without a complete destination.
func normalizeNotebook(notebook Notebook) (Notebook, error) {
	notebook.DisplayName = strings.TrimSpace(notebook.DisplayName)
	notebook.Destination.Kind = NotebookDestinationKind(strings.ToLower(strings.TrimSpace(string(notebook.Destination.Kind))))
	notebook.Destination.NotebookID = strings.TrimSpace(notebook.Destination.NotebookID)
	notebook.Destination.SectionID = strings.TrimSpace(notebook.Destination.SectionID)
	notebook.Destination.SiteID = strings.TrimSpace(notebook.Destination.SiteID)
	notebook.Destination.DisplayPath = strings.TrimSpace(notebook.Destination.DisplayPath)
	if notebook.DisplayName == "" {
		return Notebook{}, fmt.Errorf("%w: a display name is required", ErrNotebookInvalid)
	}
	switch notebook.Destination.Kind {
	case NotebookDestinationOneNote:
		if notebook.Destination.NotebookID == "" || notebook.Destination.SectionID == "" {
			return Notebook{}, fmt.Errorf("%w: a OneNote destination needs a notebook and a section", ErrNotebookInvalid)
		}
	default:
		return Notebook{}, fmt.Errorf("%w: unknown destination kind", ErrNotebookInvalid)
	}
	return notebook, nil
}

func scanNotebook(row recordingSessionRow) (*Notebook, error) {
	var notebook Notebook
	var kind string
	var lastSynced sql.NullTime
	if err := row.Scan(
		&notebook.ID, &notebook.DisplayName, &kind, &notebook.Destination.NotebookID,
		&notebook.Destination.SectionID, &notebook.Destination.SiteID, &notebook.Destination.DisplayPath, &lastSynced,
		&notebook.CreatedAt, &notebook.UpdatedAt,
	); err != nil {
		return nil, err
	}
	notebook.Destination.Kind = NotebookDestinationKind(kind)
	if lastSynced.Valid {
		notebook.LastSyncedAt = lastSynced.Time.UTC()
	}
	return &notebook, nil
}

// runSQLiteNotebooksMigration creates the notebook index and the session
// reference. SQLite adds a REFERENCES column only with a NULL default, which
// is what an unlinked session has.
func runSQLiteNotebooksMigration(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, sqliteMigration032); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "recording_sessions", "notebook_id", "INTEGER REFERENCES notebooks(id) ON DELETE SET NULL"); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_recording_sessions_notebook ON recording_sessions(notebook_id)`)
	return err
}

// runSQLiteNotebookSiteMigration adds the SharePoint site a OneNote
// destination lives in; empty is the user's own notebooks.
func runSQLiteNotebookSiteMigration(ctx context.Context, db *sql.DB) error {
	return ensureSQLiteColumn(ctx, db, "notebooks", "site_id", "TEXT NOT NULL DEFAULT ''")
}
