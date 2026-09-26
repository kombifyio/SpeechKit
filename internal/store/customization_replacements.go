package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func (s *sqlStore) ReplaceReplacements(ctx context.Context, language string, replacements []speechcustomize.Replacement) error {
	return s.ReplaceReplacementsWithOptions(ctx, CustomizationReplaceOpts{Language: language, Source: userDictionarySettingsSource}, replacements)
}

func (s *sqlStore) ReplaceReplacementsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, replacements []speechcustomize.Replacement) error {
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

	if err := s.replaceReplacementsTx(ctx, tx, scopeID, language, source, replacements); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sqlStore) replaceReplacementsTx(ctx context.Context, tx *sql.Tx, scopeID int64, language, source string, replacements []speechcustomize.Replacement) error {
	if _, err := tx.ExecContext(ctx,
		s.dialect.rebind(`DELETE FROM customization_replacements WHERE scope_id = ? AND language = ? AND source = ?`),
		scopeID, language, source,
	); err != nil {
		return fmt.Errorf("clear customization replacements: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`INSERT INTO customization_replacements
		 (scope_id, id, kind, match_type, match_pattern, match_case_sensitive, match_word_boundary,
		  output_text, output_intent, output_template, output_payload_json, language, modes_json, stage, priority, tags_json, source, enabled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?%s, ?, ?%s, ?, ?, ?%s, ?, ?)
		 ON CONFLICT(scope_id, id)
		 DO UPDATE SET kind = excluded.kind, match_type = excluded.match_type, match_pattern = excluded.match_pattern,
			match_case_sensitive = excluded.match_case_sensitive, match_word_boundary = excluded.match_word_boundary,
			output_text = excluded.output_text, output_intent = excluded.output_intent, output_template = excluded.output_template,
			output_payload_json = excluded.output_payload_json, language = excluded.language, modes_json = excluded.modes_json,
			stage = excluded.stage, priority = excluded.priority, tags_json = excluded.tags_json, source = excluded.source, enabled = excluded.enabled,
			updated_at = %s`,
		s.dialect.jsonbInsert(), s.dialect.jsonbInsert(), s.dialect.jsonbInsert(), s.dialect.now(),
	)))
	if err != nil {
		return fmt.Errorf("prepare customization replacement insert: %w", err)
	}
	defer stmt.Close() //nolint:errcheck // statement close during transaction cleanup is not actionable

	replacements = speechcustomize.MergeReplacements(replacements)
	for _, replacement := range replacements {
		replacement.Language = normalizeDictionaryLanguage(firstNonEmpty(replacement.Language, language))
		replacement.Source = normalizeCustomizationSource(firstNonEmpty(replacement.Source, source))
		replacement = speechcustomize.WithDefaultsReplacement(replacement)
		if err := speechcustomize.ValidateReplacement(replacement); err != nil {
			return err
		}
		payload, err := jsonObject(replacement.Output.Payload)
		if err != nil {
			return err
		}
		modes, err := jsonList(replacementModes(replacement.Modes))
		if err != nil {
			return err
		}
		tags, err := jsonList(replacement.Tags)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			scopeID, replacement.ID, replacement.Kind, replacement.Match.Type, replacement.Match.Pattern,
			replacement.Match.CaseSensitive, replacement.Match.WordBoundary,
			replacement.Output.Text, replacement.Output.Intent, replacement.Output.Template, payload,
			replacement.Language, modes, replacement.Stage, replacement.Priority, tags, replacement.Source, replacement.Enabled,
		); err != nil {
			return fmt.Errorf("insert customization replacement: %w", err)
		}
	}
	return nil
}

func (s *sqlStore) ListReplacements(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Replacement, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	language := normalizeDictionaryLanguage(opts.Language)
	stage := speechcustomize.NormalizeStage(opts.Stage)
	query := fmt.Sprintf(`SELECT id, kind, match_type, match_pattern, match_case_sensitive, match_word_boundary,
			output_text, output_intent, output_template, %s, language, %s, stage, priority, %s, source, enabled, usage_count, created_at, updated_at
		FROM customization_replacements
		WHERE scope_id = ? AND (? = '' OR language = ? OR language = '')`,
		s.dialect.jsonbText("output_payload_json"), s.dialect.jsonbText("modes_json"), s.dialect.jsonbText("tags_json"))
	args := []any{scopeID, language, language}
	if stage != "" {
		query += " AND stage = ?"
		args = append(args, stage)
	}
	if !opts.IncludeDisabled {
		query += fmt.Sprintf(" AND enabled = %s", s.dialect.boolLit(true))
	}
	if strings.TrimSpace(opts.Source) != "" {
		query += " AND source = ?"
		args = append(args, strings.TrimSpace(strings.ToLower(opts.Source)))
	}
	query += " ORDER BY priority DESC, length(match_pattern) DESC, id ASC"
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err below reports iteration failures; close error is not actionable

	replacements := make([]speechcustomize.Replacement, 0)
	for rows.Next() {
		var replacement speechcustomize.Replacement
		var payloadRaw, modesRaw, tagsRaw string
		var enabled, caseSensitive, wordBoundary boolValue
		if err := rows.Scan(
			&replacement.ID, &replacement.Kind, &replacement.Match.Type, &replacement.Match.Pattern, &caseSensitive, &wordBoundary,
			&replacement.Output.Text, &replacement.Output.Intent, &replacement.Output.Template, &payloadRaw,
			&replacement.Language, &modesRaw, &replacement.Stage, &replacement.Priority, &tagsRaw, &replacement.Source, &enabled,
			&replacement.UsageCount, &replacement.CreatedAt, &replacement.UpdatedAt,
		); err != nil {
			return nil, err
		}
		replacement.Enabled = bool(enabled)
		replacement.Match.CaseSensitive = bool(caseSensitive)
		replacement.Match.WordBoundary = bool(wordBoundary)
		replacement.Output.Payload = unmarshalMap(payloadRaw)
		replacement.Modes = unmarshalModes(modesRaw)
		replacement.Tags = unmarshalStringList(tagsRaw)
		if opts.Mode != "" && len(replacement.Modes) > 0 && !replacementHasMode(replacement, speechcustomize.NormalizeMode(opts.Mode)) {
			continue
		}
		replacements = append(replacements, replacement)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return speechcustomize.MergeReplacements(replacements), nil
}

func (s *sqlStore) RecordReplacementUsage(ctx context.Context, id string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE customization_replacements SET usage_count = usage_count + 1, updated_at = %s WHERE scope_id = ? AND id = ?`,
		s.dialect.now(),
	)), scopeID, id)
	return err
}
