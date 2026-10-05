// Package customize defines Words, Replacements and shared recognition hints.
package customize

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// RecognitionHints carries dictionary-derived hints separately from explicit
// request hints, so vocabulary_bias=false can suppress only the dictionary.
type RecognitionHints struct {
	Prompt   string   `json:"prompt,omitempty"`
	Keyterms []string `json:"keyterms,omitempty"`
}

// BuildRecognitionHints renders enabled Words for batch and streaming recognition.
// Provider adapters choose prompt or native keyterms according to their manifest.
func BuildRecognitionHints(words []Word) RecognitionHints {
	return RecognitionHints{Prompt: BuildPrompt(words), Keyterms: BuildKeyterms(words)}
}

// ProviderBias describes vocabulary hints and their supported provider mappings.
type ProviderBias struct {
	Prompt     string
	Keyterms   []string
	ByProvider map[string]provideropts.Values
	Preview    []ProviderBiasPreview
}

// ProviderBiasPreview describes a provider's vocabulary bias strategy.
type ProviderBiasPreview struct {
	Provider string
	Modality string
	Strategy string
	Native   bool
	Keyterms []string
	Prompt   string
}

// BuildPrompt renders enabled canonical terms as a transcript-style prompt.
func BuildPrompt(words []Word) string {
	terms := CanonicalTerms(words)
	if len(terms) == 0 {
		return ""
	}
	// Whisper-family models treat `prompt` as previous-transcript style, not as
	// an instruction. A comma-separated English list ("Prefer these terms: A, B.")
	// makes German recordings copy that list punctuation.
	return strings.Join(terms, " ")
}

// BuildKeyterms returns enabled canonical terms and their pronunciation aliases.
func BuildKeyterms(words []Word) []string {
	terms := CanonicalTerms(words)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, term)
	}
	for _, word := range words {
		if !word.Enabled {
			continue
		}
		for _, alias := range NormalizeAliasList(word.Term, word.SoundsLike) {
			key := strings.ToLower(alias)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, alias)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildVoiceAgentHint renders enabled terms for recognition and responses.
func BuildVoiceAgentHint(words []Word) string {
	terms := CanonicalTerms(words)
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these names and product terms in recognition and responses: " + strings.Join(terms, ", ") + "."
}

// BuildProviderBias describes the STT mappings available for enabled words.
func BuildProviderBias(words []Word) ProviderBias {
	return BuildProviderBiasForModality(words, provideropts.ModalitySTT)
}

// BuildProviderBiasForModality describes vocabulary mappings for a modality.
func BuildProviderBiasForModality(words []Word, modality string) ProviderBias {
	modality = strings.TrimSpace(modality)
	if modality == "" {
		modality = provideropts.ModalitySTT
	}
	keyterms := BuildKeyterms(words)
	prompt := BuildPrompt(words)
	bias := ProviderBias{
		Prompt:     prompt,
		Keyterms:   keyterms,
		ByProvider: map[string]provideropts.Values{},
	}
	if len(keyterms) == 0 && prompt == "" {
		return bias
	}
	for _, manifest := range provideropts.DefaultManifests() {
		if manifest.Modality != modality {
			continue
		}
		support := manifest.SupportByID()
		values := provideropts.Values{}
		strategy := ""
		if opt, ok := support[provideropts.OptionKeyterms]; ok && optionCanCarryBias(opt) && len(keyterms) > 0 {
			if vocab, ok := support[provideropts.OptionVocabularyBias]; !ok || optionCanCarryBias(vocab) {
				values[provideropts.OptionVocabularyBias] = true
			}
			values[provideropts.OptionKeyterms] = append([]string(nil), keyterms...)
			strategy = string(opt.Status)
		} else if opt, ok := support[provideropts.OptionPromptHint]; ok && optionCanCarryBias(opt) && prompt != "" {
			if vocab, ok := support[provideropts.OptionVocabularyBias]; ok && optionCanCarryBias(vocab) {
				values[provideropts.OptionVocabularyBias] = true
			}
			values[provideropts.OptionPromptHint] = prompt
			strategy = string(opt.Status)
		}
		if len(values) == 0 {
			continue
		}
		bias.ByProvider[manifest.Provider] = values
		bias.Preview = append(bias.Preview, ProviderBiasPreview{
			Provider: manifest.Provider,
			Modality: manifest.Modality,
			Strategy: strategy,
			Native:   strategy == string(provideropts.SupportNative),
			Keyterms: append([]string(nil), keyterms...),
			Prompt:   prompt,
		})
	}
	return bias
}

// CanonicalTerms returns distinct enabled canonical terms in input order.
func CanonicalTerms(words []Word) []string {
	terms := make([]string, 0, len(words))
	seen := map[string]struct{}{}
	for _, word := range words {
		if !word.Enabled {
			continue
		}
		term := strings.TrimSpace(word.Term)
		if term == "" {
			continue
		}
		key := strings.ToLower(term)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		terms = append(terms, term)
	}
	return terms
}

func optionCanCarryBias(opt provideropts.OptionSupport) bool {
	return opt.Status != "" && opt.Status != provideropts.SupportUnsupported
}

// DictionaryEntry is the compatibility projection of a stored vocabulary rule.
type DictionaryEntry struct {
	Spoken     string
	Canonical  string
	Language   string
	Source     string
	Enabled    bool
	UsageCount int
}

// WordsFromDictionary converts enabled dictionary entries into framework words.
func WordsFromDictionary(entries []DictionaryEntry) []Word {
	words := make([]Word, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		canonical := strings.TrimSpace(entry.Canonical)
		if canonical == "" {
			continue
		}
		language := NormalizeLanguage(entry.Language)
		key := strings.ToLower(language + "\x00" + canonical)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		word := WithDefaultsWord(Word{
			Term:       canonical,
			Language:   language,
			Source:     dictionarySource(entry.Source),
			Enabled:    true,
			UsageCount: entry.UsageCount,
		})
		words = append(words, word)
	}
	return words
}

func dictionarySource(source string) string {
	if strings.TrimSpace(source) != "" {
		return source
	}
	return "settings"
}

// ParseDictionary reads newline-separated terms or spoken => canonical rules.
// Empty lines and rules with an empty side are discarded.
func ParseDictionary(raw string) []DictionaryEntry {
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\r", "\n")
	var out []DictionaryEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		spoken, canonical, rule := strings.Cut(line, "=>")
		if !rule {
			canonical = spoken
		}
		spoken, canonical = strings.TrimSpace(spoken), strings.TrimSpace(canonical)
		if spoken != "" && canonical != "" {
			out = append(out, DictionaryEntry{Spoken: spoken, Canonical: canonical, Enabled: true, Source: "settings"})
		}
	}
	return out
}
