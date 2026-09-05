// Package provideropts defines SpeechKit's provider-neutral voice option
// vocabulary and the manifest/resolve types used by concrete provider
// adapters.
package provideropts

import (
	"fmt"
	"sort"
	"strings"
)

type OptionID string

const (
	OptionLanguage           OptionID = "language"
	OptionDetectLanguage     OptionID = "detect_language"
	OptionPunctuation        OptionID = "punctuation"
	OptionSmartFormat        OptionID = "smart_format"
	OptionDictation          OptionID = "dictation"
	OptionFillerWords        OptionID = "filler_words"
	OptionNumerals           OptionID = "numerals"
	OptionVocabularyBias     OptionID = "vocabulary_bias"
	OptionKeyterms           OptionID = "keyterms"
	OptionPromptHint         OptionID = "prompt_hint"
	OptionSpeakerDiarization OptionID = "speaker_diarization"
	OptionTimestamps         OptionID = "timestamps"
	OptionEndpointingMs      OptionID = "endpointing_ms"
	OptionTurnDetection      OptionID = "turn_detection"
	OptionVoice              OptionID = "voice"
	OptionSpeed              OptionID = "speed"
	OptionAudioFormat        OptionID = "audio_format"
	OptionContextPrompt      OptionID = "context_prompt"
	OptionLanguageHints      OptionID = "language_hints"
	OptionPrivacyRedaction   OptionID = "privacy_redaction"
	OptionVoiceFocus         OptionID = "voice_focus"
	OptionMedicalDomain      OptionID = "medical_domain"
	OptionReasoningEffort    OptionID = "reasoning_effort"
	OptionTranslation        OptionID = "translation"
	OptionTranscriptionOnly  OptionID = "transcription_only"
	OptionResume             OptionID = "resume"
	// OptionNoStore asks the provider not to retain the request content
	// beyond the time it takes to answer, and not to train on it. What that
	// costs differs per vendor: a query parameter for some, an account or
	// project setting for others, and nothing at all for a local runtime.
	// The manifest says which, so a retention policy can refuse a provider
	// that cannot assert it rather than promising something untrue.
	OptionNoStore OptionID = "no_store"
)

type OptionType string

const (
	TypeString     OptionType = "string"
	TypeBool       OptionType = "bool"
	TypeInt        OptionType = "int"
	TypeFloat      OptionType = "float"
	TypeStringList OptionType = "string_list"
)

type SupportStatus string

const (
	SupportNative          SupportStatus = "native"
	SupportEmulated        SupportStatus = "emulated"
	SupportDerived         SupportStatus = "derived"
	SupportUnsupported     SupportStatus = "unsupported"
	SupportProviderDefault SupportStatus = "provider_default"
)

type ValueSource string

const (
	SourceUnset            ValueSource = "unset"
	SourceProviderDefault  ValueSource = "provider_default"
	SourceGlobalDefault    ValueSource = "global"
	SourceProviderOverride ValueSource = "provider_override"
	SourceRequestOverride  ValueSource = "request_override"
)

type OptionDefinition struct {
	ID          OptionID   `json:"id"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	Type        OptionType `json:"type"`
	Advanced    bool       `json:"advanced,omitempty"`
}

type OptionSupport struct {
	ID          OptionID         `json:"id"`
	Status      SupportStatus    `json:"status"`
	NativeKey   string           `json:"nativeKey,omitempty"`
	Implemented bool             `json:"implemented"`
	EvidenceURL string           `json:"evidenceUrl,omitempty"`
	Notes       string           `json:"notes,omitempty"`
	Definition  OptionDefinition `json:"definition"`
}

type ProviderOptionManifest struct {
	Schema     string          `json:"schema"`
	Updated    string          `json:"updated"`
	Provider   string          `json:"provider"`
	Label      string          `json:"label"`
	Modality   string          `json:"modality"`
	ProfileIDs []string        `json:"profileIds,omitempty"`
	Options    []OptionSupport `json:"options"`
}

type Values map[OptionID]any

func (v Values) Clone() Values {
	if len(v) == 0 {
		return nil
	}
	out := make(Values, len(v))
	for key, value := range v {
		out[key] = cloneValue(value)
	}
	return out
}

func (v Values) Merge(other Values) Values {
	if len(other) == 0 {
		return v
	}
	if v == nil {
		v = Values{}
	}
	for key, value := range other {
		v[key] = cloneValue(value)
	}
	return v
}

func (v Values) Has(id OptionID) bool {
	_, ok := v[id]
	return ok
}

func (v Values) Get(id OptionID) any {
	if v == nil {
		return nil
	}
	return v[id]
}

func (v Values) String(id OptionID) string {
	if value, ok := v[id]; ok {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func (v Values) Bool(id OptionID) bool {
	if value, ok := v[id]; ok {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return false
}

func (v Values) Int(id OptionID) int {
	if value, ok := v[id]; ok {
		switch n := value.(type) {
		case int:
			return n
		case int32:
			return int(n)
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

func (v Values) Float(id OptionID) float64 {
	if value, ok := v[id]; ok {
		switch n := value.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		}
	}
	return 0
}

func (v Values) StringList(id OptionID) []string {
	value, ok := v[id]
	if !ok {
		return nil
	}
	switch terms := value.(type) {
	case []string:
		return normalizeStrings(terms)
	case []any:
		out := make([]string, 0, len(terms))
		for _, term := range terms {
			if s, ok := term.(string); ok {
				out = append(out, s)
			}
		}
		return normalizeStrings(out)
	case string:
		return normalizeStrings(strings.FieldsFunc(terms, func(r rune) bool {
			return r == ',' || r == '\n' || r == ';'
		}))
	default:
		return nil
	}
}

type EffectiveOption struct {
	ID      OptionID      `json:"id"`
	Value   any           `json:"value,omitempty"`
	Source  ValueSource   `json:"source"`
	Support OptionSupport `json:"support"`
}

type UnsupportedOptionReport struct {
	ID        OptionID    `json:"id"`
	Source    ValueSource `json:"source"`
	Value     any         `json:"value,omitempty"`
	Provider  string      `json:"provider"`
	Modality  string      `json:"modality"`
	ProfileID string      `json:"profileId,omitempty"`
	Reason    string      `json:"reason"`
}

type EffectiveOptions struct {
	Provider    string                       `json:"provider"`
	Modality    string                       `json:"modality"`
	ProfileID   string                       `json:"profileId,omitempty"`
	Options     map[OptionID]EffectiveOption `json:"options"`
	Unsupported []UnsupportedOptionReport    `json:"unsupported,omitempty"`
}

type ResolveInput struct {
	Manifest          ProviderOptionManifest
	ProfileID         string
	ProviderDefaults  Values
	GlobalDefaults    Values
	ProviderOverrides Values
	RequestOverrides  Values
}

func Resolve(input ResolveInput) EffectiveOptions {
	out := EffectiveOptions{
		Provider:  input.Manifest.Provider,
		Modality:  input.Manifest.Modality,
		ProfileID: strings.TrimSpace(input.ProfileID),
		Options:   map[OptionID]EffectiveOption{},
	}
	optionIDs := sortedOptionIDs(input)
	supportByID := input.Manifest.SupportByID()
	for _, id := range optionIDs {
		support, ok := supportByID[id]
		if !ok {
			support = OptionSupport{
				ID:          id,
				Status:      SupportUnsupported,
				Implemented: true,
				Definition:  OptionDefinition{ID: id, Label: string(id), Type: TypeString},
			}
		}
		value, source := resolvedValue(id, input)
		out.Options[id] = EffectiveOption{ID: id, Value: cloneValue(value), Source: source, Support: support}
		if shouldReportUnsupported(support, source, value) {
			out.Unsupported = append(out.Unsupported, UnsupportedOptionReport{
				ID:        id,
				Source:    source,
				Value:     cloneValue(value),
				Provider:  out.Provider,
				Modality:  out.Modality,
				ProfileID: out.ProfileID,
				Reason:    fmt.Sprintf("%s does not support %s for %s", out.Provider, id, out.Modality),
			})
		}
	}
	return out
}

func (m ProviderOptionManifest) SupportByID() map[OptionID]OptionSupport {
	out := make(map[OptionID]OptionSupport, len(m.Options))
	for _, opt := range m.Options {
		out[opt.ID] = opt
	}
	return out
}

func (e EffectiveOptions) Value(id OptionID) any {
	if e.Options == nil {
		return nil
	}
	return e.Options[id].Value
}

func (e EffectiveOptions) String(id OptionID) string {
	if opt, ok := e.Options[id]; ok {
		if s, ok := opt.Value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func (e EffectiveOptions) Bool(id OptionID) bool {
	if opt, ok := e.Options[id]; ok {
		if b, ok := opt.Value.(bool); ok {
			return b
		}
	}
	return false
}

func (e EffectiveOptions) Int(id OptionID) int {
	if opt, ok := e.Options[id]; ok {
		switch n := opt.Value.(type) {
		case int:
			return n
		case int32:
			return int(n)
		case int64:
			return int(n)
		case float64:
			return int(n)
		}
	}
	return 0
}

func (e EffectiveOptions) Float(id OptionID) float64 {
	if opt, ok := e.Options[id]; ok {
		switch n := opt.Value.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		case int32:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return 0
}

func (e EffectiveOptions) StringList(id OptionID) []string {
	if opt, ok := e.Options[id]; ok {
		return Values{id: opt.Value}.StringList(id)
	}
	return nil
}

func resolvedValue(id OptionID, input ResolveInput) (any, ValueSource) {
	if input.RequestOverrides.Has(id) {
		return input.RequestOverrides.Get(id), SourceRequestOverride
	}
	if input.ProviderOverrides.Has(id) {
		return input.ProviderOverrides.Get(id), SourceProviderOverride
	}
	if input.GlobalDefaults.Has(id) {
		return input.GlobalDefaults.Get(id), SourceGlobalDefault
	}
	if input.ProviderDefaults.Has(id) {
		return input.ProviderDefaults.Get(id), SourceProviderDefault
	}
	return nil, SourceUnset
}

func sortedOptionIDs(input ResolveInput) []OptionID {
	seen := map[OptionID]bool{}
	var ids []OptionID
	addValues := func(values Values) {
		for id := range values {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	for _, opt := range input.Manifest.Options {
		if !seen[opt.ID] {
			seen[opt.ID] = true
			ids = append(ids, opt.ID)
		}
	}
	addValues(input.ProviderDefaults)
	addValues(input.GlobalDefaults)
	addValues(input.ProviderOverrides)
	addValues(input.RequestOverrides)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func shouldReportUnsupported(support OptionSupport, source ValueSource, value any) bool {
	if source == SourceUnset || source == SourceProviderDefault {
		return false
	}
	if support.Status != SupportUnsupported {
		return false
	}
	return !isZeroValue(value)
}

func isZeroValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case bool:
		return !v
	case int:
		return v == 0
	case int32:
		return v == 0
	case int64:
		return v == 0
	case float32:
		return v == 0
	case float64:
		return v == 0
	case []string:
		return len(normalizeStrings(v)) == 0
	case []any:
		return len(v) == 0
	default:
		return false
	}
}

func cloneValue(value any) any {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		return append([]any(nil), v...)
	default:
		return value
	}
}

func normalizeStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	seen := map[string]bool{}
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}
