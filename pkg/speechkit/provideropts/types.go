// Package provideropts defines SpeechKit's provider-neutral voice option
// vocabulary and the manifest/resolve types used by concrete provider
// adapters.
package provideropts

import (
	"fmt"
	"sort"
	"strings"
)

// OptionID names one provider-neutral voice option. A
// [ProviderOptionManifest] says how each provider adapter honors it.
type OptionID string

// Provider-neutral option identifiers. The same ID may back a different
// native parameter, or none at all, in each provider manifest.
const (
	// OptionLanguage is the recognition or synthesis language (BCP-47).
	OptionLanguage OptionID = "language"
	// OptionDetectLanguage asks the provider to detect the spoken language.
	OptionDetectLanguage OptionID = "detect_language"
	// OptionPunctuation enables automatic punctuation.
	OptionPunctuation OptionID = "punctuation"
	// OptionSmartFormat enables provider text formatting of dates, numbers,
	// and similar entities.
	OptionSmartFormat OptionID = "smart_format"
	// OptionDictation turns spoken punctuation commands into punctuation.
	OptionDictation OptionID = "dictation"
	// OptionFillerWords keeps filler words such as "um" in the transcript.
	OptionFillerWords OptionID = "filler_words"
	// OptionNumerals writes spoken numbers as digits.
	OptionNumerals OptionID = "numerals"
	// OptionVocabularyBias sends the resolved Words as provider vocabulary
	// hints.
	OptionVocabularyBias OptionID = "vocabulary_bias"
	// OptionKeyterms is a static list of terms to boost.
	OptionKeyterms OptionID = "keyterms"
	// OptionPromptHint is free-text prompt context for Whisper-style STT.
	OptionPromptHint OptionID = "prompt_hint"
	// OptionSpeakerDiarization labels speakers in the transcript.
	OptionSpeakerDiarization OptionID = "speaker_diarization"
	// OptionTimestamps requests word-level timing.
	OptionTimestamps OptionID = "timestamps"
	// OptionEndpointingMs is the silence, in milliseconds, that ends an
	// utterance or turn.
	OptionEndpointingMs OptionID = "endpointing_ms"
	// OptionTurnDetection enables server-side turn detection; some providers
	// take a confidence threshold instead of a bool.
	OptionTurnDetection OptionID = "turn_detection"
	// OptionVoice selects the TTS voice or speak model.
	OptionVoice OptionID = "voice"
	// OptionSpeed is the TTS speaking rate.
	OptionSpeed OptionID = "speed"
	// OptionAudioFormat selects the TTS output encoding or container.
	OptionAudioFormat OptionID = "audio_format"
	// OptionContextPrompt is the system or context prompt of a streaming STT
	// or Voice Agent session.
	OptionContextPrompt OptionID = "context_prompt"
	// OptionLanguageHints lists likely languages for multilingual sessions.
	OptionLanguageHints OptionID = "language_hints"
	// OptionPrivacyRedaction redacts personal information from transcripts.
	OptionPrivacyRedaction OptionID = "privacy_redaction"
	// OptionVoiceFocus suppresses background noise so recognition follows the
	// primary speaker.
	OptionVoiceFocus OptionID = "voice_focus"
	// OptionMedicalDomain routes to a medical-domain model.
	OptionMedicalDomain OptionID = "medical_domain"
	// OptionReasoningEffort sets the Voice Agent model's reasoning effort.
	OptionReasoningEffort OptionID = "reasoning_effort"
	// OptionTranslation enables realtime translation.
	OptionTranslation OptionID = "translation"
	// OptionTranscriptionOnly runs a Voice Agent session as transcription
	// without agent responses.
	OptionTranscriptionOnly OptionID = "transcription_only"
	// OptionResume resumes an interrupted Voice Agent session.
	OptionResume OptionID = "resume"
	// OptionNoStore asks the provider not to retain the request content
	// beyond the time it takes to answer, and not to train on it. What that
	// costs differs per vendor: a query parameter for some, an account or
	// project setting for others, and nothing at all for a local runtime.
	// The manifest says which, so a retention policy can refuse a provider
	// that cannot assert it rather than promising something untrue.
	OptionNoStore OptionID = "no_store"
)

// OptionType is the value type an option carries in [Values].
type OptionType string

// Option value types. The accessors on [Values] and [EffectiveOptions]
// coerce loosely typed values (JSON numbers, []any) to these shapes.
const (
	TypeString     OptionType = "string"
	TypeBool       OptionType = "bool"
	TypeInt        OptionType = "int"
	TypeFloat      OptionType = "float"
	TypeStringList OptionType = "string_list"
)

// SupportStatus says how a provider adapter honors an option.
type SupportStatus string

// Support statuses recorded in a manifest row.
const (
	// SupportNative maps to a real provider parameter, named by
	// [OptionSupport].NativeKey.
	SupportNative SupportStatus = "native"
	// SupportEmulated is reproduced by SpeechKit's own runtime because the
	// provider has no switch for it.
	SupportEmulated SupportStatus = "emulated"
	// SupportDerived is honored indirectly, for example by omitting the
	// language field or rendering terms into a prompt; Notes say how.
	SupportDerived SupportStatus = "derived"
	// SupportUnsupported cannot be asserted on a request; an explicitly
	// configured value is reported in [EffectiveOptions].Unsupported.
	SupportUnsupported SupportStatus = "unsupported"
	// SupportProviderDefault is already satisfied by the provider without
	// being asked, so SpeechKit sends nothing.
	SupportProviderDefault SupportStatus = "provider_default"
)

// ValueSource records which configuration layer supplied an effective
// option value.
type ValueSource string

// Value sources. [Resolve] takes the first layer that has the option in
// the order request override, provider override, global default, provider
// default.
const (
	// SourceUnset means no layer set the option.
	SourceUnset ValueSource = "unset"
	// SourceProviderDefault is the adapter's built-in default.
	SourceProviderDefault ValueSource = "provider_default"
	// SourceGlobalDefault is the host's provider-independent setting.
	SourceGlobalDefault ValueSource = "global"
	// SourceProviderOverride is the host's per-provider setting.
	SourceProviderOverride ValueSource = "provider_override"
	// SourceRequestOverride was passed with the individual request.
	SourceRequestOverride ValueSource = "request_override"
)

// OptionDefinition is the UI-facing description of an option: its value
// Type plus a Label and Description; Advanced hides it from basic settings
// surfaces.
type OptionDefinition struct {
	ID          OptionID   `json:"id"`
	Label       string     `json:"label"`
	Description string     `json:"description,omitempty"`
	Type        OptionType `json:"type"`
	Advanced    bool       `json:"advanced,omitempty"`
}

// OptionSupport is one manifest row: how a provider adapter treats one
// option. NativeKey names the provider parameter for native support,
// Implemented says whether the adapter actually wires it, EvidenceURL
// points at the vendor documentation the status was verified against, and
// Notes carries caveats such as how multilanguage is expressed.
type OptionSupport struct {
	ID          OptionID         `json:"id"`
	Status      SupportStatus    `json:"status"`
	NativeKey   string           `json:"nativeKey,omitempty"`
	Implemented bool             `json:"implemented"`
	EvidenceURL string           `json:"evidenceUrl,omitempty"`
	Notes       string           `json:"notes,omitempty"`
	Definition  OptionDefinition `json:"definition"`
}

// ProviderOptionManifest declares, for one provider adapter and modality,
// how every option is supported. ProfileIDs lists the catalog profiles the
// manifest covers; Schema and Updated carry [SchemaProviderOptions] and
// [ManifestUpdated].
type ProviderOptionManifest struct {
	Schema     string          `json:"schema"`
	Updated    string          `json:"updated"`
	Provider   string          `json:"provider"`
	Label      string          `json:"label"`
	Modality   string          `json:"modality"`
	ProfileIDs []string        `json:"profileIds,omitempty"`
	Options    []OptionSupport `json:"options"`
}

// Values is one layer of option values keyed by [OptionID]. Values are
// loosely typed (bool, string, numbers, []string, or []any from JSON); the
// typed accessors coerce them. A nil Values is safe to read.
type Values map[OptionID]any

// Clone returns a deep copy of v (slice values are copied), or nil when v
// is empty.
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

// Merge copies every entry of other into v, overwriting existing keys, and
// returns the result. It mutates v in place unless v is nil, in which case
// a new map is returned; an empty other returns v unchanged.
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

// Has reports whether id is present in v, even when its value is nil.
func (v Values) Has(id OptionID) bool {
	_, ok := v[id]
	return ok
}

// Get returns the raw value stored for id, or nil when absent.
func (v Values) Get(id OptionID) any {
	if v == nil {
		return nil
	}
	return v[id]
}

// String returns the trimmed string value for id, or "" when the option is
// absent or not a string.
func (v Values) String(id OptionID) string {
	if value, ok := v[id]; ok {
		if s, ok := value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// Bool returns the bool value for id, or false when the option is absent
// or not a bool.
func (v Values) Bool(id OptionID) bool {
	if value, ok := v[id]; ok {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return false
}

// Int returns the value for id as an int, accepting int, int32, int64, and
// float64 (truncated); it returns 0 when absent or of another type.
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

// Float returns the value for id as a float64, accepting float64, float32,
// and int; it returns 0 when absent or of another type.
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

// StringList returns the value for id as a trimmed list de-duplicated
// case-insensitively. It accepts []string, []any of strings, or a single
// string split on commas, semicolons, and newlines, and returns nil when
// absent or empty.
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

// EffectiveOption is one resolved option: the winning Value, the layer it
// came from, and the provider's support row for it.
type EffectiveOption struct {
	ID      OptionID      `json:"id"`
	Value   any           `json:"value,omitempty"`
	Source  ValueSource   `json:"source"`
	Support OptionSupport `json:"support"`
}

// UnsupportedOptionReport explains why a configured option will not reach
// the provider: it was set explicitly (not unset or a provider default), is
// non-zero, and the manifest marks it [SupportUnsupported].
type UnsupportedOptionReport struct {
	ID        OptionID    `json:"id"`
	Source    ValueSource `json:"source"`
	Value     any         `json:"value,omitempty"`
	Provider  string      `json:"provider"`
	Modality  string      `json:"modality"`
	ProfileID string      `json:"profileId,omitempty"`
	Reason    string      `json:"reason"`
}

// EffectiveOptions is the result of [Resolve] for one provider and
// modality: every option seen in the manifest or any input layer, plus
// reports for explicitly configured options the provider cannot honor.
type EffectiveOptions struct {
	Provider    string                       `json:"provider"`
	Modality    string                       `json:"modality"`
	ProfileID   string                       `json:"profileId,omitempty"`
	Options     map[OptionID]EffectiveOption `json:"options"`
	Unsupported []UnsupportedOptionReport    `json:"unsupported,omitempty"`
}

// ResolveInput carries the manifest and the four value layers [Resolve]
// merges, lowest precedence first: ProviderDefaults, GlobalDefaults,
// ProviderOverrides, RequestOverrides. ProfileID is echoed into the result.
type ResolveInput struct {
	Manifest          ProviderOptionManifest
	ProfileID         string
	ProviderDefaults  Values
	GlobalDefaults    Values
	ProviderOverrides Values
	RequestOverrides  Values
}

// Resolve merges the layers of input into one [EffectiveOptions]. For each
// option the highest-precedence layer that has it wins; options absent
// from the manifest count as unsupported and are reported when explicitly
// configured to a non-zero value. Options are processed in sorted ID order
// so the Unsupported list is deterministic.
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

// SupportByID indexes the manifest's Options by [OptionID]; a later
// duplicate row replaces an earlier one.
func (m ProviderOptionManifest) SupportByID() map[OptionID]OptionSupport {
	out := make(map[OptionID]OptionSupport, len(m.Options))
	for _, opt := range m.Options {
		out[opt.ID] = opt
	}
	return out
}

// Value returns the raw resolved value for id, or nil when the option was
// not resolved.
func (e EffectiveOptions) Value(id OptionID) any {
	if e.Options == nil {
		return nil
	}
	return e.Options[id].Value
}

// String returns the trimmed resolved string for id, or "" when absent or
// not a string.
func (e EffectiveOptions) String(id OptionID) string {
	if opt, ok := e.Options[id]; ok {
		if s, ok := opt.Value.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// Bool returns the resolved bool for id, or false when absent or not a
// bool.
func (e EffectiveOptions) Bool(id OptionID) bool {
	if opt, ok := e.Options[id]; ok {
		if b, ok := opt.Value.(bool); ok {
			return b
		}
	}
	return false
}

// Int returns the resolved value for id as an int, accepting int, int32,
// int64, and float64 (truncated); it returns 0 when absent or of another
// type.
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

// Float returns the resolved value for id as a float64, accepting float64,
// float32, int, int32, and int64; it returns 0 when absent or of another
// type.
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

// StringList returns the resolved value for id with the same coercion
// rules as [Values.StringList], or nil when absent.
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
