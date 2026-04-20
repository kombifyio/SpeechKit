package main

import (
	"context"
	"regexp"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/store"
)

type vocabularyEntry struct {
	Spoken    string
	Canonical string
}

func parseVocabularyDictionary(raw string) []vocabularyEntry {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	entries := make([]vocabularyEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := vocabularyEntry{}
		if strings.Contains(line, "=>") {
			parts := strings.SplitN(line, "=>", 2)
			entry.Spoken = strings.TrimSpace(parts[0])
			entry.Canonical = strings.TrimSpace(parts[1])
		} else {
			entry.Spoken = line
			entry.Canonical = line
		}
		if entry.Spoken == "" || entry.Canonical == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func buildVocabularyPrompt(entries []vocabularyEntry) string {
	terms := canonicalVocabularyTerms(entries)
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these terms when transcribing: " + strings.Join(terms, ", ") + "."
}

func buildVoiceAgentVocabularyHint(entries []vocabularyEntry) string {
	terms := canonicalVocabularyTerms(entries)
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these names and product terms in recognition and responses: " + strings.Join(terms, ", ") + "."
}

func canonicalVocabularyTerms(entries []vocabularyEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	terms := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		term := entry.Canonical
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func applyVocabularyCorrectionsWithMatches(text string, entries []vocabularyEntry) (string, []string) {
	normalized := text
	matchedTerms := make([]string, 0)
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if strings.EqualFold(entry.Spoken, entry.Canonical) {
			continue
		}
		pattern := `(?i)\b` + regexp.QuoteMeta(entry.Spoken) + `\b`
		re := regexp.MustCompile(pattern)
		if re.MatchString(normalized) {
			key := strings.ToLower(entry.Canonical)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				matchedTerms = append(matchedTerms, entry.Canonical)
			}
		}
		normalized = re.ReplaceAllString(normalized, entry.Canonical)
	}
	return normalized, matchedTerms
}

func syncVocabularyDictionaryStore(ctx context.Context, feedbackStore store.Store, language, raw string) error {
	dictionaryStore := userDictionaryStoreFromFeedbackStore(feedbackStore)
	if dictionaryStore == nil {
		return nil
	}
	entries := parseVocabularyDictionary(raw)
	storeEntries := make([]store.UserDictionaryEntry, 0, len(entries))
	for _, entry := range entries {
		storeEntries = append(storeEntries, store.UserDictionaryEntry{
			Spoken:    entry.Spoken,
			Canonical: entry.Canonical,
			Language:  language,
			Source:    "settings",
		})
	}
	return dictionaryStore.ReplaceUserDictionaryEntries(ctx, language, storeEntries)
}

func userDictionaryStoreFromFeedbackStore(feedbackStore store.Store) store.UserDictionaryStore {
	dictionaryStore, ok := feedbackStore.(store.UserDictionaryStore)
	if !ok {
		return nil
	}
	return dictionaryStore
}
