package templates

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
)

const (
	StandardPunctuationID = "standard-punctuation-de-en"
	DeveloperID           = "developer-de-en"
	VersionV1             = "v1"
)

type Template struct {
	ID               string                 `json:"id"`
	Version          string                 `json:"version"`
	Name             string                 `json:"name"`
	Description      string                 `json:"description,omitempty"`
	Source           string                 `json:"source"`
	Languages        []string               `json:"languages,omitempty"`
	Modes            []speechcustomize.Mode `json:"modes,omitempty"`
	BuiltIn          bool                   `json:"builtIn"`
	DefaultActive    bool                   `json:"defaultActive,omitempty"`
	WordCount        int                    `json:"wordCount"`
	ReplacementCount int                    `json:"replacementCount"`
}

type templateDefinition struct {
	meta    Template
	factory func() speechcustomize.Pack
}

func DefaultActiveTemplateIDs() []string {
	return []string{StandardPunctuationID}
}

func NormalizeActiveTemplateIDs(ids []string) []string {
	if len(ids) == 0 {
		return DefaultActiveTemplateIDs()
	}
	normalized := NormalizeTemplateIDs(ids)
	if len(normalized) == 0 {
		return DefaultActiveTemplateIDs()
	}
	return normalized
}

func NormalizeTemplateIDs(ids []string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(strings.ToLower(id))
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	return normalized
}

func ListTemplates() []Template {
	definitions := builtinDefinitions()
	templates := make([]Template, 0, len(definitions))
	for _, definition := range definitions {
		meta := definition.meta
		pack := definition.factory()
		meta.WordCount = len(pack.Words)
		meta.ReplacementCount = len(pack.Replacements)
		templates = append(templates, meta)
	}
	sort.SliceStable(templates, func(i, j int) bool {
		return templates[i].ID < templates[j].ID
	})
	return templates
}

func ResolveTemplatePack(id, version string) (speechcustomize.Pack, Template, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	version = strings.TrimSpace(strings.ToLower(version))
	definition, ok := builtinDefinitions()[id]
	if !ok {
		return speechcustomize.Pack{}, Template{}, false
	}
	if version != "" && version != strings.ToLower(definition.meta.Version) {
		return speechcustomize.Pack{}, Template{}, false
	}
	pack := definition.factory()
	meta := definition.meta
	meta.WordCount = len(pack.Words)
	meta.ReplacementCount = len(pack.Replacements)
	return pack, meta, true
}

func ActiveBuiltinPacks(ids []string) []speechcustomize.Pack {
	ids = NormalizeActiveTemplateIDs(ids)
	packs := make([]speechcustomize.Pack, 0, len(ids))
	for _, id := range ids {
		pack, _, ok := ResolveTemplatePack(id, "")
		if ok {
			packs = append(packs, pack)
		}
	}
	return packs
}

func builtinDefinitions() map[string]templateDefinition {
	return map[string]templateDefinition{
		StandardPunctuationID: {
			meta: Template{
				ID:            StandardPunctuationID,
				Version:       VersionV1,
				Name:          "Standard punctuation DE/EN",
				Description:   "Provider-neutral punctuation, numeric separator, spoken-number, and filler-word rules for German and English dictation.",
				Source:        source(StandardPunctuationID, VersionV1),
				Languages:     []string{"de", "en"},
				Modes:         []speechcustomize.Mode{speechcustomize.ModeDictation, speechcustomize.ModeAssist},
				BuiltIn:       true,
				DefaultActive: true,
			},
			factory: standardPunctuationPack,
		},
		DeveloperID: {
			meta: Template{
				ID:          DeveloperID,
				Version:     VersionV1,
				Name:        "Developer DE/EN",
				Description: "Technical vocabulary and developer-oriented spoken replacements for code, APIs, CLIs and cloud platforms.",
				Source:      source(DeveloperID, VersionV1),
				Languages:   []string{"de", "en"},
				Modes:       []speechcustomize.Mode{speechcustomize.ModeDictation, speechcustomize.ModeAssist, speechcustomize.ModeVoiceAgent},
				BuiltIn:     true,
			},
			factory: developerPack,
		},
	}
}

type spokenDigit struct {
	spoken string
	digit  string
}

func standardPunctuationPack() speechcustomize.Pack {
	templateID := StandardPunctuationID
	version := VersionV1
	tag := templateTag(templateID)
	replacements := []speechcustomize.Replacement{
		regexReplacement(templateID, version, "de.numeric-dot", "de", `(^|[^\pL\pN_])([0-9]+(?:\.[0-9]+)*)\s+(?:punkt|dot)\s+([0-9]+)([^\pL\pN_]|$)`, `${1}${2}.${3}${4}`, 110, tag),
		regexReplacement(templateID, version, "de.numeric-comma", "de", `(^|[^\pL\pN_])([0-9]+)\s+komma\s+([0-9]+)([^\pL\pN_]|$)`, `${1}${2},${3}${4}`, 100, tag),
		regexReplacement(templateID, version, "en.numeric-point", "en", `(^|[^\pL\pN_])([0-9]+(?:\.[0-9]+)*)\s+(?:point|dot)\s+([0-9]+)([^\pL\pN_]|$)`, `${1}${2}.${3}${4}`, 110, tag),
		regexReplacement(templateID, version, "en.numeric-comma", "en", `(^|[^\pL\pN_])([0-9]+)\s+comma\s+([0-9]+)([^\pL\pN_]|$)`, `${1}${2},${3}${4}`, 90, tag),
		regexReplacement(templateID, version, "de.clear-slash", "de", `(^|[^\pL\pN_])([A-Za-z0-9]+)\s+(?:slash|schraegstrich|schrägstrich)\s+([A-Za-z0-9]+)([^\pL\pN_]|$)`, `${1}${2}/${3}${4}`, 80, tag),
		regexReplacement(templateID, version, "de.clear-dash", "de", `(^|[^\pL\pN_])([A-Za-z0-9]+)\s+(?:dash|bindestrich)\s+([A-Za-z0-9]+)([^\pL\pN_]|$)`, `${1}${2}-${3}${4}`, 80, tag),
		regexReplacement(templateID, version, "de.clear-colon", "de", `(^|[^\pL\pN_])([A-Za-z0-9]+)\s+(?:colon|doppelpunkt)\s+([A-Za-z0-9]+)([^\pL\pN_]|$)`, `${1}${2}:${3}${4}`, 80, tag),
		regexReplacement(templateID, version, "en.clear-slash", "en", `(^|[^\pL\pN_])([A-Za-z0-9]+)\s+slash\s+([A-Za-z0-9]+)([^\pL\pN_]|$)`, `${1}${2}/${3}${4}`, 80, tag),
		regexReplacement(templateID, version, "en.clear-dash", "en", `(^|[^\pL\pN_])([A-Za-z0-9]+)\s+dash\s+([A-Za-z0-9]+)([^\pL\pN_]|$)`, `${1}${2}-${3}${4}`, 80, tag),
		regexReplacement(templateID, version, "en.clear-colon", "en", `(^|[^\pL\pN_])([A-Za-z0-9]+)\s+colon\s+([A-Za-z0-9]+)([^\pL\pN_]|$)`, `${1}${2}:${3}${4}`, 80, tag),
		regexReplacement(templateID, version, "filler.leading", "", `^(?:äh|ähm|aeh|aehm)[,\s]+`, "", 50, tag),
		regexReplacement(templateID, version, "filler.inline", "", `\s+(?:äh|ähm|aeh|aehm)([^\pL\pN_]|$)`, `${1}`, 50, tag),
	}
	replacements = append(replacements, numberWordDotReplacements(templateID, version, "de", []string{"punkt", "dot"}, germanDigits(), tag)...)
	replacements = append(replacements, numberWordDotReplacements(templateID, version, "en", []string{"point", "dot"}, englishDigits(), tag)...)
	replacements = append(replacements, numberWordJoinReplacements(templateID, version, "de", "von", " von ", germanDigits(), tag, 100)...)
	replacements = append(replacements, numberWordJoinReplacements(templateID, version, "en", "of", " of ", englishDigits(), tag, 100)...)
	return speechcustomize.Pack{
		SchemaVersion: speechcustomize.PackSchemaVersion,
		ID:            templateID,
		Name:          "Standard punctuation DE/EN",
		Description:   "Native SpeechKit punctuation, spoken-number, and filler-word defaults.",
		Replacements:  replacements,
	}
}

func developerPack() speechcustomize.Pack {
	templateID := DeveloperID
	version := VersionV1
	tag := templateTag(templateID)
	words := []speechcustomize.Word{
		word(templateID, version, "api", "API", "", tag, "a p i"),
		word(templateID, version, "sdk", "SDK", "", tag, "s d k"),
		word(templateID, version, "json", "JSON", "", tag, "j son", "jason"),
		word(templateID, version, "yaml", "YAML", "", tag, "yammel", "yamel"),
		word(templateID, version, "github", "GitHub", "", tag, "git hub"),
		word(templateID, version, "typescript", "TypeScript", "", tag, "type script"),
		word(templateID, version, "powershell", "PowerShell", "", tag, "power shell"),
		word(templateID, version, "wails", "Wails", "", tag),
		word(templateID, version, "auth0", "Auth0", "", tag, "auth zero"),
		word(templateID, version, "cloudflare", "Cloudflare", "", tag, "cloud flare"),
		word(templateID, version, "websocket", "WebSocket", "", tag, "web socket"),
		word(templateID, version, "oauth", "OAuth", "", tag, "o auth"),
		word(templateID, version, "jwt", "JWT", "", tag, "j w t"),
		word(templateID, version, "rest", "REST", "", tag),
		word(templateID, version, "http", "HTTP", "", tag, "h t t p"),
		word(templateID, version, "https", "HTTPS", "", tag, "h t t p s"),
		word(templateID, version, "cli", "CLI", "", tag, "c l i"),
		word(templateID, version, "mcp", "MCP", "", tag, "m c p"),
		word(templateID, version, "vad", "VAD", "", tag, "v a d"),
		word(templateID, version, "stt", "STT", "", tag, "s t t"),
		word(templateID, version, "tts", "TTS", "", tag, "t t s"),
		word(templateID, version, "wasapi", "WASAPI", "", tag, "w a s a p i"),
	}
	replacements := []speechcustomize.Replacement{
		aliasReplacement(templateID, version, "api", "a p i", "API", 100, tag),
		aliasReplacement(templateID, version, "sdk", "s d k", "SDK", 100, tag),
		aliasReplacement(templateID, version, "json-j-son", "j son", "JSON", 100, tag),
		aliasReplacement(templateID, version, "json-jason", "jason", "JSON", 100, tag),
		aliasReplacement(templateID, version, "yaml", "yammel", "YAML", 100, tag),
		aliasReplacement(templateID, version, "github", "git hub", "GitHub", 100, tag),
		aliasReplacement(templateID, version, "typescript", "type script", "TypeScript", 100, tag),
		aliasReplacement(templateID, version, "powershell", "power shell", "PowerShell", 100, tag),
		aliasReplacement(templateID, version, "auth0", "auth zero", "Auth0", 100, tag),
		aliasReplacement(templateID, version, "cloudflare", "cloud flare", "Cloudflare", 100, tag),
		aliasReplacement(templateID, version, "websocket", "web socket", "WebSocket", 100, tag),
		aliasReplacement(templateID, version, "oauth", "o auth", "OAuth", 100, tag),
		regexReplacement(templateID, version, "clear-dot", "", `(^|[^\pL\pN_])([A-Za-z0-9_]+)\s+(?:dot|punkt)\s+([A-Za-z0-9_]+)([^\pL\pN_]|$)`, `${1}${2}.${3}${4}`, 90, tag),
		regexReplacement(templateID, version, "clear-slash", "", `(^|[^\pL\pN_])([A-Za-z0-9_.-]+)\s+(?:slash|schraegstrich|schrägstrich)\s+([A-Za-z0-9_.-]+)([^\pL\pN_]|$)`, `${1}${2}/${3}${4}`, 90, tag),
		regexReplacement(templateID, version, "clear-underscore", "", `(^|[^\pL\pN_])([A-Za-z0-9.-]+)\s+(?:underscore|unterstrich)\s+([A-Za-z0-9.-]+)([^\pL\pN_]|$)`, `${1}${2}_${3}${4}`, 90, tag),
		regexReplacement(templateID, version, "clear-dash", "", `(^|[^\pL\pN_])([A-Za-z0-9_.]+)\s+(?:dash|bindestrich)\s+([A-Za-z0-9_.]+)([^\pL\pN_]|$)`, `${1}${2}-${3}${4}`, 90, tag),
		regexReplacement(templateID, version, "clear-arrow", "", `(^|[^\pL\pN_])([A-Za-z0-9_.-]+)\s+(?:arrow|pfeil)\s+([A-Za-z0-9_.-]+)([^\pL\pN_]|$)`, `${1}${2} -> ${3}${4}`, 80, tag),
		regexReplacement(templateID, version, "clear-equals", "", `(^|[^\pL\pN_])([A-Za-z0-9_.-]+)\s+(?:equals|gleich)\s+([A-Za-z0-9_.-]+)([^\pL\pN_]|$)`, `${1}${2} = ${3}${4}`, 80, tag),
	}
	return speechcustomize.Pack{
		SchemaVersion: speechcustomize.PackSchemaVersion,
		ID:            templateID,
		Name:          "Developer DE/EN",
		Description:   "Native SpeechKit developer vocabulary and replacements.",
		Words:         words,
		Replacements:  replacements,
	}
}

func numberWordJoinReplacements(templateID, version, language, joinWord, outputJoin string, digits []spokenDigit, tag string, priority int) []speechcustomize.Replacement {
	joinPattern := regexp.QuoteMeta(joinWord)
	replacements := make([]speechcustomize.Replacement, 0, len(digits)*len(digits)+len(digits)*2)
	for _, left := range digits {
		for _, right := range digits {
			pattern := fmt.Sprintf(`(^|[^\pL\pN_])%s\s+%s\s+%s([^\pL\pN_]|$)`, regexp.QuoteMeta(left.spoken), joinPattern, regexp.QuoteMeta(right.spoken))
			replacements = append(replacements, regexReplacement(templateID, version, fmt.Sprintf("%s.word-join.%s.%s.%s", language, joinWord, left.spoken, right.spoken), language, pattern, "${1}"+left.digit+outputJoin+right.digit+"${2}", priority, tag))
		}
		pattern := fmt.Sprintf(`(^|[^\pL\pN_])([0-9]+)\s+%s\s+%s([^\pL\pN_]|$)`, joinPattern, regexp.QuoteMeta(left.spoken))
		replacements = append(replacements, regexReplacement(templateID, version, fmt.Sprintf("%s.numeric-join-word.%s.%s", language, joinWord, left.spoken), language, pattern, "${1}${2}"+outputJoin+left.digit+"${3}", priority, tag))
		pattern = fmt.Sprintf(`(^|[^\pL\pN_])%s\s+%s\s+([0-9]+)([^\pL\pN_]|$)`, regexp.QuoteMeta(left.spoken), joinPattern)
		replacements = append(replacements, regexReplacement(templateID, version, fmt.Sprintf("%s.word-join-numeric.%s.%s", language, joinWord, left.spoken), language, pattern, "${1}"+left.digit+outputJoin+"${2}${3}", priority, tag))
	}
	return replacements
}

func numberWordDotReplacements(templateID, version, language string, separators []string, digits []spokenDigit, tag string) []speechcustomize.Replacement {
	separatorPattern := "(?:" + strings.Join(quotedAlternatives(separators), "|") + ")"
	replacements := make([]speechcustomize.Replacement, 0, len(digits)*len(digits)+len(digits)*2)
	for _, left := range digits {
		for _, right := range digits {
			pattern := fmt.Sprintf(`(^|[^\pL\pN_])%s\s+%s\s+%s([^\pL\pN_]|$)`, regexp.QuoteMeta(left.spoken), separatorPattern, regexp.QuoteMeta(right.spoken))
			replacements = append(replacements, regexReplacement(templateID, version, fmt.Sprintf("%s.word-dot.%s.%s", language, left.spoken, right.spoken), language, pattern, "${1}"+left.digit+"."+right.digit+"${2}", 130, tag))
		}
	}
	for _, digit := range digits {
		pattern := fmt.Sprintf(`(^|[^\pL\pN_])([0-9]+(?:\.[0-9]+)*)\s+%s\s+%s([^\pL\pN_]|$)`, separatorPattern, regexp.QuoteMeta(digit.spoken))
		replacements = append(replacements, regexReplacement(templateID, version, fmt.Sprintf("%s.numeric-dot-word.%s", language, digit.spoken), language, pattern, "${1}${2}."+digit.digit+"${3}", 120, tag))

		pattern = fmt.Sprintf(`(^|[^\pL\pN_])%s\s+%s\s+([0-9]+)([^\pL\pN_]|$)`, regexp.QuoteMeta(digit.spoken), separatorPattern)
		replacements = append(replacements, regexReplacement(templateID, version, fmt.Sprintf("%s.word-dot-numeric.%s", language, digit.spoken), language, pattern, "${1}"+digit.digit+".${2}${3}", 120, tag))
	}
	return replacements
}

func quotedAlternatives(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, regexp.QuoteMeta(value))
	}
	return out
}

func germanDigits() []spokenDigit {
	return []spokenDigit{
		{spoken: "null", digit: "0"},
		{spoken: "eins", digit: "1"},
		{spoken: "ein", digit: "1"},
		{spoken: "eine", digit: "1"},
		{spoken: "zwei", digit: "2"},
		{spoken: "drei", digit: "3"},
		{spoken: "vier", digit: "4"},
		{spoken: "fünf", digit: "5"},
		{spoken: "fuenf", digit: "5"},
		{spoken: "sechs", digit: "6"},
		{spoken: "sieben", digit: "7"},
		{spoken: "acht", digit: "8"},
		{spoken: "neun", digit: "9"},
	}
}

func englishDigits() []spokenDigit {
	return []spokenDigit{
		{spoken: "zero", digit: "0"},
		{spoken: "one", digit: "1"},
		{spoken: "two", digit: "2"},
		{spoken: "three", digit: "3"},
		{spoken: "four", digit: "4"},
		{spoken: "five", digit: "5"},
		{spoken: "six", digit: "6"},
		{spoken: "seven", digit: "7"},
		{spoken: "eight", digit: "8"},
		{spoken: "nine", digit: "9"},
	}
}

func word(templateID, version, suffix, term, language, tag string, soundsLike ...string) speechcustomize.Word {
	return speechcustomize.WithDefaultsWord(speechcustomize.Word{
		ID:         templateID + ".word." + suffix,
		Term:       term,
		SoundsLike: soundsLike,
		Language:   language,
		Weight:     1.5,
		Source:     source(templateID, version),
		Scope:      &speechcustomize.ScopeRef{Kind: speechcustomize.ScopeBuiltin},
		Enabled:    true,
	})
}

func aliasReplacement(templateID, version, suffix, pattern, output string, priority int, tag string) speechcustomize.Replacement {
	return replacement(templateID, version, suffix, "", speechcustomize.Match{
		Type:         speechcustomize.MatchSpokenAlias,
		Pattern:      pattern,
		WordBoundary: true,
	}, output, priority, tag)
}

func regexReplacement(templateID, version, suffix, language, pattern, output string, priority int, tag string) speechcustomize.Replacement {
	return replacement(templateID, version, suffix, language, speechcustomize.Match{
		Type:    speechcustomize.MatchRegex,
		Pattern: pattern,
	}, output, priority, tag)
}

func replacement(templateID, version, suffix, language string, match speechcustomize.Match, output string, priority int, tag string) speechcustomize.Replacement {
	return speechcustomize.WithDefaultsReplacement(speechcustomize.Replacement{
		ID:       templateID + ".replacement." + suffix,
		Kind:     speechcustomize.KindSubstitution,
		Match:    match,
		Output:   speechcustomize.ReplacementOutput{Text: output},
		Language: language,
		Modes:    []speechcustomize.Mode{speechcustomize.ModeDictation, speechcustomize.ModeAssist},
		Stage:    speechcustomize.StagePostSTT,
		Priority: priority,
		Source:   source(templateID, version),
		Scope:    &speechcustomize.ScopeRef{Kind: speechcustomize.ScopeBuiltin},
		Enabled:  true,
	})
}

func source(templateID, version string) string {
	return "builtin:" + templateID + "@" + version
}

func templateTag(templateID string) string {
	return "template:" + templateID
}
