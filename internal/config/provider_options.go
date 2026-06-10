package config

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// ProviderOptionsConfig stores provider-specific overrides for normalized
// speech options. Empty fields mean "inherit global/provider default"; pointer
// bools let the config preserve explicit false overrides.
type ProviderOptionsConfig struct {
	Deepgram    ProviderModalityOptions `toml:"deepgram"`
	OpenAI      ProviderModalityOptions `toml:"openai"`
	Groq        ProviderModalityOptions `toml:"groq"`
	Google      ProviderModalityOptions `toml:"google"`
	AssemblyAI  ProviderModalityOptions `toml:"assemblyai"`
	OpenRouter  ProviderModalityOptions `toml:"openrouter"`
	HuggingFace ProviderModalityOptions `toml:"huggingface"`
	Local       ProviderModalityOptions `toml:"local"`
	Ollama      ProviderModalityOptions `toml:"ollama"`
}

type ProviderModalityOptions struct {
	STT        ProviderOptionOverrides `toml:"stt"`
	TTS        ProviderOptionOverrides `toml:"tts"`
	VoiceAgent ProviderOptionOverrides `toml:"voice_agent"`
}

type ProviderOptionOverrides struct {
	Language           string   `toml:"language,omitempty"`
	DetectLanguage     *bool    `toml:"detect_language,omitempty"`
	Punctuation        *bool    `toml:"punctuation,omitempty"`
	SmartFormat        *bool    `toml:"smart_format,omitempty"`
	Dictation          *bool    `toml:"dictation,omitempty"`
	FillerWords        *bool    `toml:"filler_words,omitempty"`
	Numerals           *bool    `toml:"numerals,omitempty"`
	VocabularyBias     *bool    `toml:"vocabulary_bias,omitempty"`
	Keyterms           []string `toml:"keyterms,omitempty"`
	PromptHint         string   `toml:"prompt_hint,omitempty"`
	SpeakerDiarization *bool    `toml:"speaker_diarization,omitempty"`
	Timestamps         *bool    `toml:"timestamps,omitempty"`
	EndpointingMs      *int     `toml:"endpointing_ms,omitempty"`
	TurnDetection      *bool    `toml:"turn_detection,omitempty"`
	Voice              string   `toml:"voice,omitempty"`
	Speed              *float64 `toml:"speed,omitempty"`
	AudioFormat        string   `toml:"audio_format,omitempty"`
}

func ProviderOptionOverridesFor(cfg *Config, provider, modality string) provideropts.Values {
	if cfg == nil {
		return nil
	}
	provider = normalizeProviderOptionKey(provider)
	modality = normalizeProviderOptionKey(modality)

	values := provideropts.Values{}
	if provider == "deepgram" && modality == provideropts.ModalitySTT {
		values = values.Merge(legacyDeepgramSTTOverrides(cfg.Providers.Deepgram))
	}
	values = values.Merge(cfg.ProviderOptions.overrides(provider, modality))
	if len(values) == 0 {
		return nil
	}
	return values
}

func ProviderOptionOverridesByProvider(cfg *Config, modality string) map[string]provideropts.Values {
	out := map[string]provideropts.Values{}
	for _, provider := range []string{"deepgram", "openai", "groq", "google", "assemblyai", "openrouter", "huggingface", "local", "ollama"} {
		if values := ProviderOptionOverridesFor(cfg, provider, modality); len(values) > 0 {
			out[provider] = values
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c ProviderOptionsConfig) overrides(provider, modality string) provideropts.Values {
	modalityOptions, ok := c.provider(provider)
	if !ok {
		return nil
	}
	return modalityOptions.overrides(modality).Values()
}

func (c ProviderOptionsConfig) Overrides(provider, modality string) provideropts.Values {
	return c.overrides(provider, modality)
}

func (c *ProviderOptionsConfig) SetOverrides(provider, modality string, values provideropts.Values) {
	if c == nil {
		return
	}
	modalityOptions := c.providerPtr(provider)
	if modalityOptions == nil {
		return
	}
	modalityOptions.setOverrides(modality, values)
}

func (c ProviderOptionsConfig) provider(provider string) (ProviderModalityOptions, bool) {
	switch normalizeProviderOptionKey(provider) {
	case "deepgram":
		return c.Deepgram, true
	case "openai":
		return c.OpenAI, true
	case "groq":
		return c.Groq, true
	case "google":
		return c.Google, true
	case "assemblyai":
		return c.AssemblyAI, true
	case "openrouter":
		return c.OpenRouter, true
	case "huggingface":
		return c.HuggingFace, true
	case "local":
		return c.Local, true
	case "ollama":
		return c.Ollama, true
	default:
		return ProviderModalityOptions{}, false
	}
}

func (c *ProviderOptionsConfig) providerPtr(provider string) *ProviderModalityOptions {
	switch normalizeProviderOptionKey(provider) {
	case "deepgram":
		return &c.Deepgram
	case "openai":
		return &c.OpenAI
	case "groq":
		return &c.Groq
	case "google":
		return &c.Google
	case "assemblyai":
		return &c.AssemblyAI
	case "openrouter":
		return &c.OpenRouter
	case "huggingface":
		return &c.HuggingFace
	case "local":
		return &c.Local
	case "ollama":
		return &c.Ollama
	default:
		return nil
	}
}

func (o ProviderModalityOptions) overrides(modality string) ProviderOptionOverrides {
	switch normalizeProviderOptionKey(modality) {
	case provideropts.ModalitySTT:
		return o.STT
	case provideropts.ModalityTTS:
		return o.TTS
	case provideropts.ModalityVoiceAgent:
		return o.VoiceAgent
	default:
		return ProviderOptionOverrides{}
	}
}

func (o *ProviderModalityOptions) setOverrides(modality string, values provideropts.Values) {
	switch normalizeProviderOptionKey(modality) {
	case provideropts.ModalitySTT:
		o.STT.SetValues(values)
	case provideropts.ModalityTTS:
		o.TTS.SetValues(values)
	case provideropts.ModalityVoiceAgent:
		o.VoiceAgent.SetValues(values)
	}
}

func (o ProviderOptionOverrides) Values() provideropts.Values {
	values := provideropts.Values{}
	if value := strings.TrimSpace(o.Language); value != "" {
		values[provideropts.OptionLanguage] = value
	}
	setBoolValue(values, provideropts.OptionDetectLanguage, o.DetectLanguage)
	setBoolValue(values, provideropts.OptionPunctuation, o.Punctuation)
	setBoolValue(values, provideropts.OptionSmartFormat, o.SmartFormat)
	setBoolValue(values, provideropts.OptionDictation, o.Dictation)
	setBoolValue(values, provideropts.OptionFillerWords, o.FillerWords)
	setBoolValue(values, provideropts.OptionNumerals, o.Numerals)
	setBoolValue(values, provideropts.OptionVocabularyBias, o.VocabularyBias)
	if terms := normalizeProviderOptionStrings(o.Keyterms); len(terms) > 0 {
		values[provideropts.OptionKeyterms] = terms
	}
	if value := strings.TrimSpace(o.PromptHint); value != "" {
		values[provideropts.OptionPromptHint] = value
	}
	setBoolValue(values, provideropts.OptionSpeakerDiarization, o.SpeakerDiarization)
	setBoolValue(values, provideropts.OptionTimestamps, o.Timestamps)
	if o.EndpointingMs != nil && *o.EndpointingMs >= 0 {
		values[provideropts.OptionEndpointingMs] = *o.EndpointingMs
	}
	setBoolValue(values, provideropts.OptionTurnDetection, o.TurnDetection)
	if value := strings.TrimSpace(o.Voice); value != "" {
		values[provideropts.OptionVoice] = value
	}
	if o.Speed != nil && *o.Speed > 0 {
		values[provideropts.OptionSpeed] = *o.Speed
	}
	if value := strings.TrimSpace(o.AudioFormat); value != "" {
		values[provideropts.OptionAudioFormat] = value
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func (o *ProviderOptionOverrides) SetValues(values provideropts.Values) {
	if o == nil {
		return
	}
	*o = ProviderOptionOverrides{}
	for id, value := range values {
		o.setValue(id, value)
	}
}

func (o *ProviderOptionOverrides) setValue(id provideropts.OptionID, value any) {
	switch id {
	case provideropts.OptionLanguage:
		o.Language = strings.TrimSpace(valueString(value))
	case provideropts.OptionDetectLanguage:
		o.DetectLanguage = boolPtr(valueBool(value))
	case provideropts.OptionPunctuation:
		o.Punctuation = boolPtr(valueBool(value))
	case provideropts.OptionSmartFormat:
		o.SmartFormat = boolPtr(valueBool(value))
	case provideropts.OptionDictation:
		o.Dictation = boolPtr(valueBool(value))
	case provideropts.OptionFillerWords:
		o.FillerWords = boolPtr(valueBool(value))
	case provideropts.OptionNumerals:
		o.Numerals = boolPtr(valueBool(value))
	case provideropts.OptionVocabularyBias:
		o.VocabularyBias = boolPtr(valueBool(value))
	case provideropts.OptionKeyterms:
		o.Keyterms = normalizeProviderOptionStrings(provideropts.Values{id: value}.StringList(id))
	case provideropts.OptionPromptHint:
		o.PromptHint = strings.TrimSpace(valueString(value))
	case provideropts.OptionSpeakerDiarization:
		o.SpeakerDiarization = boolPtr(valueBool(value))
	case provideropts.OptionTimestamps:
		o.Timestamps = boolPtr(valueBool(value))
	case provideropts.OptionEndpointingMs:
		o.EndpointingMs = intPtr(valueInt(value))
	case provideropts.OptionTurnDetection:
		o.TurnDetection = boolPtr(valueBool(value))
	case provideropts.OptionVoice:
		o.Voice = strings.TrimSpace(valueString(value))
	case provideropts.OptionSpeed:
		o.Speed = floatPtr(valueFloat(value))
	case provideropts.OptionAudioFormat:
		o.AudioFormat = strings.TrimSpace(valueString(value))
	}
}

func legacyDeepgramSTTOverrides(dg DeepgramProviderConfig) provideropts.Values {
	values := provideropts.Values{}
	if value := strings.TrimSpace(dg.STTLanguage); value != "" {
		values[provideropts.OptionLanguage] = value
	}
	if !dg.STTSmartFormat {
		values[provideropts.OptionSmartFormat] = false
	}
	if dg.STTDictation {
		values[provideropts.OptionDictation] = true
	}
	if dg.STTFillerWords {
		values[provideropts.OptionFillerWords] = true
	}
	if dg.STTNumerals {
		values[provideropts.OptionNumerals] = true
	}
	if dg.STTDetectLanguage {
		values[provideropts.OptionDetectLanguage] = true
	}
	if !dg.STTUseVocabularyKeyterms {
		values[provideropts.OptionVocabularyBias] = false
	}
	if dg.STTEndpointingMs > 0 {
		values[provideropts.OptionEndpointingMs] = dg.STTEndpointingMs
	}
	if terms := splitProviderOptionTerms(dg.STTKeyterms); len(terms) > 0 {
		values[provideropts.OptionKeyterms] = terms
	}
	return values
}

func setBoolValue(values provideropts.Values, id provideropts.OptionID, value *bool) {
	if value != nil {
		values[id] = *value
	}
}

func normalizeProviderOptionKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeProviderOptionStrings(input []string) []string {
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

func splitProviderOptionTerms(raw string) []string {
	return normalizeProviderOptionStrings(strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	}))
}

func valueString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func valueBool(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return false
}

func valueInt(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func valueFloat(value any) float64 {
	switch n := value.(type) {
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
	default:
		return 0
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func floatPtr(value float64) *float64 {
	return &value
}
