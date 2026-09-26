package assemblyai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Universal-3.5 Pro realtime context limits (docs: streaming context
// carryover). agent_context is capped per value; keyterms_prompt takes up to
// 100 terms of at most 50 characters each.
const (
	assemblyAIStreamingMaxContextChars = 1750
	assemblyAIStreamingMaxKeyterms     = 100
	assemblyAIStreamingMaxKeytermChars = 50
)

// StartSpeakerStream implements [speaker.StreamingProvider] over the v3
// realtime WebSocket with speaker labels on. opts must ask for diarization;
// a single streaming speech model is picked from opts.Model, the provider's
// StreamingModel or the flagship default (batch-only ids are skipped), and
// the expected speaker count bounds max_speakers.
func (p *Provider) StartSpeakerStream(ctx context.Context, opts speaker.Options, format speaker.AudioFormat) (speaker.SpeakerStream, error) {
	opts = opts.Normalized()
	if !opts.WantsDiarization() {
		return nil, fmt.Errorf("assemblyai speaker streaming requires diarization options")
	}
	format = format.Normalized()
	model := assemblyAIStreamingSpeechModel(stt.FirstNonEmptyTrimmed(opts.Model, p.StreamingModel, assemblyAIStreamingModel))
	endpoint, err := p.assemblyAIStreamingEndpoint(model, opts, format)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", p.APIKey)
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: p.client,
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("assemblyai speaker stream dial: %w", err)
	}
	return &assemblyAISpeakerStream{
		conn:     conn,
		provider: p.Name(),
		model:    model,
		opts:     opts,
		openedAt: time.Now(),
	}, nil
}

// StartDictationStream opens a Universal-3.5 Pro realtime session for live
// dictation partials. DictationStreamOptions.PromptHint rides as
// agent_context — the "minimal situational info" (domain, audience, locale
// hints) the model conditions on from the first frame; finalized user turns
// are carried forward by the provider automatically. Finalize sends
// Terminate: the provider flushes the trailing turn, emits Termination, and
// closes the socket (Receive then returns io.EOF), which matches SpeechKit's
// one-provider-stream-per-segment model.
func (p *Provider) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	format = format.Normalized()
	// Rejected before the dial: the v3 realtime API has no channel parameter,
	// so a stereo socket is decoded as mono — the interleaved L,R,L,R frames
	// braid into garbage at twice the true rate. That fails silently and is
	// billed, which is worse than refusing. AssemblyAI's own multichannel
	// guidance is one session per channel, not a parameter.
	if format.Channels != 1 {
		return nil, fmt.Errorf(
			"assemblyai dictation streaming requires mono audio (the v3 realtime API has no channel parameter and decodes the socket as a single channel); got %d channels: %w",
			format.Channels, speechkit.ErrUnsupportedAudioFormat)
	}
	model := assemblyAIStreamingSpeechModel(stt.FirstNonEmptyTrimmed(opts.Model, p.StreamingModel, assemblyAIStreamingModel))
	endpoint, err := p.assemblyAIDictationStreamingEndpoint(model, opts, format)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", p.APIKey)
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: p.client,
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("assemblyai dictation stream dial: %w", err)
	}
	slog.Info("assemblyai dictation stream opened", "speech_model", model)
	return &assemblyAIDictationStream{
		conn:      conn,
		provider:  p.Name(),
		model:     model,
		language:  strings.TrimSpace(opts.Language),
		sessionID: opts.SessionID,
		interim:   opts.InterimResults,
		diarize:   opts.Diarization,
		llm:       p.StreamingLLM != nil && strings.TrimSpace(p.StreamingLLM.Model) != "",
		openedAt:  time.Now(),
	}, nil
}

func (p *Provider) assemblyAIDictationStreamingEndpoint(model string, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (string, error) {
	u, err := p.assemblyAIStreamingURL()
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	q.Set("speech_model", model)
	q.Set("format_turns", "true")
	// Pinned rather than mapped: the protocol admits only linear16/pcm16 and
	// both are S16LE, so there is exactly one value to send. It is also v3's
	// default — setting it explicitly keeps the session honest if that default
	// ever moves. A new non-S16 encoding must revisit this line.
	q.Set("encoding", "pcm_s16le")
	// Native on the v3 realtime API and supported on every streaming model,
	// including universal-3-5-pro — the same param the speaker-stream path
	// sets on this same endpoint. Only when asked: plain dictation must not
	// silently pay for diarization.
	if opts.Diarization {
		q.Set("speaker_labels", "true")
	}
	minSilence := assemblyAIDictationMinTurnSilenceMs
	maxSilence := assemblyAIDictationMaxTurnSilenceMs
	if opts.EndpointingMs > 0 {
		maxSilence = opts.EndpointingMs
	}
	q.Set("min_turn_silence", strconv.Itoa(minSilence))
	q.Set("max_turn_silence", strconv.Itoa(maxSilence))
	if keyterms := assemblyAIStreamingKeyterms(opts.Keyterms); len(keyterms) > 0 {
		encoded, err := json.Marshal(keyterms)
		if err != nil {
			return "", fmt.Errorf("assemblyai keyterms encode: %w", err)
		}
		q.Set("keyterms_prompt", string(encoded))
	}
	if hint := strings.TrimSpace(opts.PromptHint); hint != "" {
		q.Set("agent_context", truncateRunes(hint, assemblyAIStreamingMaxContextChars))
	}
	if encoded := assemblyAILLMGatewayQuery(p.StreamingLLM); encoded != "" {
		q.Set("llm_gateway", encoded)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func assemblyAILLMGatewayQuery(cfg *StreamingLLM) string {
	if cfg == nil {
		return ""
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return ""
	}
	prompt := strings.TrimSpace(cfg.Prompt)
	if prompt == "" {
		prompt = DefaultTurnCleanupPrompt
	}
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 256
	}
	payload, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": maxTokens,
	})
	if err != nil {
		return ""
	}
	return string(payload)
}

// assemblyAIStreamingURL resolves the validated v3 WebSocket base URL.
func (p *Provider) assemblyAIStreamingURL() (*url.URL, error) {
	base := stt.FirstNonEmptyTrimmed(p.StreamingBaseURL, assemblyAIStreamingBaseURL)
	baseForValidation := base
	if strings.HasPrefix(strings.ToLower(baseForValidation), "wss://") {
		baseForValidation = "https://" + strings.TrimPrefix(baseForValidation, "wss://")
	} else if strings.HasPrefix(strings.ToLower(baseForValidation), "ws://") {
		baseForValidation = "http://" + strings.TrimPrefix(baseForValidation, "ws://")
	}
	endpoint, err := netsec.BuildEndpoint(baseForValidation, "v3/ws", p.Validation)
	if err != nil {
		return nil, fmt.Errorf("assemblyai streaming endpoint: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("assemblyai streaming endpoint parse: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return nil, fmt.Errorf("assemblyai streaming endpoint: unsupported scheme %q", u.Scheme)
	}
	return u, nil
}

// assemblyAIStreamingSpeechModel picks a single v3 realtime speech_model.
// Catalog ModelIDs are comma lists ("universal-3-5-pro,universal-2") for the
// batch fallback chain; the streaming handshake rejects that as one token.
func assemblyAIStreamingSpeechModel(requested string) string {
	for _, token := range strings.Split(requested, ",") {
		token = strings.TrimSpace(token)
		if token == "" || assemblyAIBatchOnlySpeechModel(token) {
			continue
		}
		return token
	}
	return assemblyAIStreamingModel
}

func assemblyAIBatchOnlySpeechModel(id string) bool {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "universal-2", "universal-1", "best", "nano":
		return true
	default:
		return false
	}
}

func assemblyAIStreamingKeyterms(terms []string) []string {
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" || len([]rune(term)) > assemblyAIStreamingMaxKeytermChars {
			continue
		}
		out = append(out, term)
		if len(out) >= assemblyAIStreamingMaxKeyterms {
			break
		}
	}
	return out
}

func (p *Provider) assemblyAIStreamingEndpoint(model string, opts speaker.Options, format speaker.AudioFormat) (string, error) {
	u, err := p.assemblyAIStreamingURL()
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	q.Set("format_turns", "true")
	q.Set("speaker_labels", "true")
	q.Set("speech_model", model)
	if maxSpeakers := stt.MaxSpeakers(opts); maxSpeakers > 0 {
		q.Set("max_speakers", strconv.Itoa(maxSpeakers))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
