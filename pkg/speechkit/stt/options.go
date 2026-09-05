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
	// NoStore asks the provider not to retain this request beyond answering
	// it. Only providers whose manifest declares the option natively act on
	// it; the rest report it as unsupported so a retention policy can refuse
	// them instead of assuming.
	NoStore       bool
	VoiceFocus    bool
	MedicalDomain bool
	Effective     provideropts.EffectiveOptions
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
		NoStore:               effective.Bool(provideropts.OptionNoStore),
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

// LanguageMulti is the value SpeechKit carries when no language is pinned.
//
// SpeechKit does not narrow speech to one language: a pinned language breaks
// switching language mid-conversation and speaking different languages with
// different people in one session, and it is a silent data-loss bug — the same
// English audio returns a zero-length transcript when pinned to German, with
// HTTP 200 and no error.
//
// It is deliberately NOT forwarded verbatim. Each provider expresses
// multilanguage in its own dialect: Deepgram takes the literal "multi",
// AssemblyAI sets language_detection, and the OpenAI-compatible, local and
// OpenRouter adapters express it by omitting the field so the model
// auto-detects. Sending "multi" to any of the latter is an invalid value.
const LanguageMulti = "multi"

// IsMultilanguage reports whether language asks for multilanguage rather than a
// specific locale. Empty, "auto" and "multi" all mean the same thing: do not
// pin. The settings UI offers "auto" and "multi" as separate labels, so both
// have to resolve here or the literal token reaches providers that reject it.
func IsMultilanguage(language string) bool {
	trimmed := strings.TrimSpace(language)
	return trimmed == "" ||
		strings.EqualFold(trimmed, "auto") ||
		strings.EqualFold(trimmed, LanguageMulti)
}

func normalizedRequestLanguage(language string, detectLanguage bool) string {
	if detectLanguage {
		return ""
	}
	if IsMultilanguage(language) {
		return ""
	}
	return strings.TrimSpace(language)
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

// FirstNonEmptyTrimmed returns the first value that is not blank after
// trimming, or "". Provider adapters use it to layer a request override over
// a provider default over a package default.
func FirstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// MinSpeakers and MaxSpeakers collapse the three speaker-count knobs into the
// single lower and upper bound a provider request carries. An exact expected
// count wins over the range.
func MinSpeakers(opts speaker.Options) int {
	if opts.SpeakersExpected > 0 {
		return opts.SpeakersExpected
	}
	return opts.MinSpeakersExpected
}

// MaxSpeakers is the upper-bound counterpart of MinSpeakers.
func MaxSpeakers(opts speaker.Options) int {
	if opts.SpeakersExpected > 0 {
		return opts.SpeakersExpected
	}
	return opts.MaxSpeakersExpected
}
