package customize

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

type Set struct {
	Words        []speechcustomize.Word
	Replacements []speechcustomize.Replacement
}

type DictionaryEntry struct {
	Spoken     string
	Canonical  string
	Language   string
	Source     string
	Enabled    bool
	UsageCount int
}

type MatchRecord struct {
	ReplacementID string `json:"replacement_id,omitempty"`
	Term          string `json:"term"`
	Count         int    `json:"count"`
}

type Action struct {
	ReplacementID string               `json:"replacement_id,omitempty"`
	Kind          speechcustomize.Kind `json:"kind"`
	Intent        string               `json:"intent,omitempty"`
	Text          string               `json:"text,omitempty"`
	Template      string               `json:"template,omitempty"`
	Payload       map[string]any       `json:"payload,omitempty"`
	MatchedText   string               `json:"matched_text,omitempty"`
	Count         int                  `json:"count"`
}

type ApplyResult struct {
	Text    string
	Matches []MatchRecord
	Actions []Action
}

func PublicActions(actions []Action) []speechkit.CustomizationAction {
	if len(actions) == 0 {
		return nil
	}
	out := make([]speechkit.CustomizationAction, 0, len(actions))
	for _, action := range actions {
		out = append(out, speechkit.CustomizationAction{
			ReplacementID: action.ReplacementID,
			Kind:          string(action.Kind),
			Intent:        action.Intent,
			Text:          action.Text,
			Template:      action.Template,
			Payload:       clonePayload(action.Payload),
			MatchedText:   action.MatchedText,
			Count:         action.Count,
		})
	}
	return out
}

type ProviderBias struct {
	Prompt     string
	Keyterms   []string
	ByProvider map[string]provideropts.Values
	Preview    []ProviderBiasPreview
}

type ProviderBiasPreview struct {
	Provider string
	Modality string
	Strategy string
	Native   bool
	Keyterms []string
	Prompt   string
}

type CompiledApplier struct {
	Replacements []speechcustomize.Replacement
	compiled     []compiledReplacement
}

type compiledReplacement struct {
	Replacement speechcustomize.Replacement
	Regexp      *regexp.Regexp
}

const maxReplacementApplyPasses = 8

func WordsFromDictionary(entries []DictionaryEntry) []speechcustomize.Word {
	words := make([]speechcustomize.Word, 0, len(entries))
	seen := map[string]struct{}{}
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		canonical := strings.TrimSpace(entry.Canonical)
		if canonical == "" {
			continue
		}
		language := speechcustomize.NormalizeLanguage(entry.Language)
		key := strings.ToLower(language + "\x00" + canonical)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		word := speechcustomize.WithDefaultsWord(speechcustomize.Word{
			Term:       canonical,
			Language:   language,
			Source:     firstNonEmpty(entry.Source, "settings"),
			Enabled:    true,
			UsageCount: entry.UsageCount,
		})
		words = append(words, word)
	}
	return words
}

func ReplacementsFromDictionary(entries []DictionaryEntry) []speechcustomize.Replacement {
	replacements := make([]speechcustomize.Replacement, 0, len(entries))
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		spoken := strings.TrimSpace(entry.Spoken)
		canonical := strings.TrimSpace(entry.Canonical)
		if spoken == "" || canonical == "" || strings.EqualFold(spoken, canonical) {
			continue
		}
		replacement := speechcustomize.WithDefaultsReplacement(speechcustomize.Replacement{
			Kind:     speechcustomize.KindSubstitution,
			Language: speechcustomize.NormalizeLanguage(entry.Language),
			Modes:    []speechcustomize.Mode{speechcustomize.ModeDictation, speechcustomize.ModeAssist},
			Stage:    speechcustomize.StagePostSTT,
			Match: speechcustomize.Match{
				Type:         speechcustomize.MatchSpokenAlias,
				Pattern:      spoken,
				WordBoundary: true,
			},
			Output:     speechcustomize.ReplacementOutput{Text: canonical},
			Source:     firstNonEmpty(entry.Source, "settings"),
			Enabled:    true,
			UsageCount: entry.UsageCount,
		})
		replacements = append(replacements, replacement)
	}
	return replacements
}

func DictionaryFromSet(set Set) []DictionaryEntry {
	entries := make([]DictionaryEntry, 0, len(set.Words)+len(set.Replacements))
	seen := map[string]struct{}{}
	seenCanonical := map[string]struct{}{}
	for _, replacement := range set.Replacements {
		if !replacement.Enabled || replacement.Stage != speechcustomize.StagePostSTT {
			continue
		}
		if replacement.Kind != speechcustomize.KindSubstitution && replacement.Kind != speechcustomize.KindSynonym {
			continue
		}
		spoken := strings.TrimSpace(replacement.Match.Pattern)
		canonical := strings.TrimSpace(replacement.Output.Text)
		if spoken == "" || canonical == "" {
			continue
		}
		key := dictionaryKey(spoken, canonical, replacement.Language)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		seenCanonical[strings.ToLower(replacement.Language+"\x00"+canonical)] = struct{}{}
		entries = append(entries, DictionaryEntry{
			Spoken:     spoken,
			Canonical:  canonical,
			Language:   replacement.Language,
			Source:     replacement.Source,
			Enabled:    true,
			UsageCount: replacement.UsageCount,
		})
	}
	for _, word := range set.Words {
		if !word.Enabled || strings.TrimSpace(word.Term) == "" {
			continue
		}
		canonicalKey := strings.ToLower(word.Language + "\x00" + strings.TrimSpace(word.Term))
		if _, ok := seenCanonical[canonicalKey]; ok {
			continue
		}
		key := dictionaryKey(word.Term, word.Term, word.Language)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		entries = append(entries, DictionaryEntry{
			Spoken:     word.Term,
			Canonical:  word.Term,
			Language:   word.Language,
			Source:     word.Source,
			Enabled:    true,
			UsageCount: word.UsageCount,
		})
	}
	return entries
}

func BuildPrompt(words []speechcustomize.Word) string {
	terms := CanonicalTerms(words)
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these terms when transcribing: " + strings.Join(terms, ", ") + "."
}

func BuildKeyterms(words []speechcustomize.Word) []string {
	terms := CanonicalTerms(words)
	if len(terms) == 0 {
		return nil
	}
	return append([]string(nil), terms...)
}

func BuildVoiceAgentHint(words []speechcustomize.Word) string {
	terms := CanonicalTerms(words)
	if len(terms) == 0 {
		return ""
	}
	return "Prefer these names and product terms in recognition and responses: " + strings.Join(terms, ", ") + "."
}

func BuildProviderBias(words []speechcustomize.Word) ProviderBias {
	return BuildProviderBiasForModality(words, provideropts.ModalitySTT)
}

func BuildProviderBiasForModality(words []speechcustomize.Word, modality string) ProviderBias {
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

func CanonicalTerms(words []speechcustomize.Word) []string {
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

func Apply(text string, replacements []speechcustomize.Replacement, stage speechcustomize.Stage) (ApplyResult, error) {
	applier, err := CompileApplier(replacements, speechcustomize.Context{Stage: stage})
	if err != nil {
		return ApplyResult{Text: text}, err
	}
	return applier.Apply(text), nil
}

func CompileApplier(replacements []speechcustomize.Replacement, context speechcustomize.Context) (*CompiledApplier, error) {
	active := activeReplacements(replacements, context)
	compiled := make([]compiledReplacement, 0, len(active))
	for _, replacement := range active {
		re, err := compileReplacement(replacement)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, compiledReplacement{Replacement: replacement, Regexp: re})
	}
	return &CompiledApplier{Replacements: active, compiled: compiled}, nil
}

func (a *CompiledApplier) Apply(text string) ApplyResult {
	result := ApplyResult{Text: text}
	if a == nil {
		return result
	}
	for _, replacement := range a.compiled {
		total := 0
		if replacement.Replacement.Kind == speechcustomize.KindCommand {
			matches := replacement.Regexp.FindAllString(result.Text, -1)
			if len(matches) == 0 {
				continue
			}
			total = len(matches)
			firstMatch := matches[0]
			rendered := renderReplacementOutput(replacement.Replacement, result.Text, firstMatch)
			result.Actions = append(result.Actions, Action{
				ReplacementID: replacement.Replacement.ID,
				Kind:          replacement.Replacement.Kind,
				Intent:        strings.TrimSpace(replacement.Replacement.Output.Intent),
				Text:          rendered,
				Template:      strings.TrimSpace(replacement.Replacement.Output.Template),
				Payload:       clonePayload(replacement.Replacement.Output.Payload),
				MatchedText:   firstMatch,
				Count:         total,
			})
			result.Matches = append(result.Matches, MatchRecord{
				ReplacementID: replacement.Replacement.ID,
				Term:          firstNonEmpty(replacement.Replacement.Output.Intent, firstMatch),
				Count:         total,
			})
			continue
		}
		for pass := 0; pass < maxReplacementApplyPasses; pass++ {
			matches := replacement.Regexp.FindAllString(result.Text, -1)
			if len(matches) == 0 {
				break
			}
			count := len(matches)
			var next string
			if strings.TrimSpace(replacement.Replacement.Output.Template) == "" {
				next = replacement.Regexp.ReplaceAllString(result.Text, replacement.Replacement.Output.Text)
			} else {
				current := result.Text
				next = replacement.Regexp.ReplaceAllStringFunc(result.Text, func(match string) string {
					return renderReplacementOutput(replacement.Replacement, current, match)
				})
			}
			if next == result.Text {
				break
			}
			result.Text = next
			total += count
		}
		if total > 0 {
			result.Matches = append(result.Matches, MatchRecord{
				ReplacementID: replacement.Replacement.ID,
				Term:          renderReplacementOutput(replacement.Replacement, text, ""),
				Count:         total,
			})
		}
	}
	return result
}

func activeReplacements(replacements []speechcustomize.Replacement, context speechcustomize.Context) []speechcustomize.Replacement {
	stage := speechcustomize.NormalizeStage(context.Stage)
	mode := speechcustomize.NormalizeMode(context.Mode)
	active := make([]speechcustomize.Replacement, 0, len(replacements))
	for _, replacement := range replacements {
		replacement = speechcustomize.WithDefaultsReplacement(replacement)
		if !replacement.Enabled {
			continue
		}
		if stage != "" && replacement.Stage != stage {
			continue
		}
		if !replacementKindIsActive(replacement.Kind) {
			continue
		}
		if mode != "" && len(replacement.Modes) > 0 && !replacementHasMode(replacement, mode) {
			continue
		}
		if !tagsMatch(context.Tags, replacement.Tags) {
			continue
		}
		if !replacementHasRunnableOutput(replacement) || strings.TrimSpace(replacement.Match.Pattern) == "" {
			continue
		}
		active = append(active, replacement)
	}
	sort.SliceStable(active, func(i, j int) bool {
		if active[i].Priority != active[j].Priority {
			return active[i].Priority > active[j].Priority
		}
		return active[i].ID < active[j].ID
	})
	return active
}

func replacementHasMode(replacement speechcustomize.Replacement, mode speechcustomize.Mode) bool {
	for _, candidate := range replacement.Modes {
		if speechcustomize.NormalizeMode(candidate) == mode {
			return true
		}
	}
	return false
}

func tagsMatch(contextTags, itemTags []string) bool {
	if len(itemTags) == 0 {
		return true
	}
	if len(contextTags) == 0 {
		return true
	}
	seen := map[string]struct{}{}
	for _, tag := range contextTags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			seen[tag] = struct{}{}
		}
	}
	for _, tag := range itemTags {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
}

func optionCanCarryBias(opt provideropts.OptionSupport) bool {
	return opt.Status != "" && opt.Status != provideropts.SupportUnsupported
}

func replacementKindIsActive(kind speechcustomize.Kind) bool {
	switch kind {
	case speechcustomize.KindSubstitution, speechcustomize.KindSynonym, speechcustomize.KindSnippet, speechcustomize.KindTemplate, speechcustomize.KindCommand:
		return true
	default:
		return false
	}
}

func replacementHasRunnableOutput(replacement speechcustomize.Replacement) bool {
	output := replacement.Output
	switch replacement.Kind {
	case speechcustomize.KindCommand:
		return strings.TrimSpace(output.Intent) != "" || strings.TrimSpace(output.Text) != "" || strings.TrimSpace(output.Template) != "" || len(output.Payload) > 0
	case speechcustomize.KindSubstitution, speechcustomize.KindSynonym, speechcustomize.KindSnippet, speechcustomize.KindTemplate:
		return strings.TrimSpace(output.Text) != "" || strings.TrimSpace(output.Template) != ""
	default:
		return false
	}
}

func renderReplacementOutput(replacement speechcustomize.Replacement, input, match string) string {
	raw := strings.TrimSpace(replacement.Output.Template)
	if raw == "" {
		return replacement.Output.Text
	}
	tmpl, err := template.New("replacement").Option("missingkey=zero").Funcs(template.FuncMap{
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"trim":  strings.TrimSpace,
	}).Parse(raw)
	if err != nil {
		return raw
	}
	data := map[string]any{
		"ID":      replacement.ID,
		"Kind":    replacement.Kind,
		"Input":   input,
		"Match":   match,
		"Intent":  replacement.Output.Intent,
		"Payload": replacement.Output.Payload,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return raw
	}
	return buf.String()
}

func clonePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	return out
}

func compileReplacement(replacement speechcustomize.Replacement) (*regexp.Regexp, error) {
	pattern := strings.TrimSpace(replacement.Match.Pattern)
	matchType := speechcustomize.NormalizeMatchType(replacement.Match.Type)
	if matchType != speechcustomize.MatchRegex {
		pattern = regexp.QuoteMeta(pattern)
		if replacement.Match.WordBoundary || matchType == speechcustomize.MatchPhrase || matchType == speechcustomize.MatchSpokenAlias {
			pattern = `\b` + pattern + `\b`
		}
	}
	if !replacement.Match.CaseSensitive {
		pattern = `(?i)` + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("customize: compile replacement %q: %w", replacement.ID, err)
	}
	return re, nil
}

func dictionaryKey(spoken, canonical, language string) string {
	return strings.ToLower(strings.TrimSpace(language) + "\x00" + strings.TrimSpace(spoken) + "\x00" + strings.TrimSpace(canonical))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
