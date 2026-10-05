package stt

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// WithVocabulary returns a copy of o with dictionary hints rendered by the public
// customization contract. Explicit Prompt/Keyterms and option overrides retain
// their precedence; a vocabulary_bias opt-out suppresses the derived hints.
func (o TranscribeOpts) WithVocabulary(words []customize.Word) TranscribeOpts {
	o.VocabularyHints = customize.BuildRecognitionHints(words)
	return o
}

// DictationStreamOptions carries the same recognition settings into a native
// stream. base supplies stream metadata and explicit request overrides; nonzero
// base values win. Dictionary hints remain separate until the adapter resolves
// its provider defaults and overrides, including vocabulary opt-outs.
func (o TranscribeOpts) DictationStreamOptions(base speechkit.DictationStreamOptions) speechkit.DictationStreamOptions {
	base.Language = FirstNonEmptyTrimmed(base.Language, o.Language)
	base.Model = FirstNonEmptyTrimmed(base.Model, o.Model)
	base.ProviderProfileID = FirstNonEmptyTrimmed(base.ProviderProfileID, o.ProviderProfileID)
	base.PromptHint = FirstNonEmptyTrimmed(base.PromptHint, o.Prompt)
	base.Keyterms = mergeRecognitionTerms(base.Keyterms, o.Keyterms)
	base.VocabularyHints.Prompt = FirstNonEmptyTrimmed(base.VocabularyHints.Prompt, o.VocabularyHints.Prompt)
	base.VocabularyHints.Keyterms = mergeRecognitionTerms(base.VocabularyHints.Keyterms, o.VocabularyHints.Keyterms)
	base.RequestOptions = o.RequestOptions.Clone().Merge(base.RequestOptions)
	base.Options = o.Options.Clone().Merge(base.Options)
	base.ProviderOptions = o.ProviderOptions.Clone().Merge(base.ProviderOptions)
	merged := map[string]provideropts.Values{}
	for name, values := range o.ProviderOptionsByProvider {
		merged[normalizeProviderKey(name)] = values.Clone()
	}
	for name, values := range base.ProviderOptionsByProvider {
		name = normalizeProviderKey(name)
		merged[name] = merged[name].Merge(values)
	}
	base.ProviderOptionsByProvider = merged
	return base
}

// TranscribeOptionsFromStream returns the shared recognition option sources.
// Stream session metadata remains on the original DictationStreamOptions.
func TranscribeOptionsFromStream(o speechkit.DictationStreamOptions) TranscribeOpts {
	opts := TranscribeOpts{
		Language: o.Language, Model: o.Model, Prompt: o.PromptHint,
		Keyterms: append([]string(nil), o.Keyterms...), VocabularyHints: o.VocabularyHints,
		ProviderProfileID: o.ProviderProfileID, Options: o.Options.Clone(),
		RequestOptions: o.RequestOptions.Clone(), ProviderOptions: o.ProviderOptions.Clone(), ProviderOptionsByProvider: o.ProviderOptionsByProvider,
		Speaker: speaker.Options{Diarization: o.Diarization},
	}
	if o.EndpointingMs > 0 {
		opts.RequestOptions = opts.RequestOptions.Merge(provideropts.Values{provideropts.OptionEndpointingMs: o.EndpointingMs})
	}
	if o.TurnDetection {
		opts.RequestOptions = opts.RequestOptions.Merge(provideropts.Values{provideropts.OptionTurnDetection: true})
	}
	return opts
}

// ResolveDictationStreamOptions applies the batch option resolver to a native
// stream, retaining its session metadata. Concrete adapters supply their own
// defaults and overrides, just as they do for ResolveTranscribeOptions.
func ResolveDictationStreamOptions(provider, profileID string, opts speechkit.DictationStreamOptions, defaults, overrides provideropts.Values) speechkit.DictationStreamOptions {
	request := TranscribeOptionsFromStream(opts).ForProvider(provider)
	resolved := ResolveTranscribeOptions(provider, profileID, request, defaults, overrides)
	return ApplyResolvedDictationStreamOptions(opts, resolved)
}

// ApplyResolvedDictationStreamOptions maps resolved recognition settings back
// onto a stream while preserving session, interim, format and profile metadata.
func ApplyResolvedDictationStreamOptions(opts speechkit.DictationStreamOptions, resolved ResolvedTranscribeOptions) speechkit.DictationStreamOptions {
	opts.Language = resolved.APILanguage()
	// The stream contract carries the multilanguage sentinel until the concrete
	// adapter maps it; an empty value would let a provider language override win.
	if resolved.DetectLanguage || IsMultilanguage(resolved.Language) {
		opts.Language = LanguageMulti
	}
	opts.PromptHint = resolved.Prompt
	opts.Keyterms = append([]string(nil), resolved.Keyterms...)
	opts.EndpointingMs = resolved.EndpointingMs
	opts.TurnDetection = resolved.Effective.Bool(provideropts.OptionTurnDetection)
	opts.Diarization = resolved.Speaker.WantsDiarization()
	return opts
}

func mergeRecognitionTerms(first, next []string) []string {
	out := make([]string, 0, len(first)+len(next))
	seen := map[string]bool{}
	for _, list := range [][]string{first, next} {
		for _, term := range list {
			term = strings.TrimSpace(term)
			key := strings.ToLower(term)
			if term != "" && !seen[key] {
				out = append(out, term)
				seen[key] = true
			}
		}
	}
	return out
}
