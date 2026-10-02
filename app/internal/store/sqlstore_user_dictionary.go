package store

import (
	"context"
	"fmt"
	"strings"
)

// ── User dictionary ──────────────────────────────────────────────────────────

func (s *sqlStore) ReplaceUserDictionaryEntries(ctx context.Context, language string, entries []UserDictionaryEntry) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	language = normalizeDictionaryLanguage(language)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		s.dialect.rebind(`DELETE FROM user_dictionary_entries WHERE scope_id = ? AND language = ? AND source = ?`),
		scopeID, language, userDictionarySettingsSource,
	); err != nil {
		return fmt.Errorf("clear user dictionary entries: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`INSERT INTO user_dictionary_entries (scope_id, spoken, canonical, language, source, enabled)
		 VALUES (?, ?, ?, ?, ?, %s)
		 ON CONFLICT(scope_id, spoken, canonical, language, source)
		 DO UPDATE SET enabled = %s, updated_at = %s`,
		s.dialect.boolLit(true), s.dialect.boolLit(true), s.dialect.now())),
	)
	if err != nil {
		return fmt.Errorf("prepare user dictionary insert: %w", err)
	}
	defer stmt.Close() //nolint:errcheck // statement close during transaction cleanup

	for _, entry := range entries {
		entry, ok := normalizeUserDictionaryEntry(entry, language)
		if !ok {
			continue
		}
		if _, err = stmt.ExecContext(ctx, scopeID, entry.Spoken, entry.Canonical, entry.Language, entry.Source); err != nil {
			return fmt.Errorf("insert user dictionary entry: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit user dictionary entries: %w", err)
	}
	if err := s.replaceDictionaryCustomizationProjection(ctx, language, entries); err != nil {
		return fmt.Errorf("sync customization projection: %w", err)
	}
	return nil
}

func (s *sqlStore) ListUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error) {
	projected, projectionErr := s.listProjectedUserDictionaryEntries(ctx, language)
	if projectionErr == nil && len(projected) > 0 {
		return projected, nil
	}
	if projectionErr != nil && !isMissingCustomizationTable(projectionErr) {
		return nil, projectionErr
	}

	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	language = normalizeDictionaryLanguage(language)
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`SELECT id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		 FROM user_dictionary_entries
		 WHERE scope_id = ? AND enabled = %s AND (? = '' OR language = ? OR language = '')
		 ORDER BY id ASC`, s.dialect.boolLit(true))),
		scopeID, language, language,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	entries := make([]UserDictionaryEntry, 0)
	for rows.Next() {
		var entry UserDictionaryEntry
		var enabled boolValue
		if err := rows.Scan(
			&entry.ID, &entry.Spoken, &entry.Canonical, &entry.Language, &entry.Source,
			&enabled, &entry.UsageCount, &entry.CreatedAt, &entry.UpdatedAt,
		); err != nil {
			return nil, err
		}
		entry.Enabled = bool(enabled)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *sqlStore) RecordUserDictionaryUsage(ctx context.Context, canonical, language string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	canonical = strings.TrimSpace(canonical)
	language = normalizeDictionaryLanguage(language)
	if canonical == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE user_dictionary_entries
		 SET usage_count = usage_count + 1, updated_at = %s
		 WHERE scope_id = ? AND enabled = %s AND lower(canonical) = lower(?) AND (? = '' OR language = ? OR language = '')`,
		s.dialect.now(), s.dialect.boolLit(true))),
		scopeID, canonical, language, language,
	)
	if err != nil {
		return fmt.Errorf("record user dictionary usage: %w", err)
	}
	if err := s.RecordWordUsage(ctx, canonical, language); err != nil && !isMissingCustomizationTable(err) {
		return fmt.Errorf("record customization word usage: %w", err)
	}
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE customization_replacements
		 SET usage_count = usage_count + 1, updated_at = %s
		 WHERE scope_id = ? AND enabled = %s AND lower(output_text) = lower(?) AND (? = '' OR language = ? OR language = '') AND stage = 'post_stt'`,
		s.dialect.now(), s.dialect.boolLit(true))),
		scopeID, canonical, language, language,
	)
	if err != nil && !isMissingCustomizationTable(err) {
		return fmt.Errorf("record customization replacement usage: %w", err)
	}
	return nil
}
