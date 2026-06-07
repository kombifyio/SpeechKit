package stt

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

	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

type deepgramSpeakerStream struct {
	conn      *websocket.Conn
	provider  string
	model     string
	openedAt  time.Time
	sequence  atomic.Int64
	closeOnce atomic.Bool
}

func (p *DeepgramProvider) StartSpeakerStream(ctx context.Context, opts speaker.Options, format speaker.AudioFormat) (speaker.SpeakerStream, error) {
	opts = opts.Normalized()
	if !opts.WantsDiarization() {
		return nil, fmt.Errorf("deepgram speaker streaming requires diarization options")
	}
	format = format.Normalized()
	model := firstNonEmptyTrimmed(opts.Model, p.Model, "nova-3")
	endpoint, err := p.deepgramStreamingEndpoint(model, opts.Language, format)
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
		return nil, fmt.Errorf("deepgram speaker stream dial: %w", err)
	}
	return &deepgramSpeakerStream{
		conn:     conn,
		provider: p.Name(),
		model:    model,
		openedAt: time.Now(),
	}, nil
}

func (p *DeepgramProvider) deepgramStreamingEndpoint(model, language string, format speaker.AudioFormat) (string, error) {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, deepgramBaseURL), "v1/listen", p.Validation)
	if err != nil {
		return "", fmt.Errorf("deepgram streaming endpoint: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("deepgram streaming endpoint parse: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("deepgram streaming endpoint: unsupported scheme %q", u.Scheme)
	}
	q := u.Query()
	q.Set("model", model)
	q.Set("punctuate", "true")
	q.Set("diarize", "true")
	q.Set("utterances", "true")
	q.Set("encoding", deepgramEncoding(format.Encoding))
	q.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	q.Set("channels", strconv.Itoa(format.Channels))
	language = strings.TrimSpace(language)
	if language != "" && language != "auto" {
		q.Set("language", language)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func deepgramEncoding(encoding speaker.AudioEncoding) string { //nolint:unparam // mapping retained; future non-PCM16 deepgram encodings will return distinct values
	switch encoding {
	case speaker.AudioEncodingPCM16:
		return "linear16"
	default:
		return string(speaker.AudioEncodingLinear16)
	}
}

func (s *deepgramSpeakerStream) SendAudio(ctx context.Context, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, chunk)
}

func (s *deepgramSpeakerStream) EndAudio(ctx context.Context) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"CloseStream"}`))
}

func (s *deepgramSpeakerStream) Receive(ctx context.Context) (*speaker.SpeakerFrame, error) {
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
		var event deepgramStreamingResponse
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, fmt.Errorf("deepgram speaker stream parse: %w", err)
		}
		frame := event.speakerFrame(s.provider, s.model, s.sequence.Add(1), time.Since(s.openedAt).Milliseconds())
		if frame.Text == "" && len(frame.Words) == 0 {
			continue
		}
		return &frame, nil
	}
}

func (s *deepgramSpeakerStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "speaker stream close")
}

type deepgramStreamingResponse struct {
	Type        string          `json:"type"`
	IsFinal     bool            `json:"is_final"`
	SpeechFinal bool            `json:"speech_final"`
	Channel     deepgramChannel `json:"channel"`
}

func (r deepgramStreamingResponse) speakerFrame(provider, model string, sequence, latencyMs int64) speaker.SpeakerFrame {
	if len(r.Channel.Alternatives) == 0 {
		return speaker.SpeakerFrame{Provider: provider, Model: model, Sequence: sequence, IsFinal: r.IsFinal || r.SpeechFinal, LatencyMs: latencyMs}
	}
	alt := r.Channel.Alternatives[0]
	words := make([]speaker.SpeakerWord, 0, len(alt.Words))
	for _, word := range alt.Words {
		label := ""
		if word.Speaker != nil {
			label = speaker.NormalizeSpeakerLabel(*word.Speaker)
		}
		words = append(words, speaker.SpeakerWord{
			Text:              firstNonEmptyTrimmed(word.PunctuatedWord, word.Word),
			StartMs:           secondsToMillis(word.Start),
			EndMs:             secondsToMillis(word.End),
			Confidence:        word.Confidence,
			SpeakerLabel:      label,
			SpeakerConfidence: word.SpeakerConfidence,
		})
	}
	return speaker.FrameFromWords(provider, model, sequence, alt.Transcript, r.IsFinal || r.SpeechFinal, words, latencyMs)
}

func errorsIsWebsocketClose(err error) bool {
	return websocket.CloseStatus(err) != -1
}
