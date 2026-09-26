package cascaded

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

type fakeSTT struct {
	mu    sync.Mutex
	text  string
	err   error
	calls int
}

func (f *fakeSTT) Route(_ context.Context, _ []byte, _ float64, _ stt.TranscribeOpts) (*stt.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &stt.Result{Text: f.text, Language: "en", Provider: "fake"}, nil
}

type fakeAgent struct {
	mu       sync.Mutex
	response string
	err      error
	lastIn   AgentInput
	calls    int
}

func (f *fakeAgent) Run(_ context.Context, in AgentInput) (AgentOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastIn = in
	if f.err != nil {
		return AgentOutput{}, f.err
	}
	return AgentOutput{Text: f.response, Action: "display"}, nil
}

type fakeTTS struct {
	mu    sync.Mutex
	audio []byte
	err   error
	calls int
}

type fakeSpeakerStreamer struct {
	mu      sync.Mutex
	started int
	stream  *fakeSpeakerStream
}

func (f *fakeSpeakerStreamer) StartSpeakerStream(_ context.Context, _ speaker.Options, _ speaker.AudioFormat) (speaker.SpeakerStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
	f.stream = &fakeSpeakerStream{frames: make(chan *speaker.SpeakerFrame, 4)}
	return f.stream, nil
}

type fakeSpeakerStream struct {
	mu     sync.Mutex
	frames chan *speaker.SpeakerFrame
	audio  [][]byte
	ended  bool
	closed bool
}

func (f *fakeSpeakerStream) SendAudio(_ context.Context, chunk []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audio = append(f.audio, append([]byte(nil), chunk...))
	return nil
}

func (f *fakeSpeakerStream) EndAudio(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = true
	return nil
}

func (f *fakeSpeakerStream) Receive(ctx context.Context) (*speaker.SpeakerFrame, error) {
	select {
	case frame, ok := <-f.frames:
		if !ok {
			return nil, io.EOF
		}
		return frame, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *fakeSpeakerStream) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	close(f.frames)
	return nil
}

func (f *fakeTTS) Synthesize(_ context.Context, _ string, _ tts.SynthesizeOpts) (*tts.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &tts.Result{Audio: f.audio, Format: "mp3"}, nil
}

// -- helpers ----------------------------------------------------------

// sineChunk generates N ms of S16LE mono 16 kHz audio at the given amplitude.
func sineChunk(ms int, amp int16) []byte {
	frames := 16000 * ms / 1000
	buf := make([]byte, frames*2)
	for i := 0; i < frames; i++ {
		t := float64(i) / 16000.0
		v := int16(float64(amp) * math.Sin(2*math.Pi*440.0*t))
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	return buf
}

// silenceChunk emits N ms of zero-PCM.
func silenceChunk(ms int) []byte {
	return make([]byte, 16000*ms/1000*2)
}

func newTestProvider(t *testing.T, deps Deps) *Provider {
	t.Helper()
	if deps.Config.SilenceTurnMs == 0 {
		deps.Config.SilenceTurnMs = 100
	}
	if deps.Config.MinTurnMs == 0 {
		deps.Config.MinTurnMs = 50
	}
	p := NewProvider(deps)
	if err := p.Connect(context.Background(), SessionConfig{Locale: "en"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func collectMessages(t *testing.T, p *Provider, timeout time.Duration) []*Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	out := []*Message{}
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		msg, err := p.Receive(ctx)
		cancel()
		if err != nil {
			break
		}
		if msg != nil {
			out = append(out, msg)
		}
	}
	return out
}

// reconnectStreamer hands out a fresh scriptStream on every StartSpeakerStream
// call so a test can drop one stream and assert delivery resumes on the next.
type reconnectStreamer struct {
	mu      sync.Mutex
	started int
	streams []*scriptStream
}

func (r *reconnectStreamer) StartSpeakerStream(_ context.Context, _ speaker.Options, _ speaker.AudioFormat) (speaker.SpeakerStream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started++
	st := &scriptStream{frames: make(chan *speaker.SpeakerFrame, 4), errc: make(chan error, 1)}
	r.streams = append(r.streams, st)
	return st, nil
}

func (r *reconnectStreamer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.started
}

func (r *reconnectStreamer) nth(i int) *scriptStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i < 0 || i >= len(r.streams) {
		return nil
	}
	return r.streams[i]
}

// scriptStream lets a test push frames and inject a transient Receive error.
type scriptStream struct {
	frames chan *speaker.SpeakerFrame
	errc   chan error
	mu     sync.Mutex
	closed bool
}

func (s *scriptStream) SendAudio(context.Context, []byte) error { return nil }

func (s *scriptStream) EndAudio(context.Context) error { return nil }

func (s *scriptStream) Receive(ctx context.Context) (*speaker.SpeakerFrame, error) {
	select {
	case f := <-s.frames:
		return f, nil
	case err := <-s.errc:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *scriptStream) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (s *scriptStream) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func waitForStream(t *testing.T, r *reconnectStreamer, idx int) *scriptStream {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s := r.nth(idx); s != nil {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stream %d never started", idx)
	return nil
}

func waitForCount(t *testing.T, r *reconnectStreamer, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.count() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stream count = %d, want >= %d", r.count(), want)
}

func receiveTranscript(t *testing.T, p *Provider) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		msg, err := p.Receive(ctx)
		cancel()
		if err != nil {
			continue
		}
		if msg != nil && msg.InputTranscript != "" {
			return msg.InputTranscript
		}
	}
	t.Fatal("no transcript message received")
	return ""
}
