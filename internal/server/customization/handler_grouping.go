//go:build linux

package customization

import (
	"strings"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

func groupWords(words []speechcustomize.Word) map[string][]speechcustomize.Word {
	out := map[string][]speechcustomize.Word{}
	for _, word := range words {
		language := speechcustomize.NormalizeLanguage(word.Language)
		out[language] = append(out[language], word)
	}
	return out
}

func groupReplacements(replacements []speechcustomize.Replacement) map[string][]speechcustomize.Replacement {
	out := map[string][]speechcustomize.Replacement{}
	for _, replacement := range replacements {
		language := speechcustomize.NormalizeLanguage(replacement.Language)
		out[language] = append(out[language], replacement)
	}
	return out
}

func groupLexicons(lexicons []speechcustomize.Lexicon) map[string][]speechcustomize.Lexicon {
	out := map[string][]speechcustomize.Lexicon{}
	for _, lexicon := range lexicons {
		language := speechcustomize.NormalizeLanguage(lexicon.Language)
		out[language] = append(out[language], lexicon)
	}
	return out
}

func groupRulesets(rulesets []speechcustomize.Ruleset) map[string][]speechcustomize.Ruleset {
	out := map[string][]speechcustomize.Ruleset{}
	for _, ruleset := range rulesets {
		language := speechcustomize.NormalizeLanguage(ruleset.Language)
		out[language] = append(out[language], ruleset)
	}
	return out
}

func withWordSource(words []speechcustomize.Word, source string) []speechcustomize.Word {
	out := append([]speechcustomize.Word(nil), words...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func withReplacementSource(replacements []speechcustomize.Replacement, source string) []speechcustomize.Replacement {
	out := append([]speechcustomize.Replacement(nil), replacements...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func withLexiconSource(lexicons []speechcustomize.Lexicon, source string) []speechcustomize.Lexicon {
	out := append([]speechcustomize.Lexicon(nil), lexicons...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func withRulesetSource(rulesets []speechcustomize.Ruleset, source string) []speechcustomize.Ruleset {
	out := append([]speechcustomize.Ruleset(nil), rulesets...)
	for i := range out {
		out[i].Source = source
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
