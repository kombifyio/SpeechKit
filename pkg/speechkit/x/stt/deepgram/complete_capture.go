package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/pipeline"
)

const (
	// A draft socket that falls this far behind the microphone is dropped
	// rather than allowed to hold audio back from the upload.
	draftFeedBacklogFrames = 256
	draftFeedEventBacklog  = 16
	// The draft socket dials in the background, so it can take longer than a
	// handshake the caller would have to wait for.
	draftFeedDialTimeout = 3 * time.Second
)

// completeCaptureStream is the [speechkit.DictationStream] behind
// DictationStreamOptions.CompleteCapture. It sends the capture to the Listen
// REST endpoint as the request body while it is being recorded, so the
// transcript of the finished recording is ready a round trip after Finalize,
// and returns that transcript as its only final.
//
// The live socket transcribes a moving window and finalizes it in chunks;
// measured on real German/English dictation it loses words at those chunk
// boundaries and at the end of the audio that the REST model returns for the
// same recording. With InterimResults the live socket still runs alongside,
// for drafts only.
type completeCaptureStream struct {
	provider  string
	model     string
	sessionID uint64

	body   *io.PipeWriter
	cancel context.CancelFunc
	result chan completeCaptureResult

	drafts   *draftFeed
	draftCh  <-chan speechkit.DictationStreamEvent
	sequence atomic.Int64

	sentBytes   atomic.Int64
	finalizedAt atomic.Int64
	delivered   atomic.Bool
	closeOnce   sync.Once
}

type completeCaptureResult struct {
	event speechkit.DictationStreamEvent
	err   error
}

// completeCaptureLog carries what the latency line reports about one upload.
type completeCaptureLog struct {
	language       string
	languageSource string
	keytermCount   int
}

func (p *Provider) startCompleteCaptureStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	format = format.Normalized()
	model := stt.FirstNonEmptyTrimmed(opts.Model, p.Model, "nova-3")
	resolved := p.resolveOptions(model, stt.TranscribeOptionsFromStream(opts).ForProvider(p.Name()))
	endpoint, err := p.deepgramListenEndpoint(model, resolved, &format)
	if err != nil {
		return nil, err
	}

	// The caller's context only bounds the handshake (the recording
	// controller cancels it as soon as Start returns); the upload lives until
	// Close.
	uploadCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	bodyReader, bodyWriter := io.Pipe()
	req, err := http.NewRequestWithContext(uploadCtx, http.MethodPost, endpoint, bodyReader)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deepgram complete capture request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+p.APIKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	apiLanguage := deepgramResolvedLanguage(resolved)
	stream := &completeCaptureStream{
		provider:  p.Name(),
		model:     model,
		sessionID: opts.SessionID,
		body:      bodyWriter,
		cancel:    cancel,
		result:    make(chan completeCaptureResult, 1),
	}
	go stream.upload(p, req, bodyReader, completeCaptureLog{
		language:       apiLanguage,
		languageSource: deepgramLanguageSource(resolved),
		keytermCount:   len(resolved.Keyterms),
	})

	if opts.InterimResults {
		liveOpts := opts
		liveOpts.CompleteCapture = false
		stream.drafts = p.startDraftFeed(uploadCtx, liveOpts, format)
		stream.draftCh = stream.drafts.events
	}
	return stream, nil
}

// completeCaptureClient carries the upload. It has no whole-request timeout,
// because the request lasts as long as the recording (the upload context and
// the caller's wait after Finalize bound it instead), and it opens a
// connection per upload: a request body that is a live recording cannot be
// replayed, so it must not start on a pooled connection the server is about
// to close. The handshake happens while the user is still speaking.
func (p *Provider) completeCaptureClient() *http.Client {
	if p.uploadClient != nil {
		return p.uploadClient
	}
	client := *p.client
	client.Timeout = 0
	return &client
}

func (s *completeCaptureStream) upload(p *Provider, req *http.Request, body *io.PipeReader, info completeCaptureLog) {
	started := time.Now()
	event, err := s.transcribe(p, req, info.language)
	// Whatever ended the request, a writer still blocked on the body must
	// not wait for a reader that is gone.
	_ = body.CloseWithError(io.ErrClosedPipe)
	finalizeToTranscript := int64(-1)
	if at := s.finalizedAt.Load(); at > 0 {
		finalizeToTranscript = time.Since(time.Unix(0, at)).Milliseconds()
	}
	p.log().Debug("deepgram.complete_capture", // #nosec G706 -- slog writes provider telemetry as structured attributes, not interpolated log text.
		"total_ms", time.Since(started).Milliseconds(),
		"finalize_to_transcript_ms", finalizeToTranscript,
		"audio_bytes", s.sentBytes.Load(),
		"provider", s.provider,
		"model", s.model,
		"language", info.language,
		"language_source", info.languageSource,
		"keyterm_count", info.keytermCount,
		"ok", err == nil,
	)
	s.result <- completeCaptureResult{event: event, err: err}
}

func (s *completeCaptureStream) transcribe(p *Provider, req *http.Request, apiLanguage string) (speechkit.DictationStreamEvent, error) {
	resp, err := p.completeCaptureClient().Do(req)
	if err != nil {
		return speechkit.DictationStreamEvent{}, fmt.Errorf("deepgram complete capture request: %w", stt.ClassifyTransportError("deepgram", err))
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	raw, err := io.ReadAll(io.LimitReader(resp.Body, deepgramMaxResponseBytes))
	if err != nil {
		return speechkit.DictationStreamEvent{}, fmt.Errorf("deepgram complete capture response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return speechkit.DictationStreamEvent{}, stt.HTTPError("deepgram", resp, raw)
	}
	var parsed deepgramResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return speechkit.DictationStreamEvent{}, fmt.Errorf("deepgram complete capture parse: %w", err)
	}
	text, confidence := parsed.bestTranscript()
	sequence := s.sequence.Add(1)
	return speechkit.DictationStreamEvent{
		Sequence:       sequence,
		SessionID:      s.sessionID,
		SegmentID:      completeCaptureSegmentID(sequence),
		ProviderItemID: "deepgram:complete-capture",
		Text:           text,
		IsFinal:        true,
		Language:       stt.FirstNonEmptyTrimmed(parsed.detectedLanguage(), apiLanguage, deepgramCodeSwitchingLanguage()),
		Provider:       s.provider,
		Model:          s.model,
		Confidence:     confidence,
		Words:          parsed.transcriptWords(),
	}, nil
}

func (s *completeCaptureStream) SendPCM(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	// The write blocks while the connection is not taking data. A caller that
	// gives up on the send must get out, and an upload with a hole in it is
	// worthless, so cancellation ends the upload.
	release := context.AfterFunc(ctx, func() {
		s.cancel()
		_ = s.body.CloseWithError(context.Cause(ctx))
	})
	_, err := s.body.Write(pcm)
	release()
	if err != nil {
		return fmt.Errorf("deepgram complete capture upload: %w", err)
	}
	s.sentBytes.Add(int64(len(pcm)))
	s.drafts.send(pcm)
	return nil
}

// Finalize ends the request body; Deepgram transcribes once it has all of it.
func (s *completeCaptureStream) Finalize(context.Context) error {
	s.finalizedAt.CompareAndSwap(0, time.Now().UnixNano())
	err := s.body.Close()
	s.drafts.stop()
	return err
}

func (s *completeCaptureStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	for {
		if s.delivered.Load() {
			return speechkit.DictationStreamEvent{}, io.EOF
		}
		select {
		case draft, ok := <-s.draftCh:
			if !ok {
				s.draftCh = nil
				continue
			}
			sequence := s.sequence.Add(1)
			draft.Sequence, draft.SegmentID = sequence, completeCaptureSegmentID(sequence)
			draft.SessionID = s.sessionID
			return draft, nil
		case result := <-s.result:
			s.delivered.Store(true)
			if result.err != nil {
				return speechkit.DictationStreamEvent{}, result.err
			}
			return result.event, nil
		case <-ctx.Done():
			return speechkit.DictationStreamEvent{}, ctx.Err()
		}
	}
}

func (s *completeCaptureStream) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		_ = s.body.CloseWithError(io.ErrClosedPipe)
		s.drafts.stop()
	})
	return nil
}

// CompleteCapture implements [speechkit.CompleteCaptureStream].
func (*completeCaptureStream) CompleteCapture() bool { return true }

// draftFeed mirrors the capture into a live Listen socket and reports what it
// hears as drafts of the whole dictation so far. It is best effort: it never
// delays or fails the upload, and none of its text is committed.
type draftFeed struct {
	cancel context.CancelFunc
	done   <-chan struct{}
	pcm    chan []byte
	events chan speechkit.DictationStreamEvent

	mu       sync.Mutex
	live     speechkit.DictationStream
	stopOnce sync.Once
}

// startDraftFeed opens the live socket in the background, so neither the
// caller nor the upload waits for its handshake; audio sent meanwhile is
// queued. A socket that cannot be opened means no drafts, not an error.
func (p *Provider) startDraftFeed(runCtx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) *draftFeed {
	feedCtx, cancel := context.WithCancel(runCtx)
	feed := &draftFeed{
		cancel: cancel,
		done:   feedCtx.Done(),
		pcm:    make(chan []byte, draftFeedBacklogFrames),
		events: make(chan speechkit.DictationStreamEvent, draftFeedEventBacklog),
	}
	go feed.run(feedCtx, func(dialCtx context.Context) (speechkit.DictationStream, error) {
		return p.startLiveDictationStream(dialCtx, opts, format)
	}, p.log())
	return feed
}

func (f *draftFeed) run(ctx context.Context, dial func(context.Context) (speechkit.DictationStream, error), log *slog.Logger) {
	defer close(f.events)
	dialCtx, dialDone := context.WithTimeout(ctx, draftFeedDialTimeout)
	live, err := dial(dialCtx)
	dialDone()
	if err != nil {
		log.Debug("deepgram draft socket unavailable; continuing without drafts", "err", err) // #nosec G706 -- structured attribute, not interpolated log text.
		f.stop()
		return
	}
	f.mu.Lock()
	f.live = live
	f.mu.Unlock()
	if ctx.Err() != nil {
		// Stopped while dialing; stop() could not see the socket yet.
		closeDraftSocket(live)
		return
	}
	go f.forward(ctx, live)
	f.receive(ctx, live)
}

func (f *draftFeed) send(pcm []byte) {
	if f == nil {
		return
	}
	select {
	case <-f.done:
		return
	default:
	}
	select {
	case f.pcm <- append([]byte(nil), pcm...):
	default:
		f.stop()
	}
}

func (f *draftFeed) forward(ctx context.Context, live speechkit.DictationStream) {
	for {
		select {
		case <-ctx.Done():
			return
		case pcm := <-f.pcm:
			if err := live.SendPCM(ctx, pcm); err != nil {
				f.stop()
				return
			}
		}
	}
}

func (f *draftFeed) receive(ctx context.Context, live speechkit.DictationStream) {
	var heard []string
	for {
		event, err := live.Receive(ctx)
		if err != nil {
			return
		}
		text := pipeline.JoinTranscriptFragments(append(append([]string(nil), heard...), event.Text)...)
		if event.IsFinal {
			heard = append(heard, event.Text)
		}
		event.IsFinal = false
		event.Text = text
		select {
		case f.events <- event:
		default:
			// The reader is busy; the next draft supersedes this one.
		}
	}
}

func (f *draftFeed) stop() {
	if f == nil {
		return
	}
	f.stopOnce.Do(func() {
		f.cancel()
		f.mu.Lock()
		live := f.live
		f.mu.Unlock()
		if live != nil {
			closeDraftSocket(live)
		}
	})
}

// closeDraftSocket closes in the background: closing a WebSocket waits for
// the peer's close frame, and the upload that stops the drafts must not wait
// with it.
func closeDraftSocket(live speechkit.DictationStream) {
	go func() { _ = live.Close() }()
}

// completeCaptureSegmentID maps the stream's positive sequence onto the
// unsigned segment ID; the counter starts at 1, so 0 is never a real segment.
func completeCaptureSegmentID(sequence int64) uint64 {
	if sequence <= 0 {
		return 0
	}
	return uint64(sequence)
}
