package tts

import (
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// ResolvedSynthesizeOptions is the provider-neutral view of one synthesis
// request after [ResolveSynthesizeOptions] has layered every option source.
// The scalar fields are the effective language, voice, speed multiplier and
// container format (empty or zero when nothing set them); Effective keeps the
// full option set with the source each value came from.
type ResolvedSynthesizeOptions struct {
	Locale    string
	Voice     string
	Speed     float64
	Format    string
	Effective provideropts.EffectiveOptions
}

// ResolveSynthesizeOptions resolves the options for one request against the
// provider's TTS option manifest (an empty manifest when none is registered;
// profileID is only stamped on the result). Precedence, lowest to highest:
// providerDefaults, opts.Options, providerOverrides merged with
// opts.ProviderOptions, then the request's own Locale, Voice, Speed (when
// positive) and Format. Options the manifest does not support are kept but
// reported at debug level.
func ResolveSynthesizeOptions(provider, profileID string, opts SynthesizeOpts, providerDefaults, providerOverrides provideropts.Values) ResolvedSynthesizeOptions {
	manifest, ok := provideropts.FindManifest(provider, provideropts.ModalityTTS)
	if !ok {
		manifest = provideropts.ProviderOptionManifest{
			Schema:   provideropts.SchemaProviderOptions,
			Updated:  provideropts.ManifestUpdated,
			Provider: provider,
			Modality: provideropts.ModalityTTS,
		}
	}
	request := provideropts.Values{}
	if locale := strings.TrimSpace(opts.Locale); locale != "" {
		request[provideropts.OptionLanguage] = locale
	}
	if voice := strings.TrimSpace(opts.Voice); voice != "" {
		request[provideropts.OptionVoice] = voice
	}
	if opts.Speed > 0 {
		request[provideropts.OptionSpeed] = opts.Speed
	}
	if format := strings.TrimSpace(opts.Format); format != "" {
		request[provideropts.OptionAudioFormat] = format
	}
	mergedProviderOverrides := providerOverrides.Clone()
	mergedProviderOverrides = mergedProviderOverrides.Merge(opts.ProviderOptions)
	effective := provideropts.Resolve(provideropts.ResolveInput{
		Manifest:          manifest,
		ProfileID:         profileID,
		ProviderDefaults:  providerDefaults,
		GlobalDefaults:    opts.Options.Clone(),
		ProviderOverrides: mergedProviderOverrides,
		RequestOverrides:  request,
	})
	logUnsupportedOptions(effective.Unsupported)
	return ResolvedSynthesizeOptions{
		Locale:    effective.String(provideropts.OptionLanguage),
		Voice:     effective.String(provideropts.OptionVoice),
		Speed:     effective.Float(provideropts.OptionSpeed),
		Format:    effective.String(provideropts.OptionAudioFormat),
		Effective: effective,
	}
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
