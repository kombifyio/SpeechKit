package shortcuts

import "sort"

type Registry struct {
	phrases []registeredPhrase
	fillers []registeredFiller
	order   int
}

type registeredPhrase struct {
	intent   Intent
	locale   string
	value    string
	prefix   bool
	priority int
	order    int
}

type registeredFiller struct {
	locale string
	value  string
	order  int
}

func NewRegistry() *Registry {
	return &Registry{}
}

func DefaultRegistry() *Registry {
	return defaultRegistry.clone()
}

func (r *Registry) RegisterLexicon(lexicon IntentLexicon) {
	if r == nil || lexicon.Intent == IntentNone {
		return
	}

	locale := normalizeLocaleKey(lexicon.Locale)
	for _, phrase := range lexicon.Phrases {
		value := normalize(phrase.Value)
		if value == "" {
			continue
		}
		r.phrases = append(r.phrases, registeredPhrase{
			intent:   lexicon.Intent,
			locale:   locale,
			value:    value,
			prefix:   phrase.Prefix,
			priority: phrase.Priority,
			order:    r.order,
		})
		r.order++
	}
}

func (r *Registry) RegisterLeadingFillers(locale string, fillers ...string) {
	if r == nil {
		return
	}

	normalizedLocale := normalizeLocaleKey(locale)
	for _, filler := range fillers {
		value := normalize(filler)
		if value == "" {
			continue
		}
		r.fillers = append(r.fillers, registeredFiller{
			locale: normalizedLocale,
			value:  value,
			order:  r.order,
		})
		r.order++
	}
}

func (r *Registry) clone() *Registry {
	if r == nil {
		return NewRegistry()
	}

	clone := &Registry{
		phrases: make([]registeredPhrase, len(r.phrases)),
		fillers: make([]registeredFiller, len(r.fillers)),
		order:   r.order,
	}
	copy(clone.phrases, r.phrases)
	copy(clone.fillers, r.fillers)
	return clone
}

func (r *Registry) orderedPhrases(locale string) []registeredPhrase {
	if r == nil {
		return nil
	}

	candidates := make([]registeredPhrase, 0, len(r.phrases))
	chain, allowAll := localeChain(locale)
	for _, phrase := range r.phrases {
		if allowAll || localeRank(phrase.locale, chain) >= 0 {
			candidates = append(candidates, phrase)
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]

		if !allowAll {
			leftRank := localeRank(left.locale, chain)
			rightRank := localeRank(right.locale, chain)
			if leftRank != rightRank {
				return leftRank < rightRank
			}
		}

		if left.priority != right.priority {
			return left.priority > right.priority
		}
		if len(left.value) != len(right.value) {
			return len(left.value) > len(right.value)
		}
		return left.order < right.order
	})

	return candidates
}

func (r *Registry) orderedFillers(locale string) []registeredFiller {
	if r == nil {
		return nil
	}

	candidates := make([]registeredFiller, 0, len(r.fillers))
	chain, allowAll := localeChain(locale)
	for _, filler := range r.fillers {
		if allowAll || localeRank(filler.locale, chain) >= 0 {
			candidates = append(candidates, filler)
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]

		if !allowAll {
			leftRank := localeRank(left.locale, chain)
			rightRank := localeRank(right.locale, chain)
			if leftRank != rightRank {
				return leftRank < rightRank
			}
		}

		if len(left.value) != len(right.value) {
			return len(left.value) > len(right.value)
		}
		return left.order < right.order
	})

	return candidates
}

func buildDefaultRegistry() *Registry {
	registry := NewRegistry()

	for _, lexicon := range defaultLexicons {
		registry.RegisterLexicon(lexicon)
	}
	for locale, fillers := range defaultLeadingFillers {
		registry.RegisterLeadingFillers(locale, fillers...)
	}

	return registry
}

func prefixPhrases(values ...string) []Phrase {
	phrases := make([]Phrase, 0, len(values))
	for _, value := range values {
		phrases = append(phrases, Phrase{
			Value:  value,
			Prefix: true,
		})
	}
	return phrases
}

var defaultLexicons = []IntentLexicon{
	{
		Intent:  IntentCopyLast,
		Locale:  "en",
		Phrases: prefixPhrases("copy last transcription", "copy last", "copy that", "copy it"),
	},
	{
		Intent:  IntentCopyLast,
		Locale:  "de",
		Phrases: prefixPhrases("kopiere das letzte", "letzte kopieren", "kopier das", "in die zwischenablage", "kopieren"),
	},
	{
		Intent:  IntentCopyLast,
		Locale:  "fr",
		Phrases: prefixPhrases("copier le dernier", "copie ca", "copier"),
	},
	{
		Intent:  IntentCopyLast,
		Locale:  "es",
		Phrases: prefixPhrases("copiar lo ultimo", "copiar eso", "copiar"),
	},
	{
		Intent:  IntentInsertLast,
		Locale:  "en",
		Phrases: prefixPhrases("insert last transcription", "insert last", "paste that", "insert that", "paste it"),
	},
	{
		Intent:  IntentInsertLast,
		Locale:  "de",
		Phrases: prefixPhrases("fuege das letzte ein", "letztes einfuegen", "einfuegen", "fueg das ein"),
	},
	{
		Intent:  IntentInsertLast,
		Locale:  "fr",
		Phrases: prefixPhrases("inserer le dernier", "coller ca", "inserer"),
	},
	{
		Intent:  IntentInsertLast,
		Locale:  "es",
		Phrases: prefixPhrases("insertar lo ultimo", "pegar eso", "insertar"),
	},
	{
		Intent:  IntentSummarize,
		Locale:  "en",
		Phrases: prefixPhrases("summarize this", "summarise this", "summarize", "summarise", "summary", "give me a summary", "sum it up"),
	},
	{
		Intent:  IntentSummarize,
		Locale:  "de",
		Phrases: prefixPhrases("fassen wir dies zusammen", "zusammenfassung", "zusammenfassen", "fass zusammen", "fass das zusammen", "kurz zusammenfassen", "mach eine zusammenfassung"),
	},
	{
		Intent:  IntentSummarize,
		Locale:  "fr",
		Phrases: prefixPhrases("resume ca", "resumer", "fais un resume", "resume"),
	},
	{
		Intent:  IntentSummarize,
		Locale:  "es",
		Phrases: prefixPhrases("resumir esto", "resumir", "haz un resumen", "resumen"),
	},
	{
		Intent:  IntentQuickNote,
		Locale:  "en",
		Phrases: prefixPhrases("quick note", "note", "take a note", "save note"),
	},
	{
		Intent:  IntentQuickNote,
		Locale:  "de",
		Phrases: prefixPhrases("notiz", "schnelle notiz", "merke dir", "notiere"),
	},
	{
		Intent:  IntentQuickNote,
		Locale:  "fr",
		Phrases: prefixPhrases("note rapide", "prends une note", "noter"),
	},
	{
		Intent:  IntentQuickNote,
		Locale:  "es",
		Phrases: prefixPhrases("nota rapida", "toma nota", "anotar"),
	},
}

var defaultLeadingFillers = map[string][]string{
	"en": {
		"please",
		"could you please",
		"could you",
		"would you please",
		"would you",
		"can you please",
		"can you",
	},
	"de": {
		"bitte",
		"kannst du bitte",
		"kannst du",
		"koenntest du bitte",
		"koenntest du",
		"wuerdest du bitte",
	},
	"fr": {
		"s'il te plait",
		"s'il vous plait",
		"est-ce que tu peux",
		"peux-tu",
	},
	"es": {
		"por favor",
		"puedes",
		"podrias",
	},
}

var defaultRegistry = buildDefaultRegistry()
