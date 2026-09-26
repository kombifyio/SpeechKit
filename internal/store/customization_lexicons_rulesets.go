package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

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
