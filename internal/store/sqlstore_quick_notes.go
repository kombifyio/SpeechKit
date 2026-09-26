package store

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ── Quick notes ──────────────────────────────────────────────────────────────

const quickNoteSelectSQL = `SELECT q.id, q.text, q.language, q.provider, COALESCE(q.duration_ms, 0), COALESCE(q.latency_ms, 0),
		COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
		COALESCE(q.pinned, %s), q.created_at, q.updated_at
	 FROM quick_notes q
	 LEFT JOIN audio_assets a ON a.id = (
		SELECT link.audio_asset_id
		FROM quick_note_audio_assets link
		JOIN audio_assets asset ON asset.id = link.audio_asset_id
		WHERE link.quick_note_id = q.id AND link.role = 'source'
		ORDER BY link.created_at DESC, link.audio_asset_id DESC
		LIMIT 1
	 )`

func (s *sqlStore) scanQuickNote(sc interface{ Scan(...any) error }) (QuickNote, error) {
	var n QuickNote
	var pinned boolValue
	var audioStorageKind, audioMimeType string
	var audioSizeBytes, audioDurationMs int64
	if err := sc.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs,
		&n.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return QuickNote{}, err
	}
	n.Pinned = bool(pinned)
	n.Audio = buildAudioAsset(audioStorageKind, n.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
	return n, nil
}

func (s *sqlStore) SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	audioPath, err := s.persistAudio(audioAssetInputFromBytes(audioData), "qn_")
	if err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	id, err := s.dialect.insertReturningID(ctx, tx,
		`INSERT INTO quick_notes (scope_id, text, language, language_base, provider, duration_ms, latency_ms, word_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID, text, language, normalizeDictionaryLanguage(language), provider, durationMs, latencyMs, countWords(text),
	)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, s.dialect.name, scopeID, "quick_note", id, audioPath, durationMs); err != nil {
			return 0, fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return 0, fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit quick note: %w", err)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return id, nil
}

func (s *sqlStore) GetQuickNote(ctx context.Context, id int64) (*QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(quickNoteSelectSQL, s.dialect.boolLit(false)) + ` WHERE q.id = ? AND q.scope_id = ?`
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(query), id, scopeID)
	n, err := s.scanQuickNote(row)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *sqlStore) ListQuickNotes(ctx context.Context, opts ListOpts) ([]QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	query := fmt.Sprintf(quickNoteSelectSQL, s.dialect.boolLit(false))
	args := []any{scopeID}
	clauses := []string{"q.scope_id = ?"}
	clauses, args = appendNormalizedLanguageFilter(clauses, args, opts.Language)
	if !opts.After.IsZero() {
		clauses = append(clauses, "q.created_at > ?")
		args = append(args, s.dialect.timeArg(opts.After))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	query += " ORDER BY q.pinned DESC, q.created_at DESC, q.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	results := make([]QuickNote, 0)
	for rows.Next() {
		n, err := s.scanQuickNote(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, n)
	}
	return results, rows.Err()
}

func (s *sqlStore) UpdateQuickNote(ctx context.Context, id int64, text string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	result, err := tx.ExecContext(ctx,
		s.dialect.rebind(fmt.Sprintf(`UPDATE quick_notes SET text = ?, word_count = ?, updated_at = %s WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		text, countWords(text), id, scopeID,
	)
	if err != nil {
		return fmt.Errorf("update quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note update: %w", err)
	}
	return nil
}

// quickNoteAudioPathSQL fetches the current source-audio path for a note.
const quickNoteAudioPathSQL = `SELECT COALESCE(a.path, '')
	FROM quick_notes q
	LEFT JOIN audio_assets a ON a.id = (
		SELECT link.audio_asset_id
		FROM quick_note_audio_assets link
		JOIN audio_assets asset ON asset.id = link.audio_asset_id
		WHERE link.quick_note_id = q.id AND link.role = 'source'
		ORDER BY link.created_at DESC, link.audio_asset_id DESC
		LIMIT 1
	)
	WHERE q.id = ? AND q.scope_id = ?
	LIMIT 1`

func (s *sqlStore) UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var currentAudioPath string
	if err := tx.QueryRowContext(ctx, s.dialect.rebind(quickNoteAudioPathSQL), id, scopeID).Scan(&currentAudioPath); err != nil {
		return fmt.Errorf("lookup quick note %d: %w", id, err)
	}

	nextAudioPath, err := s.persistAudio(audioAssetInputFromBytes(audioData), "qn_")
	if err != nil {
		return fmt.Errorf("%s: persist quicknote audio: %w", s.dialect.name, err)
	}
	if nextAudioPath == "" {
		nextAudioPath = currentAudioPath
	}
	createdNewAudio := nextAudioPath != "" && currentAudioPath != nextAudioPath
	defer func() {
		if !committed && createdNewAudio {
			_ = os.Remove(nextAudioPath)
		}
	}()

	result, err := tx.ExecContext(ctx,
		s.dialect.rebind(fmt.Sprintf(`UPDATE quick_notes
		 SET text = ?, provider = ?, duration_ms = ?, latency_ms = ?, word_count = ?, updated_at = %s
		 WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		text, provider, durationMs, latencyMs, countWords(text), id, scopeID,
	)
	if err != nil {
		return fmt.Errorf("update quick note capture: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := deleteAudioAsset(ctx, tx, s.dialect.name, "quick_note", id, currentAudioPath); err != nil {
			return fmt.Errorf("delete previous audio asset: %w", err)
		}
	}
	if nextAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := recordScopedAudioAsset(ctx, tx, s.dialect.name, scopeID, "quick_note", id, nextAudioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note capture: %w", err)
	}
	committed = true
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		_ = os.Remove(currentAudioPath)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return nil
}

func (s *sqlStore) PinQuickNote(ctx context.Context, id int64, pinned bool) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(`UPDATE quick_notes SET pinned = ? WHERE id = ? AND scope_id = ?`), pinned, id, scopeID)
	if err != nil {
		return fmt.Errorf("pin quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *sqlStore) DeleteQuickNote(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var audioPath string
	_ = tx.QueryRowContext(ctx, s.dialect.rebind(quickNoteAudioPathSQL), id, scopeID).Scan(&audioPath)

	if audioPath != "" {
		if err := deleteAudioAssetsForOwner(ctx, tx, s.dialect.name, "quick_note", id); err != nil {
			return fmt.Errorf("delete quick note audio assets: %w", err)
		}
	}

	result, err := tx.ExecContext(ctx, s.dialect.rebind(`DELETE FROM quick_notes WHERE id = ? AND scope_id = ?`), id, scopeID)
	if err != nil {
		return fmt.Errorf("delete quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note delete: %w", err)
	}
	committed = true
	if audioPath != "" {
		_ = os.Remove(audioPath)
	}
	return nil
}

func (s *sqlStore) QuickNoteCount(ctx context.Context) (int, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, s.dialect.rebind(`SELECT COUNT(*) FROM quick_notes WHERE scope_id = ?`), scopeID).Scan(&count)
	return count, err
}
