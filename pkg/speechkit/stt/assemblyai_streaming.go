package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Universal-3.5 Pro realtime context limits (docs: streaming context
// carryover). agent_context is capped per value; keyterms_prompt takes up to
// 100 terms of at most 50 characters each.
const (
	assemblyAIStreamingMaxContextChars = 1750
	assemblyAIStreamingMaxKeyterms     = 100
	assemblyAIStreamingMaxKeytermChars = 50
)

type assemblyAISpeakerStream struct {
	conn      *websocket.Conn
	provider  string
	model     string
	opts      speaker.Options
	openedAt  time.Time
	sequence  atomic.Int64
	closeOnce atomic.Bool
}

func (p *AssemblyAIProvider) StartSpeakerStream(ctx context.Context, opts speaker.Options, format speaker.AudioFormat) (speaker.SpeakerStream, error) {
	opts = opts.Normalized()
	if !opts.WantsDiarization() {
		return nil, fmt.Errorf("assemblyai speaker streaming requires diarization options")
	}
	format = format.Normalized()
	model := assemblyAIStreamingSpeechModel(firstNonEmptyTrimmed(opts.Model, p.StreamingModel, assemblyAIStreamingModel))
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
func (p *AssemblyAIProvider) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
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
	model := assemblyAIStreamingSpeechModel(firstNonEmptyTrimmed(opts.Model, p.StreamingModel, assemblyAIStreamingModel))
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

func (p *AssemblyAIProvider) assemblyAIDictationStreamingEndpoint(model string, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (string, error) {
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

func assemblyAILLMGatewayQuery(cfg *AssemblyAIStreamingLLM) string {
	if cfg == nil {
		return ""
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return ""
	}
	prompt := strings.TrimSpace(cfg.Prompt)
	if prompt == "" {
		prompt = DefaultAssemblyAITurnCleanupPrompt
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
func (p *AssemblyAIProvider) assemblyAIStreamingURL() (*url.URL, error) {
	base := firstNonEmptyTrimmed(p.StreamingBaseURL, assemblyAIStreamingBaseURL)
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

func (p *AssemblyAIProvider) assemblyAIStreamingEndpoint(model string, opts speaker.Options, format speaker.AudioFormat) (string, error) {
	u, err := p.assemblyAIStreamingURL()
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	q.Set("format_turns", "true")
	q.Set("speaker_labels", "true")
	q.Set("speech_model", model)
	if maxSpeakers := speakerMax(opts); maxSpeakers > 0 {
		q.Set("max_speakers", strconv.Itoa(maxSpeakers))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *assemblyAISpeakerStream) SendAudio(ctx context.Context, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, chunk)
}

func (s *assemblyAISpeakerStream) EndAudio(ctx context.Context) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Terminate"}`))
}

func (s *assemblyAISpeakerStream) Receive(ctx context.Context) (*speaker.SpeakerFrame, error) {
	for {
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			if errorsIsWebsocketClose(err) {
				return nil, io.EOF
			}
			return nil, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var event assemblyAIStreamingTurn
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("assemblyai speaker stream parse: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(event.Type), "Turn") {
			continue
		}
		frame := event.speakerFrame(s.provider, s.model, s.sequence.Add(1), time.Since(s.openedAt).Milliseconds(), s.opts)
		if frame.Text == "" && len(frame.Words) == 0 {
			continue
		}
		return &frame, nil
	}
}

func (s *assemblyAISpeakerStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "speaker stream close")
}

// assemblyAIDictationStream adapts a Universal-3.5 Pro realtime session to
// speechkit.DictationStream.
type assemblyAIDictationStream struct {
	conn         *websocket.Conn
	provider     string
	model        string
	language     string
	sessionID    uint64
	interim      bool
	diarize      bool
	llm          bool
	pendingFinal *speechkit.DictationStreamEvent
	openedAt     time.Time
	sequence     atomic.Int64
	closeOnce    atomic.Bool
}

func (s *assemblyAIDictationStream) SendPCM(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, pcm)
}

// Finalize asks the provider to flush and end the session. Universal-3.5 Pro
// realtime replies with the trailing (formatted) Turn, a Termination event,
// and a socket close — Receive surfaces the final transcript first and then
// io.EOF, which is exactly the per-segment drain the kernel expects.
func (s *assemblyAIDictationStream) Finalize(ctx context.Context) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Terminate"}`))
}

func (s *assemblyAIDictationStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	for {
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			if errorsIsWebsocketClose(err) {
				return speechkit.DictationStreamEvent{}, io.EOF
			}
			return speechkit.DictationStreamEvent{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var event assemblyAIStreamingTurn
		if err := json.Unmarshal(payload, &event); err != nil {
			return speechkit.DictationStreamEvent{}, fmt.Errorf("assemblyai dictation stream parse: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(event.Type)) {
		case "turn":
			frame := event.dictationEvent(s.provider, s.model, s.language, s.sessionID, s.sequence.Add(1), s.diarize)
			if frame.Text == "" && len(frame.Words) == 0 {
				continue
			}
			if !frame.IsFinal && !s.interim {
				continue
			}
			if frame.IsFinal && s.llm {
				held := frame
				s.pendingFinal = &held
				continue
			}
			return frame, nil
		case "llmgatewayresponse":
			if s.pendingFinal == nil {
				continue
			}
			out := *s.pendingFinal
			s.pendingFinal = nil
			if cleaned := assemblyAILLMGatewayContent(event.Data); cleaned != "" {
				out.Text = cleaned
			}
			return out, nil
		case "termination":
			if s.pendingFinal != nil {
				out := *s.pendingFinal
				s.pendingFinal = nil
				return out, nil
			}
			return speechkit.DictationStreamEvent{}, io.EOF
		default:
			// Begin, SpeechStarted, and future event types are not
			// transcript-bearing.
			continue
		}
	}
}

func (s *assemblyAIDictationStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "dictation stream close")
}

type assemblyAIStreamingTurn struct {
	Type                string                    `json:"type"`
	Transcript          string                    `json:"transcript"`
	EndOfTurn           bool                      `json:"end_of_turn"`
	TurnIsFormatted     bool                      `json:"turn_is_formatted"`
	TurnOrder           int64                     `json:"turn_order"`
	EndOfTurnConfidence float64                   `json:"end_of_turn_confidence"`
	Speaker             string                    `json:"speaker"`
	SpeakerLabel        string                    `json:"speaker_label"`
	Words               []assemblyAIStreamingWord `json:"words"`
	Data                json.RawMessage           `json:"data"`
}

func assemblyAILLMGatewayContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if len(payload.Choices) == 0 {
		return ""
	}
	return strings.TrimSpace(payload.Choices[0].Message.Content)
}

// dictationEvent maps a Turn event onto the provider-neutral dictation event.
// With format_turns=true the formatted turn (end_of_turn && turn_is_formatted)
// is the single final per turn; the unformatted end-of-turn event stays a
// draft so hosts never commit the same turn twice.
//
// diarize mirrors the speaker_labels query param: the provider only sends
// labels when asked, and a caller who did not ask must not receive them.
func (t assemblyAIStreamingTurn) dictationEvent(provider, model, language string, sessionID uint64, sequence int64, diarize bool) speechkit.DictationStreamEvent {
	event := speechkit.DictationStreamEvent{
		Sequence:       sequence,
		SessionID:      sessionID,
		SegmentID:      uint64(t.TurnOrder) + 1, // #nosec G115 -- provider turn_order is a small non-negative counter.
		ProviderItemID: fmt.Sprintf("assemblyai:%d", t.TurnOrder),
		Text:           strings.TrimSpace(t.Transcript),
		IsFinal:        t.EndOfTurn && t.TurnIsFormatted,
		Language:       language,
		Provider:       provider,
		Model:          model,
	}
	if len(t.Words) > 0 {
		event.Words = make([]speechkit.WordConfidence, 0, len(t.Words))
		total := 0.0
		for _, word := range t.Words {
			event.Words = append(event.Words, speechkit.WordConfidence{
				Text:       word.Text,
				Confidence: word.Confidence,
				StartMs:    word.Start,
				EndMs:      word.End,
			})
			total += word.Confidence
		}
		event.Confidence = total / float64(len(t.Words))
	}
	if diarize {
		event.Speakers = t.dictationSpeakers(provider, model, language)
	}
	return event
}

// dictationSpeakers builds the provider-neutral diarization result from a v3
// Turn's speaker_labels output. Dictation carries no KnownSpeakers, so this is
// label-level only — mapping labels onto people (PersonID/DisplayName/Role)
// stays in the speaker-stream path via assemblyAIStreamingIdentity.
func (t assemblyAIStreamingTurn) dictationSpeakers(provider, model, language string) *speaker.DiarizationResult {
	turnSpeaker := firstNonEmptyTrimmed(t.SpeakerLabel, t.Speaker)
	words := make([]speaker.SpeakerWord, 0, len(t.Words))
	for _, word := range t.Words {
		words = append(words, speaker.SpeakerWord{
			Text:              word.Text,
			StartMs:           word.Start,
			EndMs:             word.End,
			Confidence:        word.Confidence,
			SpeakerLabel:      speaker.NormalizeSpeakerLabel(firstNonEmptyTrimmed(word.SpeakerLabel, word.Speaker, turnSpeaker)),
			SpeakerConfidence: word.SpeakerConfidence,
		})
	}
	segments := speaker.BuildSegmentsFromWords(words)
	if len(segments) == 0 && speaker.NormalizeSpeakerLabel(turnSpeaker) == "" {
		// Asked for labels, got none on this frame — report nothing rather
		// than an empty result that reads as "one anonymous speaker".
		return nil
	}
	return &speaker.DiarizationResult{
		Provider: provider,
		Model:    model,
		Level:    speaker.IdentificationDiarization,
		Text:     strings.TrimSpace(t.Transcript),
		Language: language,
		Speakers: speaker.SpeakersFromSegments(segments),
		Segments: segments,
		Words:    words,
	}
}

type assemblyAIStreamingWord struct {
	Text              string  `json:"text"`
	Start             int64   `json:"start"`
	End               int64   `json:"end"`
	Confidence        float64 `json:"confidence"`
	Speaker           string  `json:"speaker"`
	SpeakerLabel      string  `json:"speaker_label"`
	SpeakerConfidence float64 `json:"speaker_confidence"`
}

func (t assemblyAIStreamingTurn) speakerFrame(provider, model string, sequence, latencyMs int64, opts speaker.Options) speaker.SpeakerFrame {
	turnSpeaker := firstNonEmptyTrimmed(t.SpeakerLabel, t.Speaker)
	words := make([]speaker.SpeakerWord, 0, len(t.Words))
	for _, word := range t.Words {
		raw := firstNonEmptyTrimmed(word.SpeakerLabel, word.Speaker, turnSpeaker)
		label, personID, displayName, role := assemblyAIStreamingIdentity(raw, opts)
		words = append(words, speaker.SpeakerWord{
			Text:                  word.Text,
			StartMs:               word.Start,
			EndMs:                 word.End,
			Confidence:            word.Confidence,
			SpeakerLabel:          label,
			SpeakerConfidence:     word.SpeakerConfidence,
			PersonID:              personID,
			DisplayName:           displayName,
			Role:                  role,
			AttributionConfidence: assemblyAIAttributionConfidence(displayName, role, word.SpeakerConfidence),
		})
	}
	frame := speaker.FrameFromWords(provider, model, sequence, t.Transcript, t.EndOfTurn || t.TurnIsFormatted, words, latencyMs)
	if frame.Segment == nil {
		label, personID, displayName, role := assemblyAIStreamingIdentity(turnSpeaker, opts)
		if label != "" || personID != "" || displayName != "" || role != "" || strings.TrimSpace(t.Transcript) != "" {
			frame.Segment = &speaker.SpeakerSegment{
				Text:         strings.TrimSpace(t.Transcript),
				SpeakerLabel: label,
				PersonID:     personID,
				DisplayName:  displayName,
				Role:         role,
			}
			frame.Speakers = speaker.SpeakersFromSegments([]speaker.SpeakerSegment{*frame.Segment})
		}
	}
	return frame
}

func assemblyAIStreamingIdentity(raw string, opts speaker.Options) (label, personID, displayName, role string) {
	raw = strings.TrimSpace(raw)
	label = speaker.NormalizeSpeakerLabel(raw)
	if label == "" {
		return "", "", "", ""
	}
	if !opts.AllowProviderMapping || len(opts.KnownSpeakers) == 0 {
		return label, "", "", ""
	}
	idx := assemblyAISpeakerOrdinal(raw)
	if idx < 0 || idx >= len(opts.KnownSpeakers) {
		return label, "", "", ""
	}
	known := opts.KnownSpeakers[idx]
	return label, known.ID, known.DisplayName, known.Role
}

func assemblyAISpeakerOrdinal(raw string) int {
	raw = strings.TrimSpace(strings.TrimPrefix(speaker.NormalizeSpeakerLabel(raw), "speaker_"))
	if len(raw) != 1 {
		return -1
	}
	ch := raw[0]
	switch {
	case ch >= 'A' && ch <= 'Z':
		return int(ch - 'A')
	case ch >= 'a' && ch <= 'z':
		return int(ch - 'a')
	default:
		return -1
	}
}
