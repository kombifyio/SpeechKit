package store

import (
	"context"
	"hash/fnv"
	"strings"

	internalcustomize "github.com/kombifyio/SpeechKit/internal/customize"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

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
