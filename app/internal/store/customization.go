package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func (s *sqlStore) ReplaceWords(ctx context.Context, language string, words []speechcustomize.Word) error {
	return s.ReplaceWordsWithOptions(ctx, CustomizationReplaceOpts{Language: language, Source: userDictionarySettingsSource}, words)
}

func (s *sqlStore) ReplaceWordsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, words []speechcustomize.Word) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	language := normalizeDictionaryLanguage(opts.Language)
	source := normalizeCustomizationSource(opts.Source)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	if err := s.replaceWordsTx(ctx, tx, scopeID, language, source, words); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlStore) ReplaceVocabularyWithOptions(ctx context.Context, opts CustomizationReplaceOpts, words []speechcustomize.Word, extras []speechcustomize.Replacement) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	language := normalizeDictionaryLanguage(opts.Language)
	source := normalizeCustomizationSource(opts.Source)
	merged, replacements := speechcustomize.MaterializeVocabulary(words, extras)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if err := s.replaceWordsTx(ctx, tx, scopeID, language, source, merged); err != nil {
		return err
	}
	if err := s.replaceReplacementsTx(ctx, tx, scopeID, language, source, replacements); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlStore) replaceWordsTx(ctx context.Context, tx *sql.Tx, scopeID int64, language, source string, words []speechcustomize.Word) error {
	if _, err := tx.ExecContext(ctx,
		s.dialect.rebind(`DELETE FROM customization_words WHERE scope_id = ? AND language = ? AND source = ?`),
		scopeID, language, source,
	); err != nil {
		return fmt.Errorf("clear customization words: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`INSERT INTO customization_words
		 (scope_id, id, term, sounds_like_json, language, weight, tags_json, source, enabled)
		 VALUES (?, ?, ?, ?%s, ?, ?, ?%s, ?, ?)
		 ON CONFLICT(scope_id, id)
		 DO UPDATE SET term = excluded.term, sounds_like_json = excluded.sounds_like_json, language = excluded.language,
			weight = excluded.weight, tags_json = excluded.tags_json, source = excluded.source,
			enabled = excluded.enabled, updated_at = %s`,
		s.dialect.jsonbInsert(), s.dialect.jsonbInsert(), s.dialect.now(),
	)))
	if err != nil {
		return fmt.Errorf("prepare customization word insert: %w", err)
	}
	defer stmt.Close() //nolint:errcheck // statement close during transaction cleanup is not actionable

	words = speechcustomize.MergeWords(words)
	for _, word := range words {
		word.Language = normalizeDictionaryLanguage(firstNonEmpty(word.Language, language))
		word.Source = normalizeCustomizationSource(firstNonEmpty(word.Source, source))
		word = speechcustomize.WithDefaultsWord(word)
		if err := speechcustomize.ValidateWord(word); err != nil {
			return err
		}
		soundsLike, err := jsonList(word.SoundsLike)
		if err != nil {
			return err
		}
		tags, err := jsonList(word.Tags)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx, scopeID, word.ID, word.Term, soundsLike, word.Language, word.Weight, tags, word.Source, word.Enabled); err != nil {
			return fmt.Errorf("insert customization word: %w", err)
		}
	}
	return nil
}

func (s *sqlStore) ListWords(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Word, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	language := normalizeDictionaryLanguage(opts.Language)
	query := fmt.Sprintf(`SELECT id, term, %s, language, weight, %s, source, enabled, usage_count, created_at, updated_at
		FROM customization_words
		WHERE scope_id = ? AND (? = '' OR language = ? OR language = '')`,
		s.dialect.jsonbText("sounds_like_json"), s.dialect.jsonbText("tags_json"))
	args := []any{scopeID, language, language}
	if !opts.IncludeDisabled {
		query += fmt.Sprintf(" AND enabled = %s", s.dialect.boolLit(true))
	}
	if strings.TrimSpace(opts.Source) != "" {
		query += " AND source = ?"
		args = append(args, strings.TrimSpace(strings.ToLower(opts.Source)))
	}
	query += " ORDER BY term ASC, id ASC"
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err below reports iteration failures; close error is not actionable

	words := make([]speechcustomize.Word, 0)
	for rows.Next() {
		var word speechcustomize.Word
		var soundsLikeRaw, tagsRaw string
		var enabled boolValue
		if err := rows.Scan(&word.ID, &word.Term, &soundsLikeRaw, &word.Language, &word.Weight, &tagsRaw, &word.Source, &enabled, &word.UsageCount, &word.CreatedAt, &word.UpdatedAt); err != nil {
			return nil, err
		}
		word.Enabled = bool(enabled)
		word.SoundsLike = unmarshalStringList(soundsLikeRaw)
		word.Tags = unmarshalStringList(tagsRaw)
		words = append(words, word)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return speechcustomize.MergeWords(words), nil
}

func (s *sqlStore) RecordWordUsage(ctx context.Context, term, language string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	term = strings.TrimSpace(term)
	language = normalizeDictionaryLanguage(language)
	if term == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE customization_words
		 SET usage_count = usage_count + 1, updated_at = %s
		 WHERE scope_id = ? AND enabled = %s AND lower(term) = lower(?) AND (? = '' OR language = ? OR language = '')`,
		s.dialect.now(), s.dialect.boolLit(true))),
		scopeID, term, language, language,
	)
	return err
}

func (s *sqlStore) replaceNamedCustomization(ctx context.Context, table string, opts CustomizationReplaceOpts, insert func(context.Context, *sql.Tx, int64, string, string) error) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	language := normalizeDictionaryLanguage(opts.Language)
	source := normalizeCustomizationSource(opts.Source)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	if _, err := tx.ExecContext(ctx,
		s.dialect.rebind(fmt.Sprintf("DELETE FROM %s WHERE scope_id = ? AND language = ? AND source = ?", table)),
		scopeID, language, source,
	); err != nil {
		return err
	}
	if err := insert(ctx, tx, scopeID, language, source); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlStore) listNamedCustomizationRows(ctx context.Context, table, idsColumn string, opts CustomizationListOpts) (*sql.Rows, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	language := normalizeDictionaryLanguage(opts.Language)
	query := fmt.Sprintf(`SELECT id, name, description, language, %s, %s, source, enabled, created_at, updated_at
		FROM %s WHERE scope_id = ? AND (? = '' OR language = ? OR language = '')`,
		s.dialect.jsonbText(idsColumn), s.dialect.jsonbText("tags_json"), table)
	args := []any{scopeID, language, language}
	if !opts.IncludeDisabled {
		query += fmt.Sprintf(" AND enabled = %s", s.dialect.boolLit(true))
	}
	query += " ORDER BY name ASC, id ASC"
	return s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
}
