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

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
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

type deepgramDictationStream struct {
	conn      *websocket.Conn
	provider  string
	model     string
	language  string
	sessionID uint64
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

func (p *DeepgramProvider) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	format = format.Normalized()
	model := firstNonEmptyTrimmed(opts.Model, p.Model, "nova-3")
	language := firstNonEmptyTrimmed(p.LanguageOverride, opts.Language, deepgramCodeSwitchingLanguage())
	endpoint, err := p.deepgramDictationStreamingEndpoint(model, language, opts, format)
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
		return nil, fmt.Errorf("deepgram dictation stream dial: %w", err)
	}
	return &deepgramDictationStream{
		conn:      conn,
		provider:  p.Name(),
		model:     model,
		language:  normalizedDeepgramLanguage(language),
		sessionID: opts.SessionID,
		openedAt:  time.Now(),
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
	if p.SmartFormat {
		q.Set("smart_format", "true")
	}
	if p.Dictation {
		q.Set("dictation", "true")
	}
	if p.FillerWords {
		q.Set("filler_words", "true")
	}
	if p.Numerals {
		q.Set("numerals", "true")
	}
	q.Set("diarize", "true")
	q.Set("utterances", "true")
	q.Set("encoding", deepgramEncoding(format.Encoding))
	q.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	q.Set("channels", strconv.Itoa(format.Channels))
	if p.EndpointingMs > 0 {
		q.Set("endpointing", strconv.Itoa(p.EndpointingMs))
	}
	if language := normalizedDeepgramLanguage(firstNonEmptyTrimmed(p.LanguageOverride, language)); language != "" {
		q.Set("language", language)
	}
	p.applyVocabularyBias(q, model, nil, p.UseVocabularyKeyterms)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (p *DeepgramProvider) deepgramDictationStreamingEndpoint(model, language string, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (string, error) {
	endpoint, err := netsec.BuildEndpoint(firstNonEmptyTrimmed(p.BaseURL, deepgramBaseURL), "v1/listen", p.Validation)
	if err != nil {
		return "", fmt.Errorf("deepgram dictation streaming endpoint: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("deepgram dictation streaming endpoint parse: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("deepgram dictation streaming endpoint: unsupported scheme %q", u.Scheme)
	}
	q := u.Query()
	q.Set("model", model)
	q.Set("punctuate", "true")
	if opts.InterimResults {
		q.Set("interim_results", "true")
	}
	if p.SmartFormat {
		q.Set("smart_format", "true")
	}
	if p.Dictation {
		q.Set("dictation", "true")
	}
	if p.FillerWords {
		q.Set("filler_words", "true")
	}
	if p.Numerals {
		q.Set("numerals", "true")
	}
	if opts.Diarization {
		q.Set("diarize", "true")
		q.Set("utterances", "true")
	}
	q.Set("encoding", deepgramEncoding(format.Encoding))
	q.Set("sample_rate", strconv.Itoa(format.SampleRateHz))
	q.Set("channels", strconv.Itoa(format.Channels))
	endpointingMs := opts.EndpointingMs
	if endpointingMs <= 0 {
		endpointingMs = p.EndpointingMs
	}
	if endpointingMs > 0 {
		q.Set("endpointing", strconv.Itoa(endpointingMs))
	}
	if language := normalizedDeepgramLanguage(language); language != "" {
		q.Set("language", language)
	}
	p.applyVocabularyBias(q, model, opts.Keyterms, p.UseVocabularyKeyterms)
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

func (s *deepgramDictationStream) SendPCM(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, pcm)
}

func (s *deepgramDictationStream) Finalize(ctx context.Context) error {
	return s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Finalize"}`))
}

func (s *deepgramDictationStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
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
		var event deepgramStreamingResponse
		if err := json.Unmarshal(payload, &event); err != nil {
			return speechkit.DictationStreamEvent{}, fmt.Errorf("deepgram dictation stream parse: %w", err)
		}
		frame := event.dictationEvent(s.provider, s.model, s.language, s.sessionID, s.sequence.Add(1), time.Since(s.openedAt).Milliseconds())
		if frame.Text == "" && len(frame.Words) == 0 {
			continue
		}
		return frame, nil
	}
}

func (s *deepgramDictationStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "dictation stream close")
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

func (r deepgramStreamingResponse) dictationEvent(provider, model, fallbackLanguage string, sessionID uint64, sequence, _ int64) speechkit.DictationStreamEvent {
	isFinal := r.IsFinal || r.SpeechFinal
	event := speechkit.DictationStreamEvent{
		Sequence:       sequence,
		SessionID:      sessionID,
		SegmentID:      uint64(sequence),
		ProviderItemID: fmt.Sprintf("deepgram:%d", sequence),
		IsFinal:        isFinal,
		Language:       firstNonEmptyTrimmed(r.Channel.DetectedLanguage, fallbackLanguage, deepgramCodeSwitchingLanguage()),
		Provider:       provider,
		Model:          model,
	}
	if len(r.Channel.Alternatives) == 0 {
		return event
	}
	alt := r.Channel.Alternatives[0]
	event.Text = strings.TrimSpace(alt.Transcript)
	event.Confidence = alt.Confidence
	event.Words = make([]speechkit.WordConfidence, 0, len(alt.Words))
	for _, word := range alt.Words {
		event.Words = append(event.Words, speechkit.WordConfidence{
			Text:       firstNonEmptyTrimmed(word.PunctuatedWord, word.Word),
			Confidence: word.Confidence,
			StartMs:    secondsToMillis(word.Start),
			EndMs:      secondsToMillis(word.End),
		})
	}
	return event
}

func errorsIsWebsocketClose(err error) bool {
	return websocket.CloseStatus(err) != -1
}
