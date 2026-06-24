package stt

import (
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

type ResolvedTranscribeOptions struct {
	Language              string
	Model                 string
	Prompt                string
	ContextPrompt         string
	LanguageHints         []string
	Keyterms              []string
	Speaker               speaker.Options
	DetectLanguage        bool
	Punctuation           bool
	SmartFormat           bool
	Dictation             bool
	FillerWords           bool
	Numerals              bool
	UseVocabularyKeyterms bool
	Timestamps            bool
	EndpointingMs         int
	PrivacyRedaction      bool
	VoiceFocus            bool
	MedicalDomain         bool
	Effective             provideropts.EffectiveOptions
}

func ResolveTranscribeOptions(provider, profileID string, opts TranscribeOpts, providerDefaults, providerOverrides provideropts.Values) ResolvedTranscribeOptions {
	manifest, ok := provideropts.FindManifest(provider, provideropts.ModalitySTT)
	if !ok {
		manifest = provideropts.ProviderOptionManifest{
			Schema:   provideropts.SchemaProviderOptions,
			Updated:  provideropts.ManifestUpdated,
			Provider: provider,
			Modality: provideropts.ModalitySTT,
		}
	}
	global := opts.Options.Clone()
	request := provideropts.Values{}
	if lang := strings.TrimSpace(opts.Language); lang != "" {
		request[provideropts.OptionLanguage] = lang
	}
	if prompt := strings.TrimSpace(opts.Prompt); prompt != "" {
		request[provideropts.OptionPromptHint] = prompt
	}
	if len(opts.Keyterms) > 0 {
		request[provideropts.OptionKeyterms] = append([]string(nil), opts.Keyterms...)
	}
	if opts.Speaker.Normalized().WantsDiarization() {
		request[provideropts.OptionSpeakerDiarization] = true
	}

	mergedProviderOverrides := providerOverrides.Clone()
	mergedProviderOverrides = mergedProviderOverrides.Merge(opts.ProviderOptions)
	effective := provideropts.Resolve(provideropts.ResolveInput{
		Manifest:          manifest,
		ProfileID:         profileID,
		ProviderDefaults:  providerDefaults,
		GlobalDefaults:    global,
		ProviderOverrides: mergedProviderOverrides,
		RequestOverrides:  request,
	})
	logUnsupportedOptions(effective.Unsupported)

	terms := effective.StringList(provideropts.OptionKeyterms)
	resolved := ResolvedTranscribeOptions{
		Language:              effective.String(provideropts.OptionLanguage),
		Model:                 strings.TrimSpace(opts.Model),
		Prompt:                effective.String(provideropts.OptionPromptHint),
		ContextPrompt:         effective.String(provideropts.OptionContextPrompt),
		LanguageHints:         effective.StringList(provideropts.OptionLanguageHints),
		Keyterms:              terms,
		Speaker:               opts.Speaker.Normalized(),
		DetectLanguage:        effective.Bool(provideropts.OptionDetectLanguage),
		Punctuation:           effective.Bool(provideropts.OptionPunctuation),
		SmartFormat:           effective.Bool(provideropts.OptionSmartFormat),
		Dictation:             effective.Bool(provideropts.OptionDictation),
		FillerWords:           effective.Bool(provideropts.OptionFillerWords),
		Numerals:              effective.Bool(provideropts.OptionNumerals),
		UseVocabularyKeyterms: effective.Bool(provideropts.OptionVocabularyBias),
		Timestamps:            effective.Bool(provideropts.OptionTimestamps),
		EndpointingMs:         effective.Int(provideropts.OptionEndpointingMs),
		PrivacyRedaction:      effective.Bool(provideropts.OptionPrivacyRedaction),
		VoiceFocus:            effective.Bool(provideropts.OptionVoiceFocus),
		MedicalDomain:         effective.Bool(provideropts.OptionMedicalDomain),
		Effective:             effective,
	}
	if resolved.Prompt == "" {
		resolved.Prompt = strings.TrimSpace(opts.Prompt)
	}
	if len(resolved.Keyterms) == 0 {
		resolved.Keyterms = append([]string(nil), opts.Keyterms...)
	}
	return resolved
}

func logUnsupportedOptions(reports []provideropts.UnsupportedOptionReport) {
	for _, report := range reports {
		slog.Debug("speech option ignored by provider",
			"provider", report.Provider,
			"modality", report.Modality,
			"option", report.ID,
			"source", report.Source,
			"reason", report.Reason,
		)
	}
}

func normalizedRequestLanguage(language string, detectLanguage bool) string {
	if detectLanguage {
		return ""
	}
	language = strings.TrimSpace(language)
	if language == "" || strings.EqualFold(language, "auto") {
		return ""
	}
	return language
}

func (r ResolvedTranscribeOptions) APILanguage() string {
	option, ok := r.Effective.Options[provideropts.OptionLanguage]
	if ok {
		switch option.Source {
		case provideropts.SourceProviderDefault, provideropts.SourceUnset:
			return ""
		case provideropts.SourceGlobalDefault, provideropts.SourceProviderOverride, provideropts.SourceRequestOverride:
			return normalizedRequestLanguage(r.Language, r.DetectLanguage)
		}
	}
	return normalizedRequestLanguage(r.Language, r.DetectLanguage)
}
