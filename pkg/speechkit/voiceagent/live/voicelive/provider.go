// Package voicelive adapts the OpenAI Realtime provider to Microsoft Foundry's
// Voice Live API. Voice Live serves a superset of the OpenAI Realtime
// WebSocket protocol from the Foundry resource host
// (wss://<account-host>/voice-live/realtime?api-version=<v>&model=<brain>)
// and adds Azure-only session fields: Azure and MAI voices, a transcription
// model choice (MAI-Transcribe), semantic VAD, deep noise suppression and
// echo cancellation. This package reuses the live/openai connection loop,
// audio path and event parser and only swaps the dial URL, the credential
// header and the session.update shape.
//
// Auth is either the resource key in the api-key header (cfg.APIKey) or a
// short-lived Entra token from cfg.BearerToken sent as
// "Authorization: Bearer"; the token source wins when both are set.
package voicelive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/text/language"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

const (
	// DefaultModel is the default brain model. Voice Live addresses models
	// by name in the query string, not by deployment.
	DefaultModel = "gpt-realtime-2"
	// DefaultAPIVersion is the Voice Live API version the how-to guide
	// documents (2026-04-10); the handshake was verified live against it and
	// against the previous 2025-10-01 on 2026-09-05. APIVersion overrides it.
	DefaultAPIVersion = "2026-04-10"
	// DefaultTranscriptionModel transcribes user speech for the kernel's
	// input transcripts.
	DefaultTranscriptionModel = "mai-transcribe-2"
	// DefaultVoice is used when cfg.Voice is empty.
	DefaultVoice = "en-US-Harper:MAI-Voice-2-Flash"
	// DefaultVoiceType is the Voice Live voice type for Azure and MAI voices.
	DefaultVoiceType = "azure-standard"
	// ProfileID is the public realtime profile this provider serves.
	ProfileID = "realtime.foundry.voice-live"

	// Voice Live streams 24 kHz 16-bit PCM in both directions, the same
	// format as OpenAI Realtime, so the embedded provider's mic upsample and
	// the kernel's output contract apply unchanged.
	audioFormatPCM16 = "pcm16"
)

// Provider implements live.LiveProvider against Microsoft Foundry Voice Live
// by delegating the OpenAI Realtime protocol to live/openai. The exported
// fields tune the Azure-only session shape; set them before Connect.
type Provider struct {
	*openai.Provider

	// APIVersion is the api-version query parameter. Empty uses
	// DefaultAPIVersion.
	APIVersion string
	// TranscriptionModel transcribes user speech (input_audio_transcription).
	// New sets DefaultTranscriptionModel so the kernel sees transcripts by
	// default; empty falls back to the policy switch
	// cfg.Policies.EnableInputAudioTranscription.
	TranscriptionModel string
	// VoiceType is the Voice Live voice type. Empty uses DefaultVoiceType.
	VoiceType string
	// VoiceStyle is an optional speaking style (e.g. "whispering").
	VoiceStyle string
	// VoiceRate is an optional relative speaking rate (e.g. "+10%").
	VoiceRate string
	// SemanticVAD selects azure_semantic_vad for automatic turn detection;
	// false sends the OpenAI-style server_vad mapped from the policies. New
	// enables it.
	SemanticVAD bool
	// NoiseSuppression enables azure_deep_noise_suppression. New enables it.
	NoiseSuppression bool
	// EchoCancellation enables server_echo_cancellation. New enables it.
	EchoCancellation bool
}

// New returns a Voice Live provider with the defaults documented on the
// fields: semantic VAD, noise suppression, echo cancellation and
// MAI-Transcribe-2 transcription.
func New() *Provider {
	p := &Provider{
		Provider:           openai.New(),
		APIVersion:         DefaultAPIVersion,
		TranscriptionModel: DefaultTranscriptionModel,
		VoiceType:          DefaultVoiceType,
		SemanticVAD:        true,
		NoiseSuppression:   true,
		EchoCancellation:   true,
	}
	p.Provider.DialURL = p.dialURL
	p.Provider.DialHeaders = dialHeaders
	p.Provider.BuildSession = p.buildSession
	return p
}

// Name identifies the provider in Voice Agent logs.
func (p *Provider) Name() string { return "foundry-voicelive" }

func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider("foundry")
}

// Connect requires the Voice Live endpoint
// (wss://<account-host>/voice-live/realtime, no query) in cfg.Endpoint and a
// credential, then dials via the embedded OpenAI Realtime implementation.
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return fmt.Errorf("foundry voice live: %w (derive it from the Foundry project endpoint, e.g. wss://<account-host>/voice-live/realtime)", live.ErrMissingEndpoint)
	}
	if strings.TrimSpace(cfg.APIKey) == "" && cfg.BearerToken == nil {
		return fmt.Errorf("foundry voice live: %w (set APIKey to the resource key or BearerToken to an Entra token source)", live.ErrMissingAPIKey)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultModel
	}
	return p.Provider.Connect(ctx, cfg)
}

// dialURL appends the Voice Live api-version and brain model to the host's
// endpoint. An endpoint that already carries a query keeps it.
func (p *Provider) dialURL(baseURL, model string) string {
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%sapi-version=%s&model=%s", baseURL, sep, url.QueryEscape(p.apiVersion()), url.QueryEscape(model))
}

func (p *Provider) apiVersion() string {
	if v := strings.TrimSpace(p.APIVersion); v != "" {
		return v
	}
	return DefaultAPIVersion
}

// dialHeaders sends an Entra bearer token when the host supplies a token
// source and the resource key in the api-key header otherwise.
func dialHeaders(ctx context.Context, cfg live.LiveConfig) (http.Header, error) {
	header := http.Header{}
	if cfg.BearerToken != nil {
		token, err := cfg.BearerToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("bearer token: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("bearer token: empty token")
		}
		header.Set("Authorization", "Bearer "+token)
		return header, nil
	}
	header.Set("api-key", cfg.APIKey)
	return header, nil
}

// buildSession builds the flat Voice Live session object. The brain model is
// server-owned (it comes from the dial query), so it is not repeated here.
func (p *Provider) buildSession(cfg live.LiveConfig, _ string, instructions string) map[string]any {
	resolved := live.ResolveLiveOptions("openai", ProfileID, cfg, nil, nil)
	session := map[string]any{
		"modalities":          []string{"text", "audio"},
		"instructions":        instructions,
		"voice":               p.voice(firstNonEmpty(resolved.Voice, cfg.Voice, DefaultVoice)),
		"input_audio_format":  audioFormatPCM16,
		"output_audio_format": audioFormatPCM16,
		// Voice Live defaults to server_vad, so push-to-talk has to disable
		// turn detection explicitly with null rather than by omission.
		"turn_detection": p.turnDetection(cfg, resolved),
	}
	if transcription := p.inputTranscription(cfg, resolved); transcription != nil {
		session["input_audio_transcription"] = transcription
	}
	if p.NoiseSuppression {
		session["input_audio_noise_reduction"] = map[string]any{"type": "azure_deep_noise_suppression"}
	}
	if p.EchoCancellation {
		session["input_audio_echo_cancellation"] = map[string]any{"type": "server_echo_cancellation"}
	}
	if tools := openai.BuildTools(cfg.Tools); len(tools) > 0 {
		session["tools"] = tools
		session["tool_choice"] = "auto"
	}
	return session
}

func (p *Provider) voice(name string) map[string]any {
	voice := map[string]any{
		"name": name,
		"type": firstNonEmpty(p.VoiceType, DefaultVoiceType),
	}
	if style := strings.TrimSpace(p.VoiceStyle); style != "" {
		voice["style"] = style
	}
	if rate := strings.TrimSpace(p.VoiceRate); rate != "" {
		voice["rate"] = rate
	}
	return voice
}

// turnDetection maps the kernel's activity policy (after option overrides)
// to Voice Live turn detection. nil is push-to-talk. Semantic VAD keeps the
// server_vad numeric mapping (threshold, padding, silence) and adds the
// Azure-only switches.
func (p *Provider) turnDetection(cfg live.LiveConfig, resolved live.ResolvedLiveOptions) map[string]any {
	activity := openai.ResolveActivityDetection(cfg.Policies.ActivityDetection, resolved)
	td := openai.BuildTurnDetection(activity)
	if td == nil {
		return nil
	}
	if p.SemanticVAD {
		td["type"] = "azure_semantic_vad"
		td["remove_filler_words"] = false
	}
	return td
}

// inputTranscription is on whenever a transcription model is configured
// (the New default) or the kernel policy asks for input transcripts.
func (p *Provider) inputTranscription(cfg live.LiveConfig, resolved live.ResolvedLiveOptions) map[string]any {
	model := strings.TrimSpace(p.TranscriptionModel)
	if model == "" {
		if !cfg.Policies.EnableInputAudioTranscription {
			return nil
		}
		model = DefaultTranscriptionModel
	}
	transcription := map[string]any{"model": model}
	if lang := transcriptionLanguage(firstNonEmpty(resolved.Locale, cfg.Locale)); lang != "" {
		transcription["language"] = lang
	}
	if len(resolved.Keyterms) > 0 {
		transcription["phrase_list"] = append([]string(nil), resolved.Keyterms...)
	}
	return transcription
}

// transcriptionLanguage turns the kernel locale into the language-region tag
// Voice Live transcription expects ("de-DE"). A bare language gets its most
// likely region ("de" to "de-DE", "en" to "en-US"); auto-detect markers and
// unparseable values leave the language unset so the server detects it.
func transcriptionLanguage(locale string) string {
	locale = strings.TrimSpace(locale)
	switch strings.ToLower(locale) {
	case "", "auto", "multi", "und":
		return ""
	}
	tag, err := language.Parse(locale)
	if err != nil {
		return ""
	}
	base, confidence := tag.Base()
	if confidence == language.No {
		return ""
	}
	region, confidence := tag.Region()
	if confidence == language.No {
		return base.String()
	}
	return base.String() + "-" + region.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Compile-time assertions that Provider satisfies the kernel interfaces.
var (
	_ live.LiveProvider            = (*Provider)(nil)
	_ live.LiveInstructionUpdater  = (*Provider)(nil)
	_ live.LiveSessionCapabilities = (*Provider)(nil)
)
