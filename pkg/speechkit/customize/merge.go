package customize

import (
	"sort"
	"strings"
)

// WordIdentityKey is the authoring identity for a Word: language + term.
func WordIdentityKey(word Word) string {
	return strings.ToLower(NormalizeLanguage(word.Language) + "\x00" + strings.TrimSpace(word.Term))
}

// ReplacementIdentityKey is the authoring identity for a Replacement.
// Output is intentionally not part of the key: many spoken forms may rewrite
// to the same canonical word.
func ReplacementIdentityKey(replacement Replacement) string {
	replacement = WithDefaultsReplacement(replacement)
	modes := make([]string, 0, len(replacement.Modes))
	for _, mode := range replacement.Modes {
		if normalized := NormalizeMode(mode); normalized != "" {
			modes = append(modes, string(normalized))
		}
	}
	sort.Strings(modes)
	return strings.ToLower(strings.Join([]string{
		string(replacement.Kind),
		replacement.Language,
		string(replacement.Stage),
		string(replacement.Match.Type),
		strings.TrimSpace(replacement.Match.Pattern),
		strings.Join(modes, ","),
	}, "\x00"))
}

// NormalizeAliasList trims, drops empties, drops the canonical term, and
// de-duplicates case-insensitively while keeping first-seen spelling.
func NormalizeAliasList(term string, aliases []string) []string {
	seen := map[string]struct{}{}
	if trimmed := strings.TrimSpace(term); trimmed != "" {
		seen[strings.ToLower(trimmed)] = struct{}{}
	}
	out := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		key := strings.ToLower(alias)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, alias)
	}
	return out
}

// MergeWords collapses duplicate terms in the same language. sounds_like and
// tags are unioned. Duplicate rows are a settings/migration defect, not a
// supported authoring state.
func MergeWords(words []Word) []Word {
	type slot struct {
		word  Word
		order int
	}
	byKey := map[string]*slot{}
	next := 0
	for _, word := range words {
		word.Term = strings.TrimSpace(word.Term)
		if word.Term == "" {
			continue
		}
		word = WithDefaultsWord(word)
		word.SoundsLike = NormalizeAliasList(word.Term, word.SoundsLike)
		word.Tags = NormalizeAliasList("", word.Tags)
		key := WordIdentityKey(word)
		existing, ok := byKey[key]
		if !ok {
			byKey[key] = &slot{word: word, order: next}
			next++
			continue
		}
		existing.word = mergeWordRecord(existing.word, word)
	}
	ordered := make([]*slot, 0, len(byKey))
	for _, item := range byKey {
		ordered = append(ordered, item)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].order < ordered[j].order
	})
	out := make([]Word, 0, len(ordered))
	for _, item := range ordered {
		out = append(out, item.word)
	}
	return out
}

// MergeReplacements collapses exact identity duplicates. Distinct match
// patterns that rewrite to the same output are kept.
func MergeReplacements(replacements []Replacement) []Replacement {
	type slot struct {
		replacement Replacement
		order       int
	}
	byKey := map[string]*slot{}
	next := 0
	for _, replacement := range replacements {
		replacement.Match.Pattern = strings.TrimSpace(replacement.Match.Pattern)
		if replacement.Match.Pattern == "" {
			continue
		}
		replacement = WithDefaultsReplacement(replacement)
		key := ReplacementIdentityKey(replacement)
		existing, ok := byKey[key]
		if !ok {
			byKey[key] = &slot{replacement: replacement, order: next}
			next++
			continue
		}
		existing.replacement = mergeReplacementRecord(existing.replacement, replacement)
	}
	ordered := make([]*slot, 0, len(byKey))
	for _, item := range byKey {
		ordered = append(ordered, item)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].order < ordered[j].order
	})
	out := make([]Replacement, 0, len(ordered))
	for _, item := range ordered {
		out = append(out, item.replacement)
	}
	return out
}

// SynonymReplacement builds the derived rewrite for a Word alias.
func SynonymReplacement(term, alias, language, source string) Replacement {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "settings"
	}
	return WithDefaultsReplacement(Replacement{
		Kind:     KindSynonym,
		Language: language,
		Source:   source,
		Enabled:  true,
		Modes:    []Mode{ModeDictation, ModeAssist},
		Stage:    StagePostSTT,
		Match: Match{
			Type:         MatchSpokenAlias,
			Pattern:      strings.TrimSpace(alias),
			WordBoundary: true,
		},
		Output: ReplacementOutput{Text: strings.TrimSpace(term)},
	})
}

func mergeWordRecord(base, extra Word) Word {
	out := base
	if !base.Enabled && extra.Enabled {
		out.Term = extra.Term
	}
	out.Enabled = base.Enabled || extra.Enabled
	if extra.Weight > out.Weight {
		out.Weight = extra.Weight
	}
	out.SoundsLike = NormalizeAliasList(out.Term, append(append([]string{}, base.SoundsLike...), extra.SoundsLike...))
	out.Tags = NormalizeAliasList("", append(append([]string{}, base.Tags...), extra.Tags...))
	out.UsageCount += extra.UsageCount
	if strings.TrimSpace(out.Source) == "" {
		out.Source = extra.Source
	}
	out.ID = ""
	return WithDefaultsWord(out)
}

func mergeReplacementRecord(base, extra Replacement) Replacement {
	out := base
	out.Enabled = base.Enabled || extra.Enabled
	if extra.Priority > out.Priority {
		out.Priority = extra.Priority
	}
	if strings.TrimSpace(out.Output.Text) == "" {
		out.Output.Text = extra.Output.Text
	}
	if strings.TrimSpace(out.Output.Intent) == "" {
		out.Output.Intent = extra.Output.Intent
	}
	if strings.TrimSpace(out.Output.Template) == "" {
		out.Output.Template = extra.Output.Template
	}
	out.UsageCount += extra.UsageCount
	if strings.TrimSpace(out.Source) == "" {
		out.Source = extra.Source
	}
	out.ID = ""
	return WithDefaultsReplacement(out)
}
