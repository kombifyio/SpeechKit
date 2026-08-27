package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
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
	return tx.Commit()
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
	return words, rows.Err()
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
	return tx.Commit()
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
	return replacements, rows.Err()
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

func (s *sqlStore) ReplaceLexicons(ctx context.Context, language string, lexicons []speechcustomize.Lexicon) error {
	return s.ReplaceLexiconsWithOptions(ctx, CustomizationReplaceOpts{Language: language, Source: userDictionarySettingsSource}, lexicons)
}

func (s *sqlStore) ReplaceLexiconsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, lexicons []speechcustomize.Lexicon) error {
	return s.replaceNamedCustomization(ctx, "customization_lexicons", opts, func(ctx context.Context, tx *sql.Tx, scopeID int64, language, source string) error {
		stmt, err := tx.PrepareContext(ctx, s.dialect.rebind(fmt.Sprintf(
			`INSERT INTO customization_lexicons
			 (scope_id, id, name, description, language, word_ids_json, tags_json, source, enabled)
			 VALUES (?, ?, ?, ?, ?, ?%s, ?%s, ?, ?)
			 ON CONFLICT(scope_id, id) DO UPDATE SET name = excluded.name, description = excluded.description,
				language = excluded.language, word_ids_json = excluded.word_ids_json, tags_json = excluded.tags_json,
				source = excluded.source, enabled = excluded.enabled, updated_at = %s`,
			s.dialect.jsonbInsert(), s.dialect.jsonbInsert(), s.dialect.now(),
		)))
		if err != nil {
			return err
		}
		defer stmt.Close() //nolint:errcheck // statement close during transaction cleanup is not actionable
		for _, lexicon := range lexicons {
			lexicon.ID = strings.TrimSpace(lexicon.ID)
			lexicon.Name = strings.TrimSpace(lexicon.Name)
			lexicon.Language = normalizeDictionaryLanguage(firstNonEmpty(lexicon.Language, language))
			lexicon.Source = normalizeCustomizationSource(firstNonEmpty(lexicon.Source, source))
			if lexicon.ID == "" {
				lexicon.ID = speechcustomize.StableID("lexicon", lexicon.Language, lexicon.Name, lexicon.Source)
			}
			if lexicon.Name == "" {
				return fmt.Errorf("customize: lexicon name is required")
			}
			wordIDs, _ := jsonList(lexicon.WordIDs)
			tags, _ := jsonList(lexicon.Tags)
			if _, err := stmt.ExecContext(ctx, scopeID, lexicon.ID, lexicon.Name, lexicon.Description, lexicon.Language, wordIDs, tags, lexicon.Source, lexicon.Enabled); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *sqlStore) ListLexicons(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Lexicon, error) {
	rows, err := s.listNamedCustomizationRows(ctx, "customization_lexicons", "word_ids_json", opts)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err below reports iteration failures; close error is not actionable
	items := make([]speechcustomize.Lexicon, 0)
	for rows.Next() {
		var item speechcustomize.Lexicon
		var idsRaw, tagsRaw string
		var enabled boolValue
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Language, &idsRaw, &tagsRaw, &item.Source, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.WordIDs = unmarshalStringList(idsRaw)
		item.Tags = unmarshalStringList(tagsRaw)
		item.Enabled = bool(enabled)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *sqlStore) ReplaceRulesets(ctx context.Context, language string, rulesets []speechcustomize.Ruleset) error {
	return s.ReplaceRulesetsWithOptions(ctx, CustomizationReplaceOpts{Language: language, Source: userDictionarySettingsSource}, rulesets)
}

func (s *sqlStore) ReplaceRulesetsWithOptions(ctx context.Context, opts CustomizationReplaceOpts, rulesets []speechcustomize.Ruleset) error {
	return s.replaceNamedCustomization(ctx, "customization_rulesets", opts, func(ctx context.Context, tx *sql.Tx, scopeID int64, language, source string) error {
		stmt, err := tx.PrepareContext(ctx, s.dialect.rebind(fmt.Sprintf(
			`INSERT INTO customization_rulesets
			 (scope_id, id, name, description, language, replacement_ids_json, tags_json, source, enabled)
			 VALUES (?, ?, ?, ?, ?, ?%s, ?%s, ?, ?)
			 ON CONFLICT(scope_id, id) DO UPDATE SET name = excluded.name, description = excluded.description,
				language = excluded.language, replacement_ids_json = excluded.replacement_ids_json, tags_json = excluded.tags_json,
				source = excluded.source, enabled = excluded.enabled, updated_at = %s`,
			s.dialect.jsonbInsert(), s.dialect.jsonbInsert(), s.dialect.now(),
		)))
		if err != nil {
			return err
		}
		defer stmt.Close() //nolint:errcheck // statement close during transaction cleanup is not actionable
		for _, ruleset := range rulesets {
			ruleset.ID = strings.TrimSpace(ruleset.ID)
			ruleset.Name = strings.TrimSpace(ruleset.Name)
			ruleset.Language = normalizeDictionaryLanguage(firstNonEmpty(ruleset.Language, language))
			ruleset.Source = normalizeCustomizationSource(firstNonEmpty(ruleset.Source, source))
			if ruleset.ID == "" {
				ruleset.ID = speechcustomize.StableID("ruleset", ruleset.Language, ruleset.Name, ruleset.Source)
			}
			if ruleset.Name == "" {
				return fmt.Errorf("customize: ruleset name is required")
			}
			replacementIDs, _ := jsonList(ruleset.ReplacementIDs)
			tags, _ := jsonList(ruleset.Tags)
			if _, err := stmt.ExecContext(ctx, scopeID, ruleset.ID, ruleset.Name, ruleset.Description, ruleset.Language, replacementIDs, tags, ruleset.Source, ruleset.Enabled); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *sqlStore) ListRulesets(ctx context.Context, opts CustomizationListOpts) ([]speechcustomize.Ruleset, error) {
	rows, err := s.listNamedCustomizationRows(ctx, "customization_rulesets", "replacement_ids_json", opts)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // rows.Err below reports iteration failures; close error is not actionable
	items := make([]speechcustomize.Ruleset, 0)
	for rows.Next() {
		var item speechcustomize.Ruleset
		var idsRaw, tagsRaw string
		var enabled boolValue
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Language, &idsRaw, &tagsRaw, &item.Source, &enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ReplacementIDs = unmarshalStringList(idsRaw)
		item.Tags = unmarshalStringList(tagsRaw)
		item.Enabled = bool(enabled)
		items = append(items, item)
	}
	return items, rows.Err()
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

func (s *sqlStore) replaceDictionaryCustomizationProjection(ctx context.Context, language string, entries []UserDictionaryEntry) error {
	projected := make([]internalcustomize.DictionaryEntry, 0, len(entries))
	for _, entry := range entries {
		normalized, ok := normalizeUserDictionaryEntry(entry, language)
		if !ok {
			continue
		}
		projected = append(projected, internalcustomize.DictionaryEntry{
			Spoken:     normalized.Spoken,
			Canonical:  normalized.Canonical,
			Language:   normalized.Language,
			Source:     normalized.Source,
			Enabled:    normalized.Enabled,
			UsageCount: normalized.UsageCount,
		})
	}
	if err := s.ReplaceWords(ctx, language, internalcustomize.WordsFromDictionary(projected)); err != nil {
		return err
	}
	return s.ReplaceReplacements(ctx, language, internalcustomize.ReplacementsFromDictionary(projected))
}

func (s *sqlStore) listProjectedUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error) {
	service := internalcustomize.NewService(internalcustomize.ServiceOptions{
		Store:                   s,
		ScopeOrder:              speechcustomize.DefaultDeviceScopeOrder(),
		DisableBuiltinTemplates: true,
	})
	resolved, err := service.Resolve(ctx, speechcustomize.Context{
		Language: language,
		Mode:     speechcustomize.ModeDictation,
		Stage:    speechcustomize.StagePostSTT,
	})
	if err != nil {
		return nil, err
	}
	projected := internalcustomize.DictionaryFromSet(internalcustomize.Set{Words: resolved.Words, Replacements: resolved.Replacements})
	result := make([]UserDictionaryEntry, 0, len(projected))
	for _, entry := range projected {
		result = append(result, UserDictionaryEntry{
			ID:         stableDictionaryProjectionID(entry),
			Spoken:     entry.Spoken,
			Canonical:  entry.Canonical,
			Language:   entry.Language,
			Source:     entry.Source,
			Enabled:    entry.Enabled,
			UsageCount: entry.UsageCount,
		})
	}
	return result, nil
}

func stableDictionaryProjectionID(entry internalcustomize.DictionaryEntry) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(strings.Join([]string{entry.Language, entry.Spoken, entry.Canonical, entry.Source}, "\x00"))))
	id := int64(h.Sum64() & 0x7fffffffffffffff)
	if id == 0 {
		return 1
	}
	return id
}

func jsonList[T ~string](values []T) (string, error) {
	if values == nil {
		return "[]", nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(string(value)); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	data, err := json.Marshal(out)
	return string(data), err
}

func jsonObject(value map[string]any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	data, err := json.Marshal(value)
	return string(data), err
}

func unmarshalStringList(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(firstNonEmpty(raw, "[]")), &out); err != nil {
		return nil
	}
	return out
}

func unmarshalMap(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(firstNonEmpty(raw, "{}")), &out); err != nil {
		return nil
	}
	return out
}

func unmarshalModes(raw string) []speechcustomize.Mode {
	var values []string
	if err := json.Unmarshal([]byte(firstNonEmpty(raw, "[]")), &values); err != nil {
		return nil
	}
	modes := make([]speechcustomize.Mode, 0, len(values))
	for _, value := range values {
		if mode := speechcustomize.NormalizeMode(speechcustomize.Mode(value)); mode != "" {
			modes = append(modes, mode)
		}
	}
	return modes
}

func replacementModes(modes []speechcustomize.Mode) []string {
	out := make([]string, 0, len(modes))
	for _, mode := range modes {
		if normalized := speechcustomize.NormalizeMode(mode); normalized != "" {
			out = append(out, string(normalized))
		}
	}
	return out
}

func replacementHasMode(replacement speechcustomize.Replacement, mode speechcustomize.Mode) bool {
	if mode == "" {
		return true
	}
	for _, candidate := range replacement.Modes {
		if speechcustomize.NormalizeMode(candidate) == mode {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeCustomizationSource(source string) string {
	source = strings.TrimSpace(strings.ToLower(source))
	if source == "" {
		return userDictionarySettingsSource
	}
	return source
}

func isMissingCustomizationTable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "customization_") && (strings.Contains(msg, "no such table") || strings.Contains(msg, "does not exist"))
}
