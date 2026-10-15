//go:build linux

package dictation

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/pipeline"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

type captureResult struct {
	event speechkit.DictationStreamEvent
	err   error
}

type captureFailure struct{ code, message string }

func (e *captureFailure) Error() string { return e.message }

func captureFailureDetails(err error, fallback, message string) (string, string) {
	var failure *captureFailure
	if errors.As(err, &failure) {
		return failure.code, failure.message
	}
	return fallback, message
}

// completeRecordingStream retains the recording when the selected provider
// cannot upload a complete capture while recording. Its live socket supplies
// drafts only; one batch transcription supplies the sole committed final.
type completeRecordingStream struct {
	ctx             context.Context
	cancel          context.CancelFunc
	stopDrafts      context.CancelFunc
	draftCtx        context.Context
	draftPCM        chan []byte
	live            speechkit.DictationStream
	batch           Transcriber
	opts            speechkit.DictationStreamOptions
	format          speaker.AudioFormat
	maxBytes        int64
	maxAudioSeconds int
	mu              sync.Mutex
	pcm             []byte
	finalized       bool
	drafts          chan speechkit.DictationStreamEvent
	result          chan captureResult
	finalRead       bool // Receive has a single consumer.
	closeOnce       sync.Once
}

func newCompleteRecordingStream(parent context.Context, stream speechkit.DictationStream, batch Transcriber, opts speechkit.DictationStreamOptions, format speaker.AudioFormat, maxBytes int64) *completeRecordingStream {
	ctx, cancel := context.WithCancel(parent)
	draftCtx, stop := context.WithCancel(ctx)
	s := &completeRecordingStream{ctx: ctx, cancel: cancel, draftCtx: draftCtx, live: stream, batch: batch, opts: opts, format: format.Normalized(), maxBytes: maxBytes, drafts: make(chan speechkit.DictationStreamEvent, 16), draftPCM: make(chan []byte, 16), result: make(chan captureResult, 1)}
	var stopOnce sync.Once
	s.stopDrafts = func() {
		stopOnce.Do(func() {
			stop()
			// A provider close handshake must not block capture or finalization.
			if stream != nil {
				go func() { _ = stream.Close() }()
			}
		})
	}
	if !opts.InterimResults || stream == nil {
		s.stopDrafts()
		return s
	}
	go s.forwardDrafts(draftCtx)
	go s.receiveDrafts(draftCtx)
	return s
}

func (*completeRecordingStream) CompleteCapture() bool { return true }

func (s *completeRecordingStream) SendPCM(ctx context.Context, pcm []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.finalized {
		s.mu.Unlock()
		return io.ErrClosedPipe
	}
	if len(pcm)%(2*s.format.Channels) != 0 {
		s.mu.Unlock()
		return &captureFailure{StreamErrFormatUnsupported, "PCM frame is not sample-aligned"}
	}
	if s.maxBytes > 0 && int64(len(s.pcm))+int64(len(pcm)) > s.maxBytes {
		s.mu.Unlock()
		return &captureFailure{StreamErrPayloadTooLarge, "Complete recording exceeds the batch upload limit; no partial transcript was committed."}
	}
	if s.maxAudioSeconds > 0 && int64(len(s.pcm))+int64(len(pcm)) > int64(s.maxAudioSeconds)*int64(s.format.SampleRateHz)*int64(s.format.Channels)*2 {
		s.mu.Unlock()
		return &captureFailure{StreamErrAudioTooLong, "Complete recording exceeds the decoded audio duration limit."}
	}
	s.pcm = append(s.pcm, pcm...)
	s.mu.Unlock()
	// Draft upload is best effort. A stalled socket cannot block microphone
	// admission, finalization or Stop; queue overflow simply disables drafts.
	if s.opts.InterimResults && s.draftCtx.Err() == nil {
		select {
		case s.draftPCM <- append([]byte(nil), pcm...):
		default:
			s.stopDrafts()
		}
	}
	return nil
}

// providerCaptureTranscriber keeps a router-resolved batch-only choice pinned;
// it preserves that provider's recognition overrides and never selects another.
type providerCaptureTranscriber struct{ provider stt.STTProvider }

func (p providerCaptureTranscriber) Route(ctx context.Context, pcm []byte, _ float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	return p.provider.Transcribe(ctx, pcm, opts.ForProvider(p.provider.Name()))
}

func (s *completeRecordingStream) Finalize(context.Context) error {
	s.mu.Lock()
	if s.finalized {
		s.mu.Unlock()
		return nil
	}
	s.finalized = true
	pcm := s.pcm
	s.pcm = nil
	s.mu.Unlock()
	s.stopDrafts()
	go func() {
		result := s.transcribe(pcm)
		select {
		case s.result <- result:
		case <-s.ctx.Done():
		}
	}()
	return nil
}

func (s *completeRecordingStream) transcribe(pcm []byte) captureResult {
	if len(pcm) == 0 {
		return captureResult{err: &captureFailure{StreamErrEmptyTranscript, "No audio was recorded."}}
	}
	duration := float64(len(pcm)) / float64(s.format.SampleRateHz*s.format.Channels*2)
	if s.maxAudioSeconds > 0 && duration > float64(s.maxAudioSeconds) {
		return captureResult{err: &captureFailure{StreamErrAudioTooLong, "Complete recording exceeds the decoded audio duration limit."}}
	}
	// Use the same normalization primitive as the normal batch ingress.
	pcm, err := audio.NormalizePCM16Mono(pcm, s.format.SampleRateHz, s.format.Channels, 16000)
	if err != nil {
		return captureResult{err: err}
	}
	duration = audio.PCMDurationSecs(pcm)
	result, err := s.batch.Route(s.ctx, pcm, duration, stt.TranscribeOptionsFromStream(s.opts))
	if err != nil {
		return captureResult{err: err}
	}
	if result == nil {
		return captureResult{err: errors.New("complete-recording transcription returned no result")}
	}
	return captureResult{event: speechkit.DictationStreamEvent{Text: result.Text, IsFinal: true, Language: result.Language, Provider: result.Provider, Model: result.Model, Confidence: result.Confidence, Words: result.Words, Speakers: result.Speakers}}
}

func (s *completeRecordingStream) forwardDrafts(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pcm := <-s.draftPCM:
			if ctx.Err() != nil {
				return
			}
			if err := s.live.SendPCM(ctx, pcm); err != nil {
				s.stopDrafts()
				return
			}
		}
	}
}

func (s *completeRecordingStream) receiveDrafts(ctx context.Context) {
	var committed []string
	for {
		event, err := s.live.Receive(ctx)
		if err != nil {
			s.stopDrafts()
			return // Drafts are best effort; the full recording remains authoritative.
		}
		text := pipeline.JoinTranscriptFragments(append(append([]string(nil), committed...), event.Text)...)
		if event.IsFinal {
			committed = append(committed, event.Text)
		}
		event.Text, event.IsFinal = text, false
		if s.opts.InterimResults {
			select {
			case s.drafts <- event:
			case <-ctx.Done():
				return
			default: // A newer draft supersedes this one.
			}
		}
	}
}

func (s *completeRecordingStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	if s.finalRead {
		return speechkit.DictationStreamEvent{}, io.EOF
	}
	select {
	case <-ctx.Done():
		return speechkit.DictationStreamEvent{}, ctx.Err()
	case <-s.ctx.Done():
		return speechkit.DictationStreamEvent{}, s.ctx.Err()
	case result := <-s.result:
		s.finalRead = true
		return result.event, result.err
	case event := <-s.drafts:
		return event, nil
	}
}

func (s *completeRecordingStream) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.stopDrafts()
		s.mu.Lock()
		s.pcm = nil
		s.mu.Unlock()
	})
	return nil
}
