//go:build linux

package customization

import (
	"context"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/store"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func importPack(ctx context.Context, s store.CustomizationStore, pack speechcustomize.Pack, source string) error {
	for language, words := range groupWords(pack.Words) {
		if err := replaceWords(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withWordSource(words, source)); err != nil {
			return err
		}
	}
	for language, replacements := range groupReplacements(pack.Replacements) {
		if err := replaceReplacements(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withReplacementSource(replacements, source)); err != nil {
			return err
		}
	}
	for language, lexicons := range groupLexicons(pack.Lexicons) {
		if err := replaceLexicons(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withLexiconSource(lexicons, source)); err != nil {
			return err
		}
	}
	for language, rulesets := range groupRulesets(pack.Rulesets) {
		if err := replaceRulesets(ctx, s, store.CustomizationReplaceOpts{Language: language, Source: source}, withRulesetSource(rulesets, source)); err != nil {
			return err
		}
	}
	return nil
}

func replaceVocabulary(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, words []speechcustomize.Word, extras []speechcustomize.Replacement) error {
	if vocabStore, ok := s.(store.CustomizationVocabularyStore); ok {
		return vocabStore.ReplaceVocabularyWithOptions(ctx, opts, words, extras)
	}
	merged, replacements := speechcustomize.MaterializeVocabulary(words, extras)
	if err := replaceWords(ctx, s, opts, merged); err != nil {
		return err
	}
	return replaceReplacements(ctx, s, opts, replacements)
}

func replaceWords(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, words []speechcustomize.Word) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceWordsWithOptions(ctx, opts, words)
	}
	return s.ReplaceWords(ctx, opts.Language, words)
}

func replaceReplacements(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, replacements []speechcustomize.Replacement) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceReplacementsWithOptions(ctx, opts, replacements)
	}
	return s.ReplaceReplacements(ctx, opts.Language, replacements)
}

func replaceLexicons(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, lexicons []speechcustomize.Lexicon) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceLexiconsWithOptions(ctx, opts, lexicons)
	}
	return s.ReplaceLexicons(ctx, opts.Language, lexicons)
}

func replaceRulesets(ctx context.Context, s store.CustomizationStore, opts store.CustomizationReplaceOpts, rulesets []speechcustomize.Ruleset) error {
	if sourceStore, ok := s.(store.CustomizationSourceStore); ok {
		return sourceStore.ReplaceRulesetsWithOptions(ctx, opts, rulesets)
	}
	return s.ReplaceRulesets(ctx, opts.Language, rulesets)
}

func packSource(pack speechcustomize.Pack) string {
	key := firstNonEmpty(pack.ID, pack.Name, "customization-pack")
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(key)), "pack:") {
		return key
	}
	return "pack:" + strings.TrimPrefix(speechcustomize.StableID("pack", key), "pack_")
}
