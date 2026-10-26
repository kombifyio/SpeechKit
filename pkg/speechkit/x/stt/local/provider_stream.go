package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon"
)

const (
	// streamFeedInterval is how often a live stream sends its audio and, with
	// drafts, asks for a provisional transcript of what is not yet in a
	// segment (a decode of at most 35 s).
	streamFeedInterval = time.Second
	// streamMaxChunkBytes bounds one audio call: 32 s of 16 kHz mono PCM, well
	// under the sidecar's limit.
	streamMaxChunkBytes = 1 << 20
	streamCallTimeout   = 2 * time.Minute
	streamFinishTimeout = 5 * time.Minute
	streamDraftBacklog  = 16
)

// SupportsDictationStream reports whether the model can transcribe a
// dictation while it is recorded: the five-value engine behind a Phonon model
// directory can; whisper.cpp and sherpa-onnx models are transcribed once the
// recording ends.
func (p *Provider) SupportsDictationStream() bool {
	return phonon.IsModelDir(p.ModelPath)
}

// StartDictationStream implements [speechkit.DictationStreamProvider] over the
// sidecar's stream routes. The audio reaches the engine while it is recorded;
// segments complete at the engine's pause-aligned 25–35 s windows, and with
// InterimResults a draft of the whole dictation so far arrives about every
// second. With CompleteCapture the only final is the transcript of the whole
// recording, the same text one transcription of the finished recording gives;
// otherwise every completed segment is a final.
func (p *Provider) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	if !p.SupportsDictationStream() {
		return nil, fmt.Errorf("local model %s does not stream", p.displayModel())
	}
	if !p.ready.Load() {
		return nil, fmt.Errorf("local %s not ready", engineForModel(p.ModelPath).serverName())
	}
	format = format.Normalized()
	pcm := format.Encoding == speaker.AudioEncodingLinear16 || format.Encoding == speaker.AudioEncodingPCM16
	if !pcm || format.Channels != 1 || format.Container != "" {
		return nil, fmt.Errorf("%w: local streams take raw 16-bit mono PCM, got %s/%d channels", speechkit.ErrUnsupportedAudioFormat, format.Encoding, format.Channels)
	}

	resolved := stt.ResolveTranscribeOptionsWithManifest(provideropts.LocalParakeetSTTManifest(), engineSherpa.profileID(),
		stt.TranscribeOptionsFromStream(opts).ForProvider(p.Name()), nil, nil)
	language := resolved.APILanguage()
	if language == "" {
		language = whisperCppAutoDetectLanguage
	}
	base := p.serverURL()
	query := url.Values{"rate": {strconv.Itoa(format.SampleRateHz)}, "language": {language}}
	var opened struct {
		ID string `json:"id"`
	}
	if err := p.streamCall(ctx, p.keyed(transcribeHTTPClient(p.client, streamCallTimeout, &p.Validation)), base+"/stream?"+query.Encode(), nil, &opened); err != nil {
		return nil, err
	}
	if opened.ID == "" {
		return nil, errors.New("local stream: the server returned no stream id")
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s := &localStream{
		p:         p,
		url:       base + "/stream/" + opened.ID,
		sessionID: opts.SessionID,
		model:     p.displayModel(),
		language:  stt.FirstNonEmptyTrimmed(resolved.APILanguage(), stt.LanguageMulti),
		drafts:    opts.InterimResults,
		complete:  opts.CompleteCapture,
		cancel:    cancel,
		finalize:  make(chan struct{}),
		events:    make(chan speechkit.DictationStreamEvent, streamDraftBacklog),
	}
	go s.run(runCtx)
	return s, nil
}

// streamCall posts body (nil for none) to target and decodes the JSON answer
// into out.
func (p *Provider) streamCall(ctx context.Context, client *http.Client, target string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("local stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("local stream: %w", stt.ClassifyTransportError("local", err))
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	raw, err := io.ReadAll(io.LimitReader(resp.Body, localMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("local stream response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return stt.HTTPError("local", resp, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("local stream response: %w", err)
	}
	return nil
}

type streamSegment struct {
	Text string `json:"text"`
}

// localStream is one dictation decoded by the sidecar while it is recorded.
// SendPCM only queues audio; one goroutine sends it, so the microphone never
// waits for a decode.
type localStream struct {
	p         *Provider
	url       string
	sessionID uint64
	model     string
	language  string
	drafts    bool
	complete  bool

	cancel   context.CancelFunc
	finalize chan struct{}
	events   chan speechkit.DictationStreamEvent

	mu        sync.Mutex
	pending   []byte
	finalized bool
	err       error
	finished  bool

	sequence int64
	// heard are the texts of the completed segments, for drafts.
	heard []string
	// nextDraft holds the next draft back for twice as long as the last one
	// took to decode, so drafts of a long dictation on a slow processor take
	// at most a third of its time and rarely delay the final.
	nextDraft time.Time

	closeOnce sync.Once
}

func (s *localStream) SendPCM(_ context.Context, pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.finalized {
		return errors.New("local stream: audio after Finalize")
	}
	s.pending = append(s.pending, pcm...)
	return nil
}

// Finalize sends the rest of the audio and asks for the final transcript.
func (s *localStream) Finalize(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.finalized {
		s.finalized = true
		close(s.finalize)
	}
	return nil
}

func (s *localStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	select {
	case event, ok := <-s.events:
		if ok {
			return event, nil
		}
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return speechkit.DictationStreamEvent{}, err
		}
		return speechkit.DictationStreamEvent{}, io.EOF
	case <-ctx.Done():
		return speechkit.DictationStreamEvent{}, ctx.Err()
	}
}

// Close stops the stream; the server forgets it, finished or not.
func (s *localStream) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		finished := s.finished
		s.mu.Unlock()
		if finished {
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.url, http.NoBody)
			if err != nil {
				return
			}
			if resp, err := s.p.client.Do(req); err == nil {
				_ = resp.Body.Close()
			}
		}()
	})
	return nil
}

// CompleteCapture implements [speechkit.CompleteCaptureStream].
func (s *localStream) CompleteCapture() bool { return s.complete }

func (s *localStream) run(ctx context.Context) {
	defer close(s.events)
	ticker := time.NewTicker(streamFeedInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.fail(ctx.Err())
			return
		case <-s.finalize:
			if err := s.flush(ctx, false); err != nil {
				s.fail(err)
				return
			}
			if err := s.finish(ctx); err != nil {
				s.fail(err)
			}
			return
		case <-ticker.C:
			if err := s.flush(ctx, s.drafts && !time.Now().Before(s.nextDraft)); err != nil {
				s.fail(err)
				return
			}
		}
	}
}

// flush sends the queued audio in chunks; with draft it asks for a
// provisional transcript with the last chunk and emits it.
func (s *localStream) flush(ctx context.Context, draft bool) error {
	s.mu.Lock()
	pending := s.pending
	s.pending = nil
	if len(pending)%2 == 1 {
		// A half sample waits for its other byte.
		s.pending = append(s.pending, pending[len(pending)-1])
		pending = pending[:len(pending)-1]
	}
	s.mu.Unlock()
	client := s.p.keyed(transcribeHTTPClient(s.p.client, streamCallTimeout, &s.p.Validation))
	for len(pending) > 0 {
		n := min(len(pending), streamMaxChunkBytes)
		chunk := pending[:n]
		pending = pending[n:]
		target := s.url + "/audio"
		withDraft := draft && len(pending) == 0
		if withDraft {
			target += "?partial=1"
		}
		started := time.Now()
		var answer struct {
			Segments []streamSegment `json:"segments"`
			Partial  *string         `json:"partial"`
		}
		if err := s.p.streamCall(ctx, client, target, chunk, &answer); err != nil {
			return err
		}
		if err := s.segments(ctx, answer.Segments); err != nil {
			return err
		}
		if withDraft {
			s.nextDraft = time.Now().Add(2 * time.Since(started))
		}
		if answer.Partial != nil {
			s.emitDraft(joinTranscript(append(append([]string(nil), s.heard...), *answer.Partial)...))
		}
	}
	return nil
}

// segments records completed segments and, for a live stream, emits each as
// a final.
func (s *localStream) segments(ctx context.Context, segments []streamSegment) error {
	for _, seg := range segments {
		s.heard = append(s.heard, seg.Text)
		if !s.complete && strings.TrimSpace(seg.Text) != "" {
			if err := s.emitFinal(ctx, speechkit.DictationStreamEvent{Text: strings.TrimSpace(seg.Text)}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *localStream) finish(ctx context.Context) error {
	var answer struct {
		Segments []streamSegment `json:"segments"`
		Text     string          `json:"text"`
		Words    []struct {
			Word  string  `json:"word"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"words"`
	}
	if err := s.p.streamCall(ctx, s.p.keyed(transcribeHTTPClient(s.p.client, streamFinishTimeout, &s.p.Validation)), s.url+"/finish", nil, &answer); err != nil {
		// The server may not have seen the call; Close drops the stream.
		return err
	}
	s.mu.Lock()
	s.finished = true
	s.mu.Unlock()
	if !s.complete {
		return s.segments(ctx, answer.Segments)
	}
	words := make([]speechkit.WordConfidence, 0, len(answer.Words))
	for _, w := range answer.Words {
		words = append(words, speechkit.WordConfidence{Text: w.Word, StartMs: int64(w.Start * 1000), EndMs: int64(w.End * 1000)})
	}
	return s.emitFinal(ctx, speechkit.DictationStreamEvent{Text: strings.TrimSpace(answer.Text), Words: words})
}

func (s *localStream) stamp(event speechkit.DictationStreamEvent, final bool) speechkit.DictationStreamEvent {
	s.sequence++
	event.Sequence = s.sequence
	event.SessionID = s.sessionID
	event.SegmentID = uint64(s.sequence)
	event.ProviderItemID = "local:stream"
	event.IsFinal = final
	event.Language = s.language
	event.Provider = s.p.Name()
	event.Model = s.model
	return event
}

// emitDraft drops the draft when the reader is behind; the next one
// supersedes it.
func (s *localStream) emitDraft(text string) {
	select {
	case s.events <- s.stamp(speechkit.DictationStreamEvent{Text: text}, false):
	default:
	}
}

func (s *localStream) emitFinal(ctx context.Context, event speechkit.DictationStreamEvent) error {
	select {
	case s.events <- s.stamp(event, true):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *localStream) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

// joinTranscript joins transcript pieces with single spaces, skipping empty
// ones.
func joinTranscript(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}
