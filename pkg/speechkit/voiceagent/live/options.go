package live

import (
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// ResolvedLiveOptions is the per-session view of the provider option layers
// after [ResolveLiveOptions]: the effective locale, voice, context prompt,
// language hints, key terms, reasoning effort and resume flag, plus the turn
// detection and endpointing choices with the layer that set them, so a
// provider can tell an explicit setting from its own default. EndpointingMs
// is in milliseconds and zero when unset. Effective keeps the full
// [provideropts.EffectiveOptions], including unsupported-option reports.
type ResolvedLiveOptions struct {
	Locale              string
	Voice               string
	ContextPrompt       string
	LanguageHints       []string
	Keyterms            []string
	ReasoningEffort     string
	Resume              bool
	TurnDetection       bool
	TurnDetectionSource provideropts.ValueSource
	EndpointingMs       int
	EndpointingSource   provideropts.ValueSource
	Effective           provideropts.EffectiveOptions
}

// ResolveLiveOptions merges the option layers for provider's voice-agent
// manifest, lowest precedence first: providerDefaults, cfg.Options,
// providerOverrides merged with cfg.ProviderOptions, and a request layer
// built from cfg.Locale, cfg.Voice and, when no key terms were configured
// elsewhere, cfg.VocabularyHint split into terms. profileID is echoed into
// the result. Options the provider cannot honour are logged at debug level
// and listed in Effective.Unsupported; a provider without a manifest gets an
// empty one.
func ResolveLiveOptions(provider, profileID string, cfg LiveConfig, providerDefaults, providerOverrides provideropts.Values) ResolvedLiveOptions {
	manifest, ok := provideropts.FindManifest(provider, provideropts.ModalityVoiceAgent)
	if !ok {
		manifest = provideropts.ProviderOptionManifest{
			Schema:   provideropts.SchemaProviderOptions,
			Updated:  provideropts.ManifestUpdated,
			Provider: provider,
			Modality: provideropts.ModalityVoiceAgent,
		}
	}
	mergedProviderOverrides := providerOverrides.Clone()
	mergedProviderOverrides = mergedProviderOverrides.Merge(cfg.ProviderOptions)
	request := provideropts.Values{}
	if locale := strings.TrimSpace(cfg.Locale); locale != "" {
		request[provideropts.OptionLanguage] = locale
	}
	if voice := strings.TrimSpace(cfg.Voice); voice != "" {
		request[provideropts.OptionVoice] = voice
	}
	if len(mergedProviderOverrides.StringList(provideropts.OptionKeyterms)) == 0 &&
		len(cfg.Options.StringList(provideropts.OptionKeyterms)) == 0 {
		if keyterms := splitOptionTerms(cfg.VocabularyHint); len(keyterms) > 0 {
			request[provideropts.OptionKeyterms] = keyterms
		}
	}
	effective := provideropts.Resolve(provideropts.ResolveInput{
		Manifest:          manifest,
		ProfileID:         profileID,
		ProviderDefaults:  providerDefaults,
		GlobalDefaults:    cfg.Options.Clone(),
		ProviderOverrides: mergedProviderOverrides,
		RequestOverrides:  request,
	})
	logUnsupportedOptions(effective.Unsupported)
	turnDetectionSource := provideropts.SourceUnset
	if opt, ok := effective.Options[provideropts.OptionTurnDetection]; ok {
		turnDetectionSource = opt.Source
	}
	endpointingSource := provideropts.SourceUnset
	if opt, ok := effective.Options[provideropts.OptionEndpointingMs]; ok {
		endpointingSource = opt.Source
	}
	return ResolvedLiveOptions{
		Locale:              effective.String(provideropts.OptionLanguage),
		Voice:               effective.String(provideropts.OptionVoice),
		ContextPrompt:       effective.String(provideropts.OptionContextPrompt),
		LanguageHints:       effective.StringList(provideropts.OptionLanguageHints),
		Keyterms:            effective.StringList(provideropts.OptionKeyterms),
		ReasoningEffort:     effective.String(provideropts.OptionReasoningEffort),
		Resume:              effective.Bool(provideropts.OptionResume),
		TurnDetection:       effective.Bool(provideropts.OptionTurnDetection),
		TurnDetectionSource: turnDetectionSource,
		EndpointingMs:       effective.Int(provideropts.OptionEndpointingMs),
		EndpointingSource:   endpointingSource,
		Effective:           effective,
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

// HasTurnDetectionOverride reports whether turn detection was set by a host
// layer (global, provider or request) rather than left unset or to the
// provider default.
func (r ResolvedLiveOptions) HasTurnDetectionOverride() bool {
	return r.TurnDetectionSource != provideropts.SourceUnset &&
		r.TurnDetectionSource != provideropts.SourceProviderDefault
}

// HasEndpointingOverride reports whether endpointing was set by a host layer
// rather than left unset or to the provider default.
func (r ResolvedLiveOptions) HasEndpointingOverride() bool {
	return r.EndpointingSource != provideropts.SourceUnset &&
		r.EndpointingSource != provideropts.SourceProviderDefault
}

func splitOptionTerms(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lower := strings.ToLower(raw)
	if strings.Contains(raw, ":") || strings.ContainsAny(raw, ".!?") ||
		strings.Contains(lower, "prefer ") ||
		strings.Contains(lower, "recognition") ||
		strings.Contains(lower, "transcribing") ||
		strings.Contains(lower, "responses") {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := strings.ToLower(part)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, part)
	}
	return out
}

// AppendContextPrompt appends contextPrompt to base under a "Context:"
// heading. Both are trimmed; when either is empty the other is returned.
func AppendContextPrompt(base, contextPrompt string) string {
	base = strings.TrimSpace(base)
	contextPrompt = strings.TrimSpace(contextPrompt)
	if contextPrompt == "" {
		return base
	}
	if base == "" {
		return contextPrompt
	}
	return base + "\n\nContext:\n" + contextPrompt
}
