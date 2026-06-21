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

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
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
	model := firstNonEmptyTrimmed(opts.Model, p.StreamingModel, assemblyAIStreamingModel)
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

func (p *AssemblyAIProvider) assemblyAIStreamingEndpoint(model string, opts speaker.Options, format speaker.AudioFormat) (string, error) {
	base := firstNonEmptyTrimmed(p.StreamingBaseURL, assemblyAIStreamingBaseURL)
	validation := p.Validation
	baseForValidation := base
	if strings.HasPrefix(strings.ToLower(baseForValidation), "wss://") {
		baseForValidation = "https://" + strings.TrimPrefix(baseForValidation, "wss://")
	} else if strings.HasPrefix(strings.ToLower(baseForValidation), "ws://") {
		baseForValidation = "http://" + strings.TrimPrefix(baseForValidation, "ws://")
	}
	endpoint, err := netsec.BuildEndpoint(baseForValidation, "v3/ws", validation)
	if err != nil {
		return "", fmt.Errorf("assemblyai streaming endpoint: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("assemblyai streaming endpoint parse: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("assemblyai streaming endpoint: unsupported scheme %q", u.Scheme)
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

type assemblyAIStreamingTurn struct {
	Type            string                    `json:"type"`
	Transcript      string                    `json:"transcript"`
	EndOfTurn       bool                      `json:"end_of_turn"`
	TurnIsFormatted bool                      `json:"turn_is_formatted"`
	Speaker         string                    `json:"speaker"`
	SpeakerLabel    string                    `json:"speaker_label"`
	Words           []assemblyAIStreamingWord `json:"words"`
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
