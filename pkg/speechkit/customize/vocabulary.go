package customize

import "strings"

// WordSynonymModes is the mode set for aliases derived from a Word.
func WordSynonymModes() []Mode {
	return []Mode{ModeDictation, ModeAssist, ModeVoiceAgent}
}

// FoldVocabulary attaches synonym/substitution rewrites whose output matches a
// Word onto that Word's sounds_like list. Remaining replacements are extras
// (punctuation, regex, snippets, commands).
func FoldVocabulary(words []Word, replacements []Replacement) (folded []Word, extras []Replacement) {
	folded = MergeWords(words)
	byKey := map[string]int{}
	for i, word := range folded {
		byKey[WordIdentityKey(word)] = i
	}
	extras = make([]Replacement, 0, len(replacements))
	for _, replacement := range MergeReplacements(replacements) {
		if idx, ok := wordAliasIndex(folded, byKey, replacement); ok {
			folded[idx].SoundsLike = NormalizeAliasList(folded[idx].Term, append(append([]string{}, folded[idx].SoundsLike...), replacement.Match.Pattern))
			continue
		}
		extras = append(extras, replacement)
	}
	return folded, extras
}

// MaterializeVocabulary merges Words and rebuilds alias replacements from
// sounds_like. extras must already be non-alias rules; derived aliases that
// also appear in extras are dropped so Words stay the source of truth.
func MaterializeVocabulary(words []Word, extras []Replacement) (merged []Word, replacements []Replacement) {
	merged = MergeWords(words)
	derived := make([]Replacement, 0)
	for _, word := range merged {
		for _, alias := range word.SoundsLike {
			derived = append(derived, SynonymReplacement(word.Term, alias, word.Language, word.Source))
		}
	}
	kept := make([]Replacement, 0, len(extras))
	index := map[string]int{}
	for i, word := range merged {
		index[WordIdentityKey(word)] = i
	}
	for _, extra := range extras {
		if _, ok := wordAliasIndex(merged, index, extra); ok {
			continue
		}
		kept = append(kept, extra)
	}
	return merged, MergeReplacements(append(derived, kept...))
}

func wordAliasIndex(words []Word, byKey map[string]int, replacement Replacement) (int, bool) {
	kind := NormalizeKind(replacement.Kind)
	if kind != KindSynonym && kind != KindSubstitution {
		return 0, false
	}
	if NormalizeMatchType(replacement.Match.Type) == MatchRegex {
		return 0, false
	}
	output := strings.TrimSpace(replacement.Output.Text)
	pattern := strings.TrimSpace(replacement.Match.Pattern)
	if output == "" || pattern == "" {
		return 0, false
	}
	language := NormalizeLanguage(replacement.Language)
	if idx, ok := byKey[strings.ToLower(language+"\x00"+output)]; ok {
		return idx, true
	}
	if idx, ok := byKey[strings.ToLower("\x00"+output)]; ok {
		return idx, true
	}
	if language == "" {
		for i, word := range words {
			if strings.EqualFold(strings.TrimSpace(word.Term), output) {
				return i, true
			}
		}
	}
	return 0, false
}
