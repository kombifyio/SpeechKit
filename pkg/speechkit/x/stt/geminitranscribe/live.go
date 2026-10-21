package geminitranscribe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/genai"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// LiveModel is Gemini Transcribe's streaming model on the Live API. Live
// dictation always runs on it; the batch path keeps Provider.Model.
const LiveModel = "gemini-3.5-transcribe-live"

// Wire shape source: the Gemini cookbook quickstart Get_started_transcribe
// (section "Real-time live streaming transcription") and the google-genai
// SDKs' LiveConnectConfig / AudioTranscriptionConfig types:
//
//   - client.Live.Connect(model, config) with response_modalities ["TEXT"]
//     and input_audio_transcription {mode, language_codes (BCP-47),
//     custom_vocabulary};
//   - audio goes as realtime input blobs "audio/pcm;rate=<hz>" (16 kHz mono
//     16-bit PCM recommended) and ends with audio_stream_end;
//   - transcript text arrives as server_content.input_transcription
//     fragments; finished or turn_complete closes a turn.
//
// server_content.interim_input_transcription is not used: its update
// semantics are not documented yet, so drafts are built from the committed
// fragments only.

// liveDrainTimeout bounds how long Receive waits after Finalize for the
// transcript of the trailing audio. The Live API sends no explicit
// acknowledgement of audio_stream_end when nothing was left to transcribe.
const liveDrainTimeout = 5 * time.Second

// liveSession is the part of *genai.Session the stream uses.
type liveSession interface {
	SendRealtimeInput(input genai.LiveRealtimeInput) error
	Receive() (*genai.LiveServerMessage, error)
	Close() error
}

type liveConnectFunc func(ctx context.Context, model string, cfg *genai.LiveConnectConfig) (liveSession, error)

func (p *Provider) connectLive(ctx context.Context, model string, cfg *genai.LiveConnectConfig) (liveSession, error) {
	if p.liveConnect != nil {
		return p.liveConnect(ctx, model, cfg)
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      p.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: genai.HTTPOptions{APIVersion: apiVersion},
	})
	if err != nil {
		return nil, fmt.Errorf("gemini live transcription client: %w", err)
	}
	return client.Live.Connect(ctx, model, cfg)
}

// StartDictationStream implements [speechkit.DictationStreamProvider] with
// [LiveModel] on the Gemini Live API. opts.Model is used when it names a
// Gemini live transcription model; otherwise LiveModel.
//
// The transcription mode is Provider.Mode (smart by default), opts.Language
// becomes the BCP-47 language_codes hint unless it is multilanguage, and
// opts.Keyterms ride custom_vocabulary. Each turn's fragments surface as
// drafts when opts.InterimResults is on and as one final when the turn
// closes.
//
// Audio must be mono 16-bit PCM; its sample rate is declared per chunk.
// Anything else fails with [speechkit.ErrUnsupportedAudioFormat].
//
// Finalize sends audio_stream_end; Receive then returns the remaining finals
// followed by io.EOF, matching SpeechKit's one-provider-stream-per-segment
// model.
func (p *Provider) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	opts = stt.ResolveDictationStreamOptions(p.Name(), "stt.gemini.transcribe-live", opts, provideropts.Values{provideropts.OptionVocabularyBias: true}, nil)
	format = format.Normalized()
	if format.Channels != 1 || format.SampleRateHz <= 0 ||
		(format.Encoding != speaker.AudioEncodingLinear16 && format.Encoding != speaker.AudioEncodingPCM16) {
		return nil, fmt.Errorf(
			"gemini live transcription requires mono 16-bit PCM; got %s %d Hz %d channels: %w",
			format.Encoding, format.SampleRateHz, format.Channels, speechkit.ErrUnsupportedAudioFormat)
	}
	model := liveTranscribeModel(opts.Model)
	session, err := p.connectLive(ctx, model, p.liveConnectConfig(opts))
	if err != nil {
		return nil, fmt.Errorf("gemini live transcription connect: %w", err)
	}
	stream := &liveStream{
		session:   session,
		provider:  p.Name(),
		model:     model,
		language:  liveEventLanguage(opts.Language),
		sessionID: opts.SessionID,
		interim:   opts.InterimResults,
		mimeType:  fmt.Sprintf("audio/pcm;rate=%d", format.SampleRateHz),
		messages:  make(chan liveMessage, 64),
		closed:    make(chan struct{}),
	}
	go stream.read()
	return stream, nil
}

func (p *Provider) liveConnectConfig(opts speechkit.DictationStreamOptions) *genai.LiveConnectConfig {
	mode := genai.AudioTranscriptionConfigModeSmart
	if p.Mode == ModeVerbatim {
		mode = genai.AudioTranscriptionConfigModeVerbatim
	}
	return &genai.LiveConnectConfig{
		ResponseModalities: []genai.Modality{genai.ModalityText},
		InputAudioTranscription: &genai.AudioTranscriptionConfig{
			Mode:             mode,
			LanguageCodes:    languageCodes(opts.Language, nil),
			CustomVocabulary: nonEmpty(opts.Keyterms),
		},
	}
}

// liveTranscribeModel keeps a requested model only when it is a Gemini live
// transcription id; the batch gemini-3.5-transcribe is not served on the
// Live API.
func liveTranscribeModel(requested string) string {
	requested = strings.TrimSpace(requested)
	lower := strings.ToLower(requested)
	if strings.HasPrefix(lower, "gemini-") && strings.Contains(lower, "transcribe-live") {
		return requested
	}
	return LiveModel
}

// liveEventLanguage is the locale stamped on emitted events: the caller's
// BCP-47 value unchanged, or "" when multilanguage.
func liveEventLanguage(language string) string {
	if stt.IsMultilanguage(language) {
		return ""
	}
	return strings.TrimSpace(language)
}

type liveMessage struct {
	msg *genai.LiveServerMessage
	err error
}

// liveStream adapts a Gemini Live transcription session to
// speechkit.DictationStream. Receive owns the turn state; only finalizing and
// closing cross goroutines.
type liveStream struct {
	session   liveSession
	provider  string
	model     string
	language  string
	sessionID uint64
	interim   bool
	mimeType  string

	messages  chan liveMessage
	closed    chan struct{}
	closeOnce sync.Once

	sequence   atomic.Int64
	finalizing atomic.Bool

	segment  uint64
	text     strings.Builder
	detected string
	drainAt  time.Time
	done     bool
}

// read pumps session messages into the channel Receive selects on, because
// the SDK's Receive takes no context.
func (s *liveStream) read() {
	for {
		msg, err := s.session.Receive()
		select {
		case s.messages <- liveMessage{msg: msg, err: err}:
		case <-s.closed:
			return
		}
		if err != nil {
			return
		}
	}
}

func (s *liveStream) SendPCM(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{MIMEType: s.mimeType, Data: pcm},
	})
}

// Finalize ends the audio stream; the server then transcribes what is still
// buffered.
func (s *liveStream) Finalize(ctx context.Context) error {
	if s.finalizing.Swap(true) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.session.SendRealtimeInput(genai.LiveRealtimeInput{AudioStreamEnd: true})
}

func (s *liveStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	for {
		if s.done {
			return speechkit.DictationStreamEvent{}, io.EOF
		}
		event, ok, err := s.next(ctx)
		if err != nil || ok {
			return event, err
		}
	}
}

// next waits for one server message, the caller's context or, once
// finalizing, the drain deadline.
func (s *liveStream) next(ctx context.Context) (speechkit.DictationStreamEvent, bool, error) {
	var drain <-chan time.Time
	if s.finalizing.Load() {
		if s.drainAt.IsZero() {
			s.drainAt = time.Now().Add(liveDrainTimeout)
		}
		timer := time.NewTimer(time.Until(s.drainAt))
		defer timer.Stop()
		drain = timer.C
	}
	select {
	case <-ctx.Done():
		return speechkit.DictationStreamEvent{}, false, ctx.Err()
	case <-drain:
		// No turn_complete after audio_stream_end: commit what arrived.
		s.done = true
		event, ok := s.closeTurn()
		return event, ok, nil
	case in := <-s.messages:
		if in.err != nil {
			// A socket that ends after Finalize is the normal end of the
			// stream; before it, the error is the caller's to handle.
			if !s.finalizing.Load() {
				return speechkit.DictationStreamEvent{}, false, fmt.Errorf("gemini live transcription receive: %w", in.err)
			}
			s.done = true
			event, ok := s.closeTurn()
			return event, ok, nil
		}
		event, ok := s.handle(in.msg)
		return event, ok, nil
	}
}

// handle folds one server message into the turn state and reports whether
// it yields a dictation event. Transcript text never reaches an error or log.
func (s *liveStream) handle(msg *genai.LiveServerMessage) (speechkit.DictationStreamEvent, bool) {
	if msg == nil || msg.ServerContent == nil {
		// setupComplete, usage metadata and goAway carry no transcript.
		return speechkit.DictationStreamEvent{}, false
	}
	content := msg.ServerContent
	if tr := content.InputTranscription; tr != nil {
		s.text.WriteString(tr.Text)
		if code := strings.TrimSpace(tr.LanguageCode); code != "" {
			s.detected = code
		}
		if tr.Finished {
			return s.closeTurn()
		}
		if s.interim && tr.Text != "" {
			if text := strings.TrimSpace(s.text.String()); text != "" {
				return s.event(text, false), true
			}
		}
	}
	if content.TurnComplete {
		if s.finalizing.Load() {
			// The turn closed by audio_stream_end is the last one.
			s.done = true
		}
		return s.closeTurn()
	}
	return speechkit.DictationStreamEvent{}, false
}

// closeTurn emits the pending turn as a final and starts the next segment.
func (s *liveStream) closeTurn() (speechkit.DictationStreamEvent, bool) {
	text := strings.TrimSpace(s.text.String())
	s.text.Reset()
	if text == "" {
		return speechkit.DictationStreamEvent{}, false
	}
	event := s.event(text, true)
	s.segment++
	s.detected = ""
	return event, true
}

func (s *liveStream) event(text string, final bool) speechkit.DictationStreamEvent {
	return speechkit.DictationStreamEvent{
		Sequence:       s.sequence.Add(1),
		SessionID:      s.sessionID,
		SegmentID:      s.segment + 1,
		ProviderItemID: fmt.Sprintf("gemini:%d", s.segment+1),
		Text:           text,
		IsFinal:        final,
		Language:       stt.FirstNonEmptyTrimmed(s.language, s.detected),
		Provider:       s.provider,
		Model:          s.model,
	}
}

func (s *liveStream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.closed)
		err = s.session.Close()
	})
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
