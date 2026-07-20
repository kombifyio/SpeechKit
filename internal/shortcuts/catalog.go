// Package shortcuts implements pattern-matched intent shortcuts used by
// Assist Mode. The Registry holds two ordered lists — phrases (full
// utterances mapped to a fixed Intent) and fillers (locale-specific
// idle/filler words to strip) — and the resolver walks them to classify
// or normalize incoming transcripts before they reach the LLM path.
package shortcuts

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/language"
)

type Registry struct {
	phrases []registeredPhrase
	fillers []registeredFiller
	order   int
}

type registeredPhrase struct {
	intent        Intent
	locale        string
	value         string
	prefix        bool
	noSpacePrefix bool
	priority      int
	order         int
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
			intent:        lexicon.Intent,
			locale:        locale,
			value:         value,
			prefix:        phrase.Prefix,
			noSpacePrefix: phrase.NoSpacePrefix,
			priority:      phrase.Priority,
			order:         r.order,
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
	for _, lexicon := range homeAssistantLexicons {
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

	// Voice-Companion lexicons. Phase 0 ships DE+EN baseline coverage; FR/ES
	// are added in Phase 1 alongside the actual skill executors. Patterns
	// favour short, distinctive prefixes so the payload (timer duration,
	// city name, math expression, search topic) survives in
	// Resolution.Payload. See internal/shortcuts/resolver.go matchPhrase.
	{
		Intent:  IntentTime,
		Locale:  "en",
		Phrases: prefixPhrases("what time is it", "what time", "current time", "the time", "tell me the time"),
	},
	{
		Intent:  IntentTime,
		Locale:  "de",
		Phrases: prefixPhrases("wie spaet ist es", "wie spaet", "wieviel uhr ist es", "wieviel uhr", "uhrzeit", "die uhrzeit", "sag mir die uhrzeit"),
	},
	{
		Intent:  IntentDate,
		Locale:  "en",
		Phrases: prefixPhrases("what day is it", "what is today", "today's date", "what date is it", "what's the date"),
	},
	{
		Intent:  IntentDate,
		Locale:  "de",
		Phrases: prefixPhrases("welcher tag ist heute", "welcher tag", "welches datum ist heute", "welches datum", "datum", "der wievielte ist heute"),
	},
	{
		Intent:  IntentWeather,
		Locale:  "en",
		Phrases: prefixPhrases("what's the weather", "how is the weather", "weather forecast", "weather in", "forecast for", "weather"),
	},
	{
		Intent:  IntentWeather,
		Locale:  "de",
		Phrases: prefixPhrases("wie wird das wetter", "wie ist das wetter", "wetter in", "wetter fuer", "wettervorhersage", "wetter"),
	},
	{
		Intent:  IntentTimer,
		Locale:  "en",
		Phrases: prefixPhrases("set a timer for", "set timer for", "start a timer for", "set a timer", "timer for"),
	},
	{
		Intent:  IntentTimer,
		Locale:  "de",
		Phrases: prefixPhrases("stell einen timer auf", "stell einen timer fuer", "setze einen timer auf", "starte einen timer", "timer auf", "timer fuer"),
	},
	{
		Intent:  IntentReminder,
		Locale:  "en",
		Phrases: prefixPhrases("remind me to", "remind me at", "remind me about", "set a reminder to", "set a reminder for"),
	},
	{
		Intent:  IntentReminder,
		Locale:  "de",
		Phrases: prefixPhrases("erinnere mich an", "erinnere mich um", "erinnere mich morgen", "setze eine erinnerung", "stelle eine erinnerung"),
	},
	{
		Intent:  IntentMath,
		Locale:  "en",
		Phrases: prefixPhrases("what is", "calculate", "compute", "how much is", "what's"),
	},
	{
		Intent:  IntentMath,
		Locale:  "de",
		Phrases: prefixPhrases("was ist", "berechne", "rechne", "wie viel ist", "wieviel ist"),
	},
	{
		Intent:  IntentWikipedia,
		Locale:  "en",
		Phrases: prefixPhrases("tell me about", "who is", "who was", "what is wikipedia", "search for"),
	},
	{
		Intent:  IntentWikipedia,
		Locale:  "de",
		Phrases: prefixPhrases("erzaehl mir was ueber", "erzaehl mir ueber", "wer ist", "wer war", "was weisst du ueber", "suche nach"),
	},
	{
		Intent: IntentTemperature,
		Locale: "en",
		// Explicit "<value> <unit> to/in <unit>" phrasing only; "how many
		// fahrenheit is …" is intentionally omitted because the target unit
		// would live in the trigger, not the payload the skill parses.
		Phrases: prefixPhrases("convert", "how many degrees is", "how many degrees"),
	},
	{
		Intent:  IntentTemperature,
		Locale:  "de",
		Phrases: prefixPhrases("umrechne", "umrechnen", "wandle", "wandel", "konvertiere"),
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

type homeAssistantCatalogFile struct {
	Intent            Intent                     `json:"intent"`
	TranslationStatus string                     `json:"translation_status"`
	Lexicons          []homeAssistantLexiconFile `json:"lexicons"`
}

// homeAssistantLexiconFile deliberately has no Intent field. The catalog owns
// one exact intent at the root, so a nested intent is rejected as unknown JSON
// instead of being silently overwritten during projection.
type homeAssistantLexiconFile struct {
	Locale  string   `json:"locale"`
	Phrases []Phrase `json:"phrases"`
}

// The non-English Home Assistant phrases are registered intent-data
// proposals. Their presence is not human-review or Wave 2 localization
// evidence.
//
//go:embed catalogs/home_assistant.json
var homeAssistantCatalogFS embed.FS

var homeAssistantLexicons = mustLoadHomeAssistantLexicons()

func mustLoadHomeAssistantLexicons() []IntentLexicon {
	raw, err := homeAssistantCatalogFS.ReadFile("catalogs/home_assistant.json")
	if err != nil {
		panic(fmt.Sprintf("shortcuts: read Home Assistant catalog: %v", err))
	}
	lexicons, err := decodeHomeAssistantLexicons(raw)
	if err != nil {
		panic(fmt.Sprintf("shortcuts: decode Home Assistant catalog: %v", err))
	}
	return lexicons
}

func decodeHomeAssistantLexicons(raw []byte) ([]IntentLexicon, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var catalog homeAssistantCatalogFile
	if err := decoder.Decode(&catalog); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("catalog contains trailing JSON")
	}
	if catalog.Intent != IntentHomeAssistant {
		return nil, fmt.Errorf("catalog intent = %q", catalog.Intent)
	}
	if catalog.TranslationStatus != "proposal" {
		return nil, fmt.Errorf("catalog translation_status = %q", catalog.TranslationStatus)
	}

	requiredLocales := map[string]bool{
		"en": false, "de": false, "es": false, "zh-hans": false, "hi": false, "ar": false,
	}
	lexicons := make([]IntentLexicon, 0, len(catalog.Lexicons))
	for _, wireLexicon := range catalog.Lexicons {
		parsedLocale, err := language.Parse(wireLexicon.Locale)
		if err != nil || parsedLocale.String() != wireLexicon.Locale {
			return nil, fmt.Errorf("catalog locale %q is not canonical BCP-47", wireLexicon.Locale)
		}
		lexicon := IntentLexicon{
			Intent:  catalog.Intent,
			Locale:  wireLexicon.Locale,
			Phrases: wireLexicon.Phrases,
		}
		locale := normalizeLocaleKey(lexicon.Locale)
		seen, supported := requiredLocales[locale]
		if !supported {
			return nil, fmt.Errorf("unsupported catalog locale %q", lexicon.Locale)
		}
		if seen {
			return nil, fmt.Errorf("duplicate catalog locale %q", lexicon.Locale)
		}
		requiredLocales[locale] = true
		if len(lexicon.Phrases) == 0 {
			return nil, fmt.Errorf("catalog locale %q has no phrases", lexicon.Locale)
		}

		seenPhrases := make(map[string]struct{}, len(lexicon.Phrases))
		for phraseIndex := range lexicon.Phrases {
			phrase := &lexicon.Phrases[phraseIndex]
			normalized := normalize(phrase.Value)
			if normalized == "" {
				return nil, fmt.Errorf("catalog locale %q has an empty phrase", lexicon.Locale)
			}
			if _, duplicate := seenPhrases[normalized]; duplicate {
				return nil, fmt.Errorf("duplicate catalog phrase %q for locale %q", phrase.Value, lexicon.Locale)
			}
			seenPhrases[normalized] = struct{}{}
			if phrase.NoSpacePrefix && (!phrase.Prefix || locale != "zh-hans" || !containsHan(normalized)) {
				return nil, fmt.Errorf("no_space_prefix is restricted to prefixed Han phrases, got %q/%q", lexicon.Locale, phrase.Value)
			}
		}
		lexicons = append(lexicons, lexicon)
	}
	for locale, found := range requiredLocales {
		if !found {
			return nil, fmt.Errorf("catalog is missing locale %q", locale)
		}
	}
	return lexicons, nil
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

var defaultRegistry = buildDefaultRegistry()
