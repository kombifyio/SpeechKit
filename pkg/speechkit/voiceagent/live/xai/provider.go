// Package xai adapts xAI's Grok Voice Agent API (speech-to-speech over
// wss://api.x.ai/v1/realtime?model=grok-voice-3.0) to [live.LiveProvider].
//
// The API follows the OpenAI Realtime event protocol (session.update,
// input_audio_buffer.*, response.output_audio.delta,
// response.function_call_arguments.done, conversation.item.create with a
// function_call_output, response.create, response.done, error), so this
// package wraps live/openai and only changes identity, endpoint, default
// model, the session shape and the cumulative input transcript event. Auth is
// the xAI API key (XAI_API_KEY) sent as a Bearer token.
//
// Wire names follow https://docs.x.ai/developers/model-capabilities/audio/speech-to-speech.
//
// Stability: Experimental — may change in any release.
package xai

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

const (
	// DefaultModel is the Grok Voice model dialled when cfg.Model is empty.
	// grok-voice-latest aliases it; grok-voice-2.0 is deprecated.
	DefaultModel = "grok-voice-3.0"
	// DefaultEndpoint is the xAI realtime WebSocket; the model goes into the
	// model query parameter.
	DefaultEndpoint = "wss://api.x.ai/v1/realtime"
	// ProfileID names this provider's sessions in [live.SessionCapabilities].
	ProfileID = "realtime.xai.grok-voice"
	// DefaultVoice is used when the configured voice is not a Grok voice.
	DefaultVoice = "eve"

	providerID = "xai"
	// sampleRate is requested for both directions: it is the API default and
	// the kernel's 24 kHz LiveMessage.Audio contract, and the embedded
	// Realtime loop upsamples the 16 kHz mic to it.
	sampleRate = 24000
	// transcriptionModel enables the cumulative
	// conversation.item.input_audio_transcription.updated events.
	transcriptionModel = "grok-transcribe"
)

// Voices are the built-in Grok voice ids (https://x.ai/api/voice).
var Voices = []string{
	"ara", "eve", "leo", "rex", "sal", "altair", "atlas", "aurora", "carina",
	"castor", "celeste", "cosmo", "helios", "helix", "iris", "kepler", "liora",
	"lumen", "luna", "lux", "naksh", "orion", "perseus", "rigel", "sirius",
	"ursa", "zagan", "zenith",
}

// Provider implements live.LiveProvider against the Grok Voice Agent API by
// delegating the Realtime-compatible protocol to live/openai.
type Provider struct {
	*openai.Provider

	mu         sync.Mutex
	itemID     string
	transcript string
}

// New returns a fresh Grok Voice provider.
func New() *Provider {
	p := &Provider{Provider: openai.New()}
	p.BuildSession = buildSession
	p.ParseEvent = p.parseEvent
	return p
}

// Name identifies the provider in Voice Agent logs.
func (p *Provider) Name() string { return "xai-grok-voice" }

// SessionCapabilities reports the profile, default model and capability
// flags of the xAI catalog descriptor.
func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider(providerID)
}

// Connect dials grok-voice-3.0 (or cfg.Model) on the xAI endpoint (or
// cfg.Endpoint) through the embedded Realtime implementation.
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" && cfg.BearerToken == nil {
		return fmt.Errorf("xai grok voice: %w", live.ErrMissingAPIKey)
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = DefaultModel
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	p.mu.Lock()
	p.itemID, p.transcript = "", ""
	p.mu.Unlock()
	return p.Provider.Connect(ctx, cfg)
}

// buildSession builds the Grok session.update object: voice and turn
// detection sit at the session's top level, audio formats carry an explicit
// rate, and reasoning.effort accepts only "high" or "none".
func buildSession(cfg live.LiveConfig, _ string, instructions string) map[string]any {
	resolved := live.ResolveLiveOptions(providerID, ProfileID, cfg, nil, nil)
	format := map[string]any{"type": "audio/pcm", "rate": sampleRate}
	input := map[string]any{"format": format}
	if cfg.Policies.EnableInputAudioTranscription {
		input["transcription"] = map[string]any{"model": transcriptionModel}
	}
	session := map[string]any{
		"voice": voice(firstNonEmpty(resolved.Voice, cfg.Voice)),
		"audio": map[string]any{
			"input":  input,
			"output": map[string]any{"format": format},
		},
	}
	if instructions != "" {
		session["instructions"] = instructions
	}
	// Grok's server VAD takes no threshold or silence tuning; nil keeps
	// manual turns, which the embedded loop commits on audio end.
	if openai.BuildTurnDetection(openai.ResolveActivityDetection(cfg.Policies.ActivityDetection, resolved)) != nil {
		session["turn_detection"] = map[string]any{"type": "server_vad"}
	} else {
		session["turn_detection"] = nil
	}
	if tools := openai.BuildTools(cfg.Tools); len(tools) > 0 {
		session["tools"] = tools
	}
	if effort := reasoningEffort(resolved.ReasoningEffort); effort != "" {
		session["reasoning"] = map[string]any{"effort": effort}
	}
	return session
}

// parseEvent maps the Grok-only cumulative input transcript event: each
// update carries the whole transcript so far and may correct earlier words.
// The kernel receives the new suffix as a partial transcript; the full text
// rides along in ProviderMetadata["transcript"] for consumers that need the
// corrected utterance.
func (p *Provider) parseEvent(eventType string, data []byte) (*live.LiveMessage, bool, error) {
	switch eventType {
	case "conversation.item.input_audio_transcription.updated":
		var ev struct {
			ItemID     string `json:"item_id"`
			Transcript string `json:"transcript"`
			Delta      string `json:"delta"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, true, fmt.Errorf("xai grok voice: decode transcription update: %w", err)
		}
		full := firstNonEmpty(ev.Transcript, ev.Delta)
		if full == "" {
			return nil, true, nil
		}
		p.mu.Lock()
		if ev.ItemID != p.itemID {
			p.itemID, p.transcript = ev.ItemID, ""
		}
		delta := full
		if strings.HasPrefix(full, p.transcript) {
			delta = strings.TrimPrefix(full, p.transcript)
		}
		p.transcript = full
		p.mu.Unlock()
		if strings.TrimSpace(delta) == "" {
			return nil, true, nil
		}
		msg := live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInputPartial, InputTranscript: delta}, eventType)
		msg.ProviderMetadata["transcript"] = full
		if ev.ItemID != "" {
			msg.ProviderMetadata["item_id"] = ev.ItemID
		}
		return msg, true, nil
	case "response.done":
		// A finished response ends the transcribed user item; the next
		// update starts a fresh cumulative transcript.
		p.mu.Lock()
		p.itemID, p.transcript = "", ""
		p.mu.Unlock()
	}
	return nil, false, nil
}

func voice(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if slices.Contains(Voices, name) {
		return name
	}
	return DefaultVoice
}

func reasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "":
		return ""
	case "none", "minimal", "low":
		return "none"
	default:
		return "high"
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

var (
	_ live.LiveProvider           = (*Provider)(nil)
	_ live.LiveInstructionUpdater = (*Provider)(nil)
)
