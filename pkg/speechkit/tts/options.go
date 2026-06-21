package tts

import (
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

type ResolvedSynthesizeOptions struct {
	Locale    string
	Voice     string
	Speed     float64
	Format    string
	Effective provideropts.EffectiveOptions
}

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
