package deepgram

import (
	"log/slog"
	"math"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// buildSettings assembles the Voice Agent Settings message: audio formats plus
// the listen/think/speak providers, the system prompt, and any tool functions.
// It is pure (no I/O) so the listen/think/speak wiring can be unit-tested.
func (p *Provider) buildSettings(cfg live.LiveConfig) map[string]any {
	resolved := live.ResolveLiveOptions("deepgram", "realtime.deepgram.voice-agent", cfg, nil, nil)
	think := map[string]any{
		"provider": map[string]any{
			"type":  dgFirst(p.ThinkProvider, deepgramThinkProviderDefault),
			"model": dgFirst(p.ThinkModel, deepgramThinkModelDefault),
		},
	}
	if endpoint := p.thinkEndpoint(); endpoint != nil {
		think["endpoint"] = endpoint
	}
	if prompt := live.AppendContextPrompt(composeDeepgramPrompt(cfg), resolved.ContextPrompt); prompt != "" {
		think["prompt"] = prompt
	}
	if funcs := buildDeepgramFunctions(cfg.Tools); len(funcs) > 0 {
		think["functions"] = funcs
	}

	listenModel := dgFirst(p.ListenModel, deepgramListenModelDefault)
	listenProvider := map[string]any{
		"type":  "deepgram",
		"model": listenModel,
	}
	// VocabularyHint boosts recognition of domain terms — Nova-3 exposes this
	// as keyterms on the listen provider.
	if keyterms := resolved.Keyterms; len(keyterms) > 0 {
		listenProvider["keyterms"] = keyterms
	}
	hints := deepgramListenLanguageHints(resolved.LanguageHints, resolved.Locale, listenModel)
	if len(hints) > 0 {
		listenProvider["language_hints"] = hints
	}
	if deepgramModelUsesFlux(listenModel) {
		// Flux runs on the Voice Agent API's v2 listen leg; the version field is
		// required for it and invalid for the Nova (v1) models. Turn detection is
		// model-integrated, so the thresholds only exist on this path.
		listenProvider["version"] = "v2"
		if v, ok := dgClamp(p.EOTThreshold, deepgramEOTThresholdMin, deepgramEOTThresholdMax); ok {
			listenProvider["eot_threshold"] = v
		}
		if v, ok := dgClamp(p.EagerEOTThreshold, deepgramEagerEOTThresholdMin, deepgramEagerEOTThresholdMax); ok {
			listenProvider["eager_eot_threshold"] = v
		}
		if v, ok := dgClamp(float64(p.EOTTimeoutMs), deepgramEOTTimeoutMinMs, deepgramEOTTimeoutMaxMs); ok {
			listenProvider["eot_timeout_ms"] = int(v)
		}
	}

	agent := map[string]any{
		"listen": map[string]any{"provider": listenProvider},
		"think":  think,
		"speak":  p.buildSpeak(resolved.Locale, resolved.Voice, hints),
	}
	if lang := deepgramAgentLanguage(resolved.Locale); lang != "" && !deepgramModelUsesFlux(listenModel) {
		agent["language"] = lang
	}

	return map[string]any{
		"type": "Settings",
		"audio": map[string]any{
			"input": map[string]any{
				"encoding":    "linear16",
				"sample_rate": deepgramAgentInputSampleRate,
			},
			"output": map[string]any{
				"encoding":    "linear16",
				"sample_rate": deepgramAgentOutputSampleRate,
				"container":   "none",
			},
		},
		"agent": agent,
	}
}

// thinkEndpoint returns the agent.think.endpoint block for a bring-your-own
// think LLM, or nil to use Deepgram's managed model. Per the Deepgram Voice
// Agent Settings schema, a BYO credential travels as an Authorization header on
// the endpoint — the provider object itself carries no key.
func (p *Provider) thinkEndpoint() map[string]any {
	url := strings.TrimSpace(p.ThinkEndpointURL)
	if url == "" {
		return nil
	}
	endpoint := map[string]any{"url": url}
	if key := strings.TrimSpace(p.ThinkAPIKey); key != "" {
		endpoint["headers"] = map[string]any{"authorization": "Bearer " + key}
	}
	return endpoint
}

// buildSpeak assembles agent.speak. An Aura voice stays a single provider
// object; a Flux TTS voice travels on the v2 leg and is emitted as the array
// form, with the locale's Aura-2 voice as the second entry so Deepgram falls
// back if the Flux leg is unavailable. Speak is always sent explicitly —
// omitting it makes Deepgram default to Flux TTS server-side, which would
// silently break non-English sessions.
func (p *Provider) buildSpeak(locale, voice string, hints []string) any {
	speakModel := p.resolveSpeakModel(locale, voice, hints)
	aura := map[string]any{
		"provider": map[string]any{"type": "deepgram", "model": deepgramAuraModelForLocale(locale)},
	}
	if !deepgramModelUsesFlux(speakModel) {
		return map[string]any{
			"provider": map[string]any{"type": "deepgram", "model": speakModel},
		}
	}
	flux := map[string]any{"type": "deepgram", "version": "v2", "model": speakModel}
	if speed := deepgramFluxSpeakSpeed(p.SpeakSpeed); speed > 0 {
		flux["speed"] = speed
	}
	return []any{map[string]any{"provider": flux}, aura}
}

// resolveSpeakModel picks the Deepgram voice for a session. An explicit voice
// wins over the configured SpeakModel, which wins over the locale default.
//
// Flux TTS voices are English-only, while a Flux listen session can code-switch
// mid-call, so a Flux voice is honoured only when the session is English-pinned:
// an English (or unset) locale and no non-English language hint. Anything else
// falls back to the Aura-2 voice for the locale. Deepgram's speak fallback array
// cannot cover this — it fires on provider failure, not on a successful
// synthesis in the wrong language.
func (p *Provider) resolveSpeakModel(locale, voice string, hints []string) string {
	selected := ""
	if v := strings.TrimSpace(voice); deepgramIsSpeakVoice(v) {
		selected = v
	} else if p.SpeakModel != "" {
		selected = strings.TrimSpace(p.SpeakModel)
	}
	if selected == "" {
		return deepgramAuraModelForLocale(locale)
	}
	if deepgramModelUsesFlux(selected) && !deepgramSessionIsEnglishPinned(locale, hints) {
		fallback := deepgramAuraModelForLocale(locale)
		slog.Warn("deepgram agent: Flux TTS is English-only; using Aura-2 for this session",
			"requested_voice", selected, "locale", locale, "language_hints", hints, "speak_model", fallback)
		return fallback
	}
	return selected
}

// deepgramIsSpeakVoice reports whether a configured voice names a Deepgram TTS
// model (Aura or Flux) rather than another provider's voice id.
func deepgramIsSpeakVoice(voice string) bool {
	lower := strings.ToLower(strings.TrimSpace(voice))
	return strings.HasPrefix(lower, "aura") || strings.HasPrefix(lower, "flux")
}

func deepgramAuraModelForLocale(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "de") {
		return deepgramSpeakModelDefaultDE
	}
	return deepgramSpeakModelDefaultEN
}

// deepgramSessionIsEnglishPinned reports whether every language the session can
// produce is English. An empty locale and empty hints count as pinned: that is
// the framework's English default, not an unknown language.
func deepgramSessionIsEnglishPinned(locale string, hints []string) bool {
	isEnglish := func(value string) bool {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return true
		}
		if value == "auto" || value == "multi" {
			return false
		}
		if idx := strings.IndexAny(value, "-_"); idx > 0 {
			value = value[:idx]
		}
		return value == "en"
	}
	if !isEnglish(locale) {
		return false
	}
	for _, hint := range hints {
		if !isEnglish(hint) {
			return false
		}
	}
	return true
}

// deepgramFluxSpeakSpeed snaps a configured speed onto the discrete steps Flux
// TTS accepts (0.85–1.15 in 0.05 increments). Out-of-range values clamp to the
// nearest bound; 0 means "unset" and returns 0 so the field is omitted.
func deepgramFluxSpeakSpeed(speed float64) float64 {
	if speed <= 0 {
		return 0
	}
	steps := []float64{0.85, 0.9, 0.95, 1.0, 1.05, 1.1, 1.15}
	best := steps[0]
	for _, step := range steps {
		if math.Abs(step-speed) < math.Abs(best-speed) {
			best = step
		}
	}
	return best
}

// dgClamp reports whether an optional numeric tuning value was set (> 0) and
// returns it clamped into the provider's accepted range.
func dgClamp(value, lower, upper float64) (float64, bool) {
	if value <= 0 {
		return 0, false
	}
	if value < lower {
		return lower, true
	}
	if value > upper {
		return upper, true
	}
	return value, true
}

// composeDeepgramPrompt merges the framework + refinement prompts into the
// single system prompt Deepgram's think leg accepts (agent.think.prompt).
func composeDeepgramPrompt(cfg live.LiveConfig) string {
	prompt := strings.TrimSpace(cfg.FrameworkPrompt)
	if refinement := strings.TrimSpace(cfg.RefinementPrompt); refinement != "" {
		if prompt == "" {
			prompt = refinement
		} else {
			prompt = prompt + "\n\n" + refinement
		}
	}
	return prompt
}

// deepgramAgentLanguage reduces a SpeechKit locale ("de-DE", "en-US") to the
// two-letter language code Deepgram's agent.language expects. "auto"/empty
// returns "" so the field is omitted.
func deepgramAgentLanguage(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" || locale == "auto" {
		return ""
	}
	if idx := strings.IndexAny(locale, "-_"); idx > 0 {
		return locale[:idx]
	}
	return locale
}

// deepgramModelUsesFlux reports whether a listen or speak model names a Flux
// model ("flux-general-multi", "flux-kit-en", …). Both legs move to the
// provider's v2 API when it does.
func deepgramModelUsesFlux(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "flux-")
}

func deepgramListenLanguageHints(hints []string, locale, listenModel string) []string {
	out := make([]string, 0, len(hints)+1)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || value == "auto" {
			return
		}
		if idx := strings.IndexAny(value, "-_"); idx > 0 {
			value = value[:idx]
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, hint := range hints {
		add(hint)
	}
	if deepgramModelUsesFlux(listenModel) {
		add(locale)
	}
	return out
}

// buildDeepgramFunctions translates kernel ToolDefinitions into the
// agent.think.functions array.
func buildDeepgramFunctions(defs []live.ToolDefinition) []map[string]any {
	if len(defs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		entry := map[string]any{
			"name":        def.Name,
			"description": def.Description,
		}
		if def.ParametersJSONSchema != nil {
			entry["parameters"] = def.ParametersJSONSchema
		}
		out = append(out, entry)
	}
	return out
}

func dgFirst(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
