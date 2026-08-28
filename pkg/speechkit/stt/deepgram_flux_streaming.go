package stt

// Deepgram Flux conversational STT (/v2/listen).
//
// Flux is a different protocol from the Listen v1 streaming API in
// deepgram_streaming.go, not a model swap: it speaks its own TurnInfo event
// shape, performs end-of-turn detection inside the model instead of leaving it
// to client-side VAD, and reports the languages it actually heard per turn. The
// two decoders stay separate because their event models do not overlap.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Flux turn lifecycle events. A turn opens with StartOfTurn, grows through
// Update events, and closes with EndOfTurn. EagerEndOfTurn is a speculative
// signal — the model believes the speaker is done and a consumer may start
// generating a response — which TurnResumed retracts when the speaker
// continues.
const (
	FluxEventStartOfTurn    = "StartOfTurn"
	FluxEventUpdate         = "Update"
	FluxEventEagerEndOfTurn = "EagerEndOfTurn"
	FluxEventTurnResumed    = "TurnResumed"
	FluxEventEndOfTurn      = "EndOfTurn"
)

// Flux tuning ranges, per the Deepgram Flux API reference.
const (
	FluxEOTThresholdMin      = 0.5
	FluxEOTThresholdMax      = 0.9
	FluxEagerEOTThresholdMin = 0.3
	FluxEagerEOTThresholdMax = 0.9
	FluxEOTTimeoutMinMs      = 500
	FluxEOTTimeoutMaxMs      = 60000
)

// FluxAudioChunk is the chunk duration Deepgram recommends for Flux input.
const FluxAudioChunk = 80 * time.Millisecond

// FluxStreamOptions configures a Flux turn stream. The zero value is valid and
// selects the multilingual model with Deepgram's default turn detection.
//
// Deprecated: moved to pkg/speechkit/stt/deepgram.FluxStreamOptions. This name is removed in
// v0.65.0; import the provider package instead.
type FluxStreamOptions struct {
	// Model selects flux-general-en or flux-general-multi. Empty uses the
	// multilingual model, which is the point of Flux for SpeechKit: it detects
	// and switches languages within a single conversation.
	Model string
	// LanguageHints biases recognition toward the given BCP-47 languages. Only
	// flux-general-multi accepts hints; they narrow the model, they do not pin
	// it, and every turn still reports what it actually heard.
	LanguageHints []string
	// Keyterms boost domain vocabulary (product names and other terms the model
	// has not seen).
	Keyterms []string
	// EOTThreshold, EagerEOTThreshold, and EOTTimeoutMs tune turn detection.
	// Zero keeps Deepgram's defaults; out-of-range values are clamped.
	EOTThreshold      float64
	EagerEOTThreshold float64
	EOTTimeoutMs      int
}

// FluxWord is a single recognized word. Flux reports no speaker label and no
// separately punctuated form — the turn transcript carries the punctuation.
//
// Deprecated: moved to pkg/speechkit/stt/deepgram.FluxWord. This name is removed in
// v0.65.0; import the provider package instead.
type FluxWord struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	StartMs    int64   `json:"startMs"`
	EndMs      int64   `json:"endMs"`
}

// FluxTurn is one decoded TurnInfo event.
//
// Deprecated: moved to pkg/speechkit/stt/deepgram.FluxTurn. This name is removed in
// v0.65.0; import the provider package instead.
type FluxTurn struct {
	// Event is one of the Flux lifecycle events above.
	Event string `json:"event"`
	// TurnIndex counts turns within the connection.
	TurnIndex int `json:"turnIndex"`
	// Transcript is the full turn transcript so far, not a delta.
	Transcript string     `json:"transcript"`
	Words      []FluxWord `json:"words,omitempty"`
	// Languages are the languages actually detected in this turn; empty when
	// the turn holds no speech yet.
	Languages []string `json:"languages,omitempty"`
	// EndOfTurnConfidence is the model's confidence that the speaker is done.
	EndOfTurnConfidence float64 `json:"endOfTurnConfidence"`
	// AudioWindowStartMs/EndMs bound the audio this turn covers.
	AudioWindowStartMs int64 `json:"audioWindowStartMs"`
	AudioWindowEndMs   int64 `json:"audioWindowEndMs"`
	// SequenceID is Deepgram's per-connection event counter.
	SequenceID int64 `json:"sequenceId"`
	// RequestID identifies the connection in Deepgram's logs.
	RequestID string `json:"requestId"`
	// LatencyMs is the time from stream open to this event.
	LatencyMs int64 `json:"latencyMs"`
}

// IsFinal reports whether the turn is closed and the transcript will not grow.
func (t FluxTurn) IsFinal() bool { return t.Event == FluxEventEndOfTurn }

// IsSpeculative reports whether the event is the eager end-of-turn signal,
// which TurnResumed can retract. A consumer may start work on it, but must be
// able to cancel that work.
func (t FluxTurn) IsSpeculative() bool { return t.Event == FluxEventEagerEndOfTurn }

// FluxTurnStream is a live Flux connection. Callers push PCM with SendPCM and
// read turn events with Receive until io.EOF.
//
// Deprecated: moved to pkg/speechkit/stt/deepgram.FluxTurnStream. This name is removed in
// v0.65.0; import the provider package instead.
type FluxTurnStream struct {
	conn      *websocket.Conn
	provider  string
	model     string
	openedAt  time.Time
	closeOnce atomic.Bool
}

// StartFluxTurnStream opens a Deepgram Flux /v2/listen stream. The format must
// be raw PCM; Deepgram recommends 16 kHz mono and FluxAudioChunk-sized writes.
func (p *DeepgramProvider) StartFluxTurnStream(ctx context.Context, opts FluxStreamOptions, format speaker.AudioFormat) (*FluxTurnStream, error) {
	format = format.Normalized()
	model := firstNonEmptyTrimmed(opts.Model, DeepgramFluxModelMulti)
	endpoint, err := p.deepgramFluxEndpoint(model, opts, format)
	if err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("Authorization", "Token "+p.APIKey)
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: p.client,
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("deepgram flux stream dial: %w", err)
	}
	return &FluxTurnStream{
		conn:     conn,
		provider: p.Name(),
		model:    model,
		openedAt: time.Now(),
	}, nil
}

// Deepgram Flux model identifiers.
const (
	DeepgramFluxModelEN    = "flux-general-en"
	DeepgramFluxModelMulti = "flux-general-multi"
)

func (p *DeepgramProvider) deepgramFluxEndpoint(model string, opts FluxStreamOptions, format speaker.AudioFormat) (string, error) {
	// Flux lives on /v2/listen; the v1 path rejects these models outright.
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, deepgramBaseURL), "v2/listen", p.Validation)
	if err != nil {
		return "", fmt.Errorf("deepgram flux endpoint: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("deepgram flux endpoint parse: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("deepgram flux endpoint: unsupported scheme %q", u.Scheme)
	}
	q := u.Query()
	q.Set("model", model)
	q.Set("encoding", deepgramEncoding(format.Encoding))
	q.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	// Flux takes no "language" parameter: the English model is pinned by its
	// name and the multilingual model is steered with repeatable hints.
	if model == DeepgramFluxModelMulti {
		for _, hint := range normalizeFluxLanguageHints(opts.LanguageHints) {
			q.Add("language_hint", hint)
		}
	}
	for _, term := range dedupeTrimmed(opts.Keyterms, deepgramMaxKeyterms) {
		q.Add("keyterm", term)
	}
	if v, ok := clampFluxRange(opts.EOTThreshold, FluxEOTThresholdMin, FluxEOTThresholdMax); ok {
		q.Set("eot_threshold", strconv.FormatFloat(v, 'f', -1, 64))
	}
	if v, ok := clampFluxRange(opts.EagerEOTThreshold, FluxEagerEOTThresholdMin, FluxEagerEOTThresholdMax); ok {
		q.Set("eager_eot_threshold", strconv.FormatFloat(v, 'f', -1, 64))
	}
	if v, ok := clampFluxRange(float64(opts.EOTTimeoutMs), FluxEOTTimeoutMinMs, FluxEOTTimeoutMaxMs); ok {
		q.Set("eot_timeout_ms", strconv.Itoa(int(v)))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// SendPCM streams one chunk of raw PCM.
func (s *FluxTurnStream) SendPCM(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, pcm)
}

// CloseStream tells Deepgram no further audio is coming, so it can flush the
// open turn instead of waiting for the end-of-turn timeout.
func (s *FluxTurnStream) CloseStream(ctx context.Context) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"CloseStream"}`))
}

// Model reports the Flux model this stream negotiated.
func (s *FluxTurnStream) Model() string { return s.model }

// Receive returns the next turn event, skipping the connection handshake and
// any frame that carries no turn. It returns io.EOF when Deepgram closes.
func (s *FluxTurnStream) Receive(ctx context.Context) (FluxTurn, error) {
	for {
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			if errorsIsWebsocketClose(err) {
				return FluxTurn{}, io.EOF
			}
			return FluxTurn{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var event deepgramFluxEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return FluxTurn{}, fmt.Errorf("deepgram flux parse: %w", err)
		}
		if err := event.err(); err != nil {
			return FluxTurn{}, err
		}
		if event.Type != "TurnInfo" || event.Event == "" {
			// Connected and any other non-turn frame carry no transcript.
			continue
		}
		return event.turn(time.Since(s.openedAt).Milliseconds()), nil
	}
}

// Close shuts the WebSocket down. It is safe to call more than once.
func (s *FluxTurnStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "flux stream close")
}

// deepgramFluxEvent mirrors the wire shape of a /v2/listen server frame.
type deepgramFluxEvent struct {
	Type                string             `json:"type"`
	RequestID           string             `json:"request_id"`
	SequenceID          int64              `json:"sequence_id"`
	Event               string             `json:"event"`
	TurnIndex           int                `json:"turn_index"`
	AudioWindowStart    float64            `json:"audio_window_start"`
	AudioWindowEnd      float64            `json:"audio_window_end"`
	Transcript          string             `json:"transcript"`
	Words               []deepgramFluxWord `json:"words"`
	Languages           []string           `json:"languages"`
	EndOfTurnConfidence float64            `json:"end_of_turn_confidence"`
	Description         string             `json:"description"`
	Message             string             `json:"message"`
}

type deepgramFluxWord struct {
	Word       string  `json:"word"`
	Confidence float64 `json:"confidence"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
}

// err converts a Deepgram error frame into a Go error.
func (e deepgramFluxEvent) err() error {
	if !strings.EqualFold(e.Type, "Error") && !strings.EqualFold(e.Type, "Fatal") {
		return nil
	}
	detail := firstNonEmptyTrimmed(e.Description, e.Message, "unknown error")
	return fmt.Errorf("deepgram flux: %s: %s", e.Type, detail)
}

func (e deepgramFluxEvent) turn(latencyMs int64) FluxTurn {
	turn := FluxTurn{
		Event:               e.Event,
		TurnIndex:           e.TurnIndex,
		Transcript:          strings.TrimSpace(e.Transcript),
		Languages:           normalizeFluxLanguageHints(e.Languages),
		EndOfTurnConfidence: e.EndOfTurnConfidence,
		AudioWindowStartMs:  secondsToMillis(e.AudioWindowStart),
		AudioWindowEndMs:    secondsToMillis(e.AudioWindowEnd),
		SequenceID:          e.SequenceID,
		RequestID:           e.RequestID,
		LatencyMs:           latencyMs,
	}
	if len(e.Words) == 0 {
		return turn
	}
	turn.Words = make([]FluxWord, 0, len(e.Words))
	for _, word := range e.Words {
		turn.Words = append(turn.Words, FluxWord{
			Text:       strings.TrimSpace(word.Word),
			Confidence: word.Confidence,
			// Flux reports the first word's start as a signed epsilon around
			// zero; clamping keeps downstream timelines non-negative.
			StartMs: maxInt64(secondsToMillis(word.Start), 0),
			EndMs:   maxInt64(secondsToMillis(word.End), 0),
		})
	}
	return turn
}

// normalizeFluxLanguageHints lowercases hints, reduces them to the base
// language subtag Flux expects, and drops blanks and duplicates.
func normalizeFluxLanguageHints(hints []string) []string {
	if len(hints) == 0 {
		return nil
	}
	out := make([]string, 0, len(hints))
	seen := map[string]struct{}{}
	for _, hint := range hints {
		hint = strings.ToLower(strings.TrimSpace(hint))
		if hint == "" || hint == "auto" || hint == "multi" {
			continue
		}
		if idx := strings.IndexAny(hint, "-_"); idx > 0 {
			hint = hint[:idx]
		}
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		out = append(out, hint)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func dedupeTrimmed(values []string, limit int) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func clampFluxRange(value, lower, upper float64) (float64, bool) {
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

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
