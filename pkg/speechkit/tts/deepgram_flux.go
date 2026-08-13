package tts

// Deepgram Flux TTS (/v2/speak, WebSocket).
//
// Flux TTS is conversation-native rather than a one-shot text-to-audio call:
// LLM tokens are streamed in as they arrive and the server places the flush
// boundaries, conversational state carries across turns so the voice stays
// consistent, and a barge-in reports the text that was actually spoken instead
// of leaving the caller to guess how much of the answer the user heard. The
// batch REST provider in deepgram.go stays as-is for Aura and for one-shot
// synthesis; this client exists for live dialogue.
//
// Flux TTS voices are English-only. Aura-2 remains the multilingual speak path.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const (
	deepgramFluxTTSPath = "v2/speak"
	// DeepgramFluxTTSDefaultVoice is Deepgram's default Flux TTS voice.
	DeepgramFluxTTSDefaultVoice = "flux-kit-en"
	deepgramFluxTTSReadLimit    = 4 << 20
)

// Flux TTS server event types. A turn opens with SpeechStarted, Flushed
// acknowledges the client's Flush, and SpeechMetadata closes the turn with its
// duration and billing counts. Interrupt arrives instead when the listener
// barges in.
const (
	FluxSpeechStarted   = "SpeechStarted"
	FluxSpeechFlushed   = "Flushed"
	FluxSpeechMetadata  = "SpeechMetadata"
	FluxSpeechInterrupt = "Interrupt"
)

// deepgramFluxTTSSpeeds are the discrete speeds Flux TTS accepts.
var deepgramFluxTTSSpeeds = []float64{0.85, 0.9, 0.95, 1.0, 1.05, 1.1, 1.15}

// FluxSpeechOptions configures a Flux TTS stream.
type FluxSpeechOptions struct {
	// Voice is a Flux TTS model id such as "flux-kit-en". Empty uses
	// DeepgramFluxTTSDefaultVoice.
	Voice string
	// SampleRateHz selects the linear16 output rate. Zero uses Deepgram's
	// default of 24 kHz, which matches SpeechKit's playback contract.
	SampleRateHz int
	// Speed snaps to the nearest value Flux accepts (0.85–1.15 in 0.05 steps).
	// Zero keeps the provider default.
	Speed float64
}

// FluxSpeechEvent is one decoded event from a Flux TTS stream. Exactly one of
// Audio or Type is populated: binary frames carry Audio, control frames carry
// Type and the fields belonging to it.
type FluxSpeechEvent struct {
	// Audio holds raw linear16 PCM at the negotiated sample rate.
	Audio []byte
	// Type is the control event name (SpeechStarted, SpeechMetadata, Interrupt).
	Type string
	// SpeechID identifies the turn this event belongs to.
	SpeechID string
	// AudioDurationMs is the synthesized duration reported with SpeechMetadata.
	AudioDurationMs int64
	// InputCharacterCount and BillableCharacterCount close out the turn's
	// billing, reported with SpeechMetadata.
	InputCharacterCount    int
	BillableCharacterCount int
	// TextSpoken and TextRemaining are reported on Interrupt: what the listener
	// actually heard before the barge-in, and what was cut. Recording
	// TextSpoken as the assistant turn keeps the conversation history honest.
	TextSpoken    string
	TextRemaining string
	// Raw is the undecoded control frame. Flux TTS is young and its event
	// payloads are not fully documented, so keeping the original lets callers
	// read fields this client does not model yet.
	Raw json.RawMessage
}

// IsAudio reports whether the event carries synthesized audio.
func (e FluxSpeechEvent) IsAudio() bool { return len(e.Audio) > 0 }

// FluxSpeechStream is a live Flux TTS connection.
type FluxSpeechStream struct {
	conn         *websocket.Conn
	voice        string
	sampleRateHz int
	closeOnce    atomic.Bool
}

// DeepgramFluxTTS opens Flux TTS streams. It reuses the Deepgram API key shared
// with the STT and Voice Agent adapters.
type DeepgramFluxTTS struct {
	apiKey     string
	BaseURL    string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// NewDeepgramFluxTTS creates a Flux TTS client.
func NewDeepgramFluxTTS(apiKey string) *DeepgramFluxTTS {
	d := &DeepgramFluxTTS{apiKey: strings.TrimSpace(apiKey), BaseURL: deepgramTTSBaseURL}
	d.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 45 * time.Second, DialValidation: &d.Validation})
	return d
}

// Open dials the Flux TTS WebSocket.
func (d *DeepgramFluxTTS) Open(ctx context.Context, opts FluxSpeechOptions) (*FluxSpeechStream, error) {
	if d.apiKey == "" {
		return nil, fmt.Errorf("deepgram flux tts: no API key configured")
	}
	voice := strings.TrimSpace(opts.Voice)
	if voice == "" {
		voice = DeepgramFluxTTSDefaultVoice
	}
	sampleRate := opts.SampleRateHz
	if sampleRate <= 0 {
		sampleRate = deepgramTTSSampleRate
	}
	endpoint, err := d.endpoint(voice, sampleRate, opts.Speed)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	header.Set("Authorization", "Token "+d.apiKey)
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: d.client,
		HTTPHeader: header,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("deepgram flux tts dial: %w", err)
	}
	conn.SetReadLimit(deepgramFluxTTSReadLimit)
	return &FluxSpeechStream{conn: conn, voice: voice, sampleRateHz: sampleRate}, nil
}

func (d *DeepgramFluxTTS) endpoint(voice string, sampleRateHz int, speed float64) (string, error) {
	base, err := netsec.BuildEndpoint(firstNonEmptyTTS(d.BaseURL, deepgramTTSBaseURL), deepgramFluxTTSPath, d.Validation)
	if err != nil {
		return "", fmt.Errorf("deepgram flux tts endpoint: %w", err)
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("deepgram flux tts endpoint parse: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("deepgram flux tts endpoint: unsupported scheme %q", u.Scheme)
	}
	q := u.Query()
	q.Set("model", voice)
	// The streaming leg produces raw audio with no container; compressed
	// encodings are batch-REST only.
	q.Set("encoding", "linear16")
	q.Set("sample_rate", strconv.Itoa(sampleRateHz))
	if snapped := SnapDeepgramFluxSpeed(speed); snapped > 0 {
		q.Set("speed", strconv.FormatFloat(snapped, 'f', -1, 64))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Voice reports the Flux voice this stream negotiated.
func (s *FluxSpeechStream) Voice() string { return s.voice }

// SampleRateHz reports the linear16 output rate.
func (s *FluxSpeechStream) SampleRateHz() int { return s.sampleRateHz }

// Speak streams text into the current turn. Callers pass LLM tokens as they
// arrive; the server decides where to break for synthesis.
func (s *FluxSpeechStream) Speak(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}
	return s.send(ctx, map[string]any{"type": "Speak", "text": text})
}

// Flush ends the turn so the server synthesizes whatever text is buffered.
func (s *FluxSpeechStream) Flush(ctx context.Context) error {
	return s.send(ctx, map[string]any{"type": "Flush"})
}

// Configure adjusts the delivery speed mid-stream without reconnecting.
func (s *FluxSpeechStream) Configure(ctx context.Context, speed float64) error {
	snapped := SnapDeepgramFluxSpeed(speed)
	if snapped <= 0 {
		return fmt.Errorf("deepgram flux tts: speed %v is not a usable value", speed)
	}
	return s.send(ctx, map[string]any{"type": "Configure", "speed": snapped})
}

func (s *FluxSpeechStream) send(ctx context.Context, msg map[string]any) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("deepgram flux tts marshal: %w", err)
	}
	return s.conn.Write(ctx, websocket.MessageText, body)
}

// Receive returns the next audio frame or control event, and io.EOF when
// Deepgram closes the stream.
func (s *FluxSpeechStream) Receive(ctx context.Context) (FluxSpeechEvent, error) {
	for {
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				return FluxSpeechEvent{}, io.EOF
			}
			return FluxSpeechEvent{}, err
		}
		if typ == websocket.MessageBinary {
			if len(payload) == 0 {
				continue
			}
			return FluxSpeechEvent{Audio: payload}, nil
		}
		var event deepgramFluxTTSEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return FluxSpeechEvent{}, fmt.Errorf("deepgram flux tts parse: %w", err)
		}
		if err := event.err(); err != nil {
			return FluxSpeechEvent{}, err
		}
		if event.Type == "" {
			continue
		}
		decoded := event.event()
		decoded.Raw = json.RawMessage(payload)
		return decoded, nil
	}
}

// Close shuts the stream down. It is safe to call more than once.
func (s *FluxSpeechStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "flux tts close")
}

type deepgramFluxTTSEvent struct {
	Type                   string `json:"type"`
	SpeechID               string `json:"speech_id"`
	AudioDurationMs        int64  `json:"audio_duration_ms"`
	InputCharacterCount    int    `json:"input_character_count"`
	BillableCharacterCount int    `json:"billable_character_count"`
	TextSpoken             string `json:"text_spoken"`
	TextRemaining          string `json:"text_remaining"`
	Description            string `json:"description"`
	Message                string `json:"message"`
}

func (e deepgramFluxTTSEvent) err() error {
	if !strings.EqualFold(e.Type, "Error") && !strings.EqualFold(e.Type, "Fatal") {
		return nil
	}
	detail := firstNonEmptyTTS(e.Description, e.Message, "unknown error")
	return fmt.Errorf("deepgram flux tts: %s: %s", e.Type, detail)
}

func (e deepgramFluxTTSEvent) event() FluxSpeechEvent {
	return FluxSpeechEvent{
		Type:                   e.Type,
		SpeechID:               e.SpeechID,
		AudioDurationMs:        e.AudioDurationMs,
		InputCharacterCount:    e.InputCharacterCount,
		BillableCharacterCount: e.BillableCharacterCount,
		TextSpoken:             e.TextSpoken,
		TextRemaining:          e.TextRemaining,
	}
}

// SnapDeepgramFluxSpeed rounds a speed onto the discrete steps Flux TTS
// accepts, clamping to the nearest bound. It returns 0 for "unset" so callers
// can omit the parameter.
func SnapDeepgramFluxSpeed(speed float64) float64 {
	if speed <= 0 {
		return 0
	}
	best := deepgramFluxTTSSpeeds[0]
	for _, step := range deepgramFluxTTSSpeeds {
		if math.Abs(step-speed) < math.Abs(best-speed) {
			best = step
		}
	}
	return best
}

// IsDeepgramFluxVoice reports whether a voice id names a Flux TTS voice rather
// than an Aura one.
func IsDeepgramFluxVoice(voice string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(voice)), "flux-")
}

func firstNonEmptyTTS(values ...string) string {
	for _, value := range values {
		if v := strings.TrimSpace(value); v != "" {
			return v
		}
	}
	return ""
}
