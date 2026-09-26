package pipeline

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// fakeIdleCollector lets a test pin an idle-since timestamp so the
// RecordingController's silence watcher fires deterministically without
// requiring real audio capture.
type fakeIdleCollector struct {
	fakeCollector
	mu        sync.Mutex
	idleSince time.Time
}

func (c *fakeIdleCollector) IdleSince() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.idleSince
}

func (c *fakeIdleCollector) setIdleSince(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.idleSince = t
}

// fakeAudioIdleCollector pins audio-anchored idle facts so the
// RecordingController's audio idle watcher runs deterministically.
type fakeAudioIdleCollector struct {
	fakeCollector
	mu        sync.Mutex
	silence   time.Duration
	lastFrame time.Time
}

func (c *fakeAudioIdleCollector) IdleAudio() (time.Duration, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.silence, c.lastFrame
}

func (c *fakeAudioIdleCollector) setIdleAudio(silence time.Duration, lastFrame time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.silence = silence
	c.lastFrame = lastFrame
}

type fakeRecorder struct {
	startErr   error
	stopErr    error
	stopPCM    []byte
	started    bool
	pcmHandler func([]byte)
}

func (r *fakeRecorder) Start() error {
	if r.startErr != nil {
		return r.startErr
	}
	r.started = true
	return nil
}

func (r *fakeRecorder) Stop() ([]byte, error) {
	r.started = false
	return append([]byte(nil), r.stopPCM...), r.stopErr
}

func (r *fakeRecorder) SetPCMHandler(handler func([]byte)) {
	r.pcmHandler = handler
}

// fakePooledRecorder implements PooledPCMRecorder on top of
// fakeRecorder so tests can verify the controller prefers the
// pool-aware handler and releases every frame.
type fakePooledRecorder struct {
	fakeRecorder
	pooledHandler func(buf []byte, release func())
}

func (r *fakePooledRecorder) SetPooledPCMHandler(handler func(buf []byte, release func())) {
	r.pooledHandler = handler
}

type fakeSubmitter struct {
	jobs []speechkit.TranscriptionJob
	err  error
}

func (s *fakeSubmitter) Submit(job speechkit.TranscriptionJob) error {
	if s.err != nil {
		return s.err
	}
	s.jobs = append(s.jobs, job.Clone())
	return nil
}

type fakeDictationStreamProvider struct {
	stream  *fakeDictationStream
	err     error
	opts    []speechkit.DictationStreamOptions
	formats []speaker.AudioFormat
}

func (p *fakeDictationStreamProvider) StartDictationStream(_ context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	if p.err != nil {
		return nil, p.err
	}
	p.opts = append(p.opts, opts)
	p.formats = append(p.formats, format)
	if p.stream == nil {
		p.stream = newFakeDictationStream(speechkit.DictationStreamEvent{})
	}
	return p.stream, nil
}

type blockingDictationStreamProvider struct {
	block time.Duration
}

func (p *blockingDictationStreamProvider) StartDictationStream(ctx context.Context, _ speechkit.DictationStreamOptions, _ speaker.AudioFormat) (speechkit.DictationStream, error) {
	timer := time.NewTimer(p.block)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return newFakeDictationStream(speechkit.DictationStreamEvent{}), nil
	}
}

type orderedDictationStreamProvider struct {
	recorder           *fakeRecorder
	sawRecorderStarted bool
}

func (p *orderedDictationStreamProvider) StartDictationStream(_ context.Context, _ speechkit.DictationStreamOptions, _ speaker.AudioFormat) (speechkit.DictationStream, error) {
	if p.recorder != nil {
		p.sawRecorderStarted = p.recorder.started
	}
	return newFakeDictationStream(speechkit.DictationStreamEvent{}), nil
}

type fakeDictationStream struct {
	finalEvent speechkit.DictationStreamEvent
	events     chan speechkit.DictationStreamEvent
	finalOnce  sync.Once
	closeOnce  sync.Once
	mu         sync.Mutex
	pcm        [][]byte
	finalized  bool
	closed     bool
}

func newFakeDictationStream(finalEvent speechkit.DictationStreamEvent) *fakeDictationStream {
	return &fakeDictationStream{
		finalEvent: finalEvent,
		events:     make(chan speechkit.DictationStreamEvent, 2),
	}
}

func (s *fakeDictationStream) SendPCM(_ context.Context, pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pcm = append(s.pcm, append([]byte(nil), pcm...))
	return nil
}

func (s *fakeDictationStream) Finalize(context.Context) error {
	s.mu.Lock()
	s.finalized = true
	s.mu.Unlock()
	s.finalOnce.Do(func() {
		if strings.TrimSpace(s.finalEvent.Text) != "" || len(s.finalEvent.Words) > 0 {
			s.events <- s.finalEvent
		}
		close(s.events)
	})
	return nil
}

func (s *fakeDictationStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	select {
	case event, ok := <-s.events:
		if !ok {
			return speechkit.DictationStreamEvent{}, io.EOF
		}
		return event, nil
	case <-ctx.Done():
		return speechkit.DictationStreamEvent{}, ctx.Err()
	}
}

func (s *fakeDictationStream) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	})
	return nil
}

func (s *fakeDictationStream) pcmFrames() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pcm)
}

type fakeDictationStreamSink struct {
	mu     sync.Mutex
	events []speechkit.DictationStreamEvent
	opts   []speechkit.DictationStreamSinkOptions
}

func (s *fakeDictationStreamSink) HandleDictationStreamEvent(_ context.Context, event speechkit.DictationStreamEvent, opts speechkit.DictationStreamSinkOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	s.opts = append(s.opts, opts)
	return nil
}

func (s *fakeDictationStreamSink) finalEvents() []speechkit.DictationStreamEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []speechkit.DictationStreamEvent
	for _, event := range s.events {
		if event.IsFinal {
			out = append(out, event)
		}
	}
	return out
}

type fakeObserver struct {
	states  []string
	logs    []string
	onState func(status, text string)
}

func (o *fakeObserver) OnState(status, text string) {
	if o.onState != nil {
		o.onState(status, text)
	}
	o.states = append(o.states, status+":"+text)
}

func (o *fakeObserver) OnLog(message, kind string) {
	o.logs = append(o.logs, kind+":"+message)
}

func (o *fakeObserver) hasLog(message string) bool {
	for _, log := range o.logs {
		if strings.Contains(log, message) {
			return true
		}
	}
	return false
}

type fakeCollector struct {
	feedErr       error
	segments      []dictationSegment
	readySegments []dictationSegment
	fedPCM        [][]byte
}

type dictationSegment struct {
	pcm       []byte
	paragraph bool
}

func (c *fakeCollector) FeedPCM(pcm []byte) error {
	c.fedPCM = append(c.fedPCM, append([]byte(nil), pcm...))
	return c.feedErr
}

func (c *fakeCollector) CollectStopSegments(_ []byte) ([]speechkit.AudioSegment, error) {
	segments := make([]speechkit.AudioSegment, 0, len(c.segments))
	for _, segment := range c.segments {
		segments = append(segments, speechkit.AudioSegment{PCM: segment.pcm, Paragraph: segment.paragraph})
	}
	return segments, nil
}

func (c *fakeCollector) DrainReadySegments() []speechkit.AudioSegment {
	segments := make([]speechkit.AudioSegment, 0, len(c.readySegments))
	for _, segment := range c.readySegments {
		segments = append(segments, speechkit.AudioSegment{PCM: segment.pcm, Paragraph: segment.paragraph})
	}
	c.readySegments = nil
	return segments
}

func makePCMForDuration(d time.Duration) []byte {
	if d <= 0 {
		return nil
	}
	const bytesPerSecond = 16000 * 2
	return make([]byte, int(d.Seconds()*bytesPerSecond))
}
