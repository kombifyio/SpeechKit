package transcription

import (
	"context"
	"regexp"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/store"
)

// VocabularyEntry pairs a spoken form with its canonical written form.
// The dictionary helpers consume slices of these.
type VocabularyEntry struct {
	Spoken    string
	Canonical string
}

// ParseVocabularyDictionary parses the user-facing newline-separated
// dictionary text. Each line is either a bare term ("Kombify") or a
// "spoken => canonical" rewrite rule. Empty lines and rules with empty
// sides are silently dropped.
func ParseVocabularyDictionary(raw string) []VocabularyEntry {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	entries := make([]VocabularyEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry := VocabularyEntry{}
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

// VocabularyEntriesFromStore loads enabled dictionary entries for the
// given language from the user-dictionary store. Nil store yields nil.
func VocabularyEntriesFromStore(ctx context.Context, dictionaryStore store.UserDictionaryStore, language string) ([]VocabularyEntry, error) {
	if dictionaryStore == nil {
		return nil, nil
	}
	storeEntries, err := dictionaryStore.ListUserDictionaryEntries(ctx, language)
	if err != nil {
		return nil, err
	}
	entries := make([]VocabularyEntry, 0, len(storeEntries))
	for _, entry := range storeEntries {
		if !entry.Enabled {
			continue
		}
		spoken := strings.TrimSpace(entry.Spoken)
		canonical := strings.TrimSpace(entry.Canonical)
		if spoken == "" || canonical == "" {
			continue
		}
		entries = append(entries, VocabularyEntry{
			Spoken:    spoken,
			Canonical: canonical,
		})
	}
	return entries, nil
}

// BuildVocabularyPrompt produces the STT-prompt hint string given a
// dictionary, for providers that accept a free-form bias prompt.
func BuildVocabularyPrompt(entries []VocabularyEntry) string {
	terms := canonicalVocabularyTerms(entries)
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these terms when transcribing: " + strings.Join(terms, ", ") + "."
}

// BuildVoiceAgentVocabularyHint produces the Voice-Agent system-prompt
// hint, which differs slightly from the STT prompt to bias both
// recognition and generation.
func BuildVoiceAgentVocabularyHint(entries []VocabularyEntry) string {
	terms := canonicalVocabularyTerms(entries)
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these names and product terms in recognition and responses: " + strings.Join(terms, ", ") + "."
}

func canonicalVocabularyTerms(entries []VocabularyEntry) []string {
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

// ApplyVocabularyCorrectionsWithMatches replaces spoken forms with
// canonical forms inside text (case-insensitive, word-boundary aware)
// and returns both the corrected text and the list of canonical terms
// that actually matched at least once.
func ApplyVocabularyCorrectionsWithMatches(text string, entries []VocabularyEntry) (string, []string) {
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

// SyncVocabularyDictionaryStore replaces the persisted user-dictionary
// entries for the given language with the entries parsed from the
// settings textarea. Returns nil if the feedback store does not implement
// UserDictionaryStore (legacy installs).
func SyncVocabularyDictionaryStore(ctx context.Context, feedbackStore store.Store, language, raw string) error {
	dictionaryStore := UserDictionaryStoreFromFeedbackStore(feedbackStore)
	if dictionaryStore == nil {
		return nil
	}
	entries := ParseVocabularyDictionary(raw)
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

// UserDictionaryStoreFromFeedbackStore narrows a store.Store to its
// UserDictionaryStore facet if available, nil otherwise.
func UserDictionaryStoreFromFeedbackStore(feedbackStore store.Store) store.UserDictionaryStore {
	dictionaryStore, ok := feedbackStore.(store.UserDictionaryStore)
	if !ok {
		return nil
	}
	return dictionaryStore
}
