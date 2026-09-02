package cascaded

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// -- fakes ------------------------------------------------------------

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

// -- tests ------------------------------------------------------------

func TestConnectRequiresSTT(t *testing.T) {
	p := NewProvider(Deps{Agent: &fakeAgent{}, TTS: &fakeTTS{}})
	err := p.Connect(context.Background(), SessionConfig{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Connect without STT = %v, want ErrNotConfigured", err)
	}
}

func TestConnectRequiresAgent(t *testing.T) {
	p := NewProvider(Deps{STT: &fakeSTT{}, TTS: &fakeTTS{}})
	err := p.Connect(context.Background(), SessionConfig{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Connect without Agent = %v, want ErrNotConfigured", err)
	}
}

func TestConnectAllowsNilTTS(t *testing.T) {
	p := NewProvider(Deps{STT: &fakeSTT{}, Agent: &fakeAgent{}})
	if err := p.Connect(context.Background(), SessionConfig{Locale: "en"}); err != nil {
		t.Fatalf("Connect should allow nil TTS (text-only mode): %v", err)
	}
	_ = p.Close()
}

func TestSendTextProducesFullRoundTrip(t *testing.T) {
	sttFake := &fakeSTT{}
	agent := &fakeAgent{response: "Hello back!"}
	ttsFake := &fakeTTS{audio: make([]byte, 4096)}
	p := newTestProvider(t, Deps{STT: sttFake, Agent: agent, TTS: ttsFake})

	if err := p.SendText("say hi"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	msgs := collectMessages(t, p, 2*time.Second)

	var sawInput, sawOutput, sawAudio bool
	for _, m := range msgs {
		if m.InputTranscript != "" && m.InputTranscriptDone {
			sawInput = true
		}
		if m.OutputTranscript == "Hello back!" && m.OutputTranscriptDone {
			sawOutput = true
		}
		if len(m.Audio) > 0 {
			sawAudio = true
		}
	}
	if !sawInput {
		t.Fatalf("expected InputTranscript message; got %d messages", len(msgs))
	}
	if !sawOutput {
		t.Fatalf("expected OutputTranscript='Hello back!'; got %+v", msgs)
	}
	if !sawAudio {
		t.Fatalf("expected audio chunks; got %d messages", len(msgs))
	}
	if agent.calls != 1 {
		t.Fatalf("agent should be called exactly once; got %d", agent.calls)
	}
	if ttsFake.calls != 1 {
		t.Fatalf("TTS should be called exactly once; got %d", ttsFake.calls)
	}
	if sttFake.calls != 0 {
		t.Fatalf("STT should NOT be called for SendText; got %d", sttFake.calls)
	}
}

func TestSendAudioTriggersAfterSilence(t *testing.T) {
	sttFake := &fakeSTT{text: "hello there"}
	agent := &fakeAgent{response: "hi!"}
	ttsFake := &fakeTTS{audio: []byte("audio-bytes")}
	p := newTestProvider(t, Deps{
		STT: sttFake, Agent: agent, TTS: ttsFake,
		Config: Config{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	_ = p.SendAudio(sineChunk(200, 10000))
	time.Sleep(60 * time.Millisecond)
	_ = p.SendAudio(silenceChunk(20))

	msgs := collectMessages(t, p, 2*time.Second)
	if sttFake.calls != 1 {
		t.Fatalf("STT should be called after silence; got %d", sttFake.calls)
	}
	var sawInput bool
	for _, m := range msgs {
		if m.InputTranscript == "hello there" && m.InputTranscriptDone {
			sawInput = true
		}
	}
	if !sawInput {
		t.Fatalf("expected STT transcript in messages; got %+v", msgs)
	}
}

func TestStreamingSpeakerFramesEmitInputTranscriptMetadata(t *testing.T) {
	sttFake := &fakeSTT{text: "batch final"}
	agent := &fakeAgent{response: "ok"}
	streamer := &fakeSpeakerStreamer{}
	p := NewProvider(Deps{
		STT:             sttFake,
		Agent:           agent,
		SpeakerStreamer: streamer,
		Config:          Config{SilenceTurnMs: 10_000, MinTurnMs: 50},
	})
	if err := p.Connect(context.Background(), SessionConfig{
		Locale: "en",
		Speaker: speaker.Options{
			Diarization:     true,
			PreferStreaming: true,
		},
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	streamer.mu.Lock()
	stream := streamer.stream
	started := streamer.started
	streamer.mu.Unlock()
	if started != 1 || stream == nil {
		t.Fatalf("speaker stream started=%d stream=%v", started, stream)
	}

	chunk := sineChunk(100, 10000)
	if err := p.SendAudio(chunk); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	if err := p.SendAudioStreamEnd(); err != nil {
		t.Fatalf("SendAudioStreamEnd: %v", err)
	}
	stream.mu.Lock()
	audioCount := len(stream.audio)
	ended := stream.ended
	stream.mu.Unlock()
	if audioCount != 1 || !ended {
		t.Fatalf("stream audioCount=%d ended=%v", audioCount, ended)
	}

	stream.frames <- &speaker.SpeakerFrame{
		Provider: "fake",
		Text:     "live partial",
		IsFinal:  false,
		Segment: &speaker.SpeakerSegment{
			SpeakerLabel:      "speaker_A",
			PersonID:          "p1",
			DisplayName:       "Alice",
			SpeakerConfidence: 0.77,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var msg *Message
	for {
		got, err := p.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if got.InputTranscript == "live partial" {
			msg = got
			break
		}
	}
	if msg.InputTranscript != "live partial" || msg.InputSpeakerLabel != "speaker_A" ||
		msg.InputPersonID != "p1" || msg.InputDisplayName != "Alice" || msg.InputSpeakerConfidence != 0.77 ||
		msg.InputTranscriptDone {
		t.Fatalf("message = %+v", msg)
	}
}

func TestAudioEndForcesImmediateTurn(t *testing.T) {
	sttFake := &fakeSTT{text: "explicit turn"}
	agent := &fakeAgent{response: "ok"}
	p := newTestProvider(t, Deps{
		STT: sttFake, Agent: agent,
		Config: Config{SilenceTurnMs: 10_000, MinTurnMs: 50},
	})

	_ = p.SendAudio(sineChunk(200, 12000))
	_ = p.SendAudioStreamEnd()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		sttFake.mu.Lock()
		calls := sttFake.calls
		sttFake.mu.Unlock()
		if calls == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("STT never fired after SendAudioStreamEnd")
}

func TestEmptySTTResultIsDropped(t *testing.T) {
	sttFake := &fakeSTT{text: ""}
	agent := &fakeAgent{response: "ignored"}
	p := newTestProvider(t, Deps{
		STT: sttFake, Agent: agent,
		Config: Config{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	_ = p.SendAudio(sineChunk(200, 8000))
	_ = p.SendAudioStreamEnd()

	time.Sleep(200 * time.Millisecond)
	if agent.calls != 0 {
		t.Fatalf("agent must NOT be called when STT returns empty text")
	}
}

func TestAgentErrorSurfacesAsMessage(t *testing.T) {
	sttFake := &fakeSTT{text: "hello"}
	agent := &fakeAgent{err: errors.New("llm down")}
	p := newTestProvider(t, Deps{
		STT: sttFake, Agent: agent,
		Config: Config{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	_ = p.SendAudioStreamEnd()
	_ = p.SendAudio(sineChunk(150, 10000))
	_ = p.SendAudioStreamEnd()

	msgs := collectMessages(t, p, 1*time.Second)
	var sawError bool
	for _, m := range msgs {
		if m.OutputTranscriptDone && strings.Contains(m.OutputTranscript, "turn_failed") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected turn_failed error in output; got %+v", msgs)
	}
}

func TestHistoryIsFedToAgent(t *testing.T) {
	sttFake := &fakeSTT{}
	agent := &fakeAgent{response: "fine"}
	p := newTestProvider(t, Deps{
		STT: sttFake, Agent: agent,
		Config: Config{HistoryTurns: 3, SilenceTurnMs: 50, MinTurnMs: 50},
	})

	_ = p.SendText("hello")
	_ = collectMessages(t, p, 500*time.Millisecond)

	_ = p.SendText("how are you")
	_ = collectMessages(t, p, 500*time.Millisecond)

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.calls < 2 {
		t.Fatalf("agent should have been called twice; got %d", agent.calls)
	}
	if !strings.Contains(agent.lastIn.LastTranscription, "User: hello") {
		t.Fatalf("history not propagated to agent; got LastTranscription=%q", agent.lastIn.LastTranscription)
	}
	if !strings.Contains(agent.lastIn.LastTranscription, "Assistant: fine") {
		t.Fatalf("history missing assistant reply; got %q", agent.lastIn.LastTranscription)
	}
}

func TestUpdateInstructionsAffectsFutureAgentTurns(t *testing.T) {
	sttFake := &fakeSTT{}
	agent := &fakeAgent{response: "roger"}
	p := NewProvider(Deps{STT: sttFake, Agent: agent})
	if err := p.Connect(context.Background(), SessionConfig{Locale: "en", SystemPrompt: "Original role."}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	if err := p.UpdateInstructions(context.Background(), SessionConfig{
		Locale:       "en",
		SystemPrompt: "Workflow role.\n\n[Current step: decide]\nDrive a decision.",
	}); err != nil {
		t.Fatalf("UpdateInstructions: %v", err)
	}
	if agent.calls != 0 {
		t.Fatalf("UpdateInstructions should not call the agent; got %d calls", agent.calls)
	}

	if err := p.SendText("next"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	_ = collectMessages(t, p, 1*time.Second)

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if !strings.Contains(agent.lastIn.SystemPrompt, "Drive a decision.") {
		t.Fatalf("SystemPrompt not passed to agent: %+v", agent.lastIn)
	}
}

func TestTTSAbsentStillReturnsTranscript(t *testing.T) {
	sttFake := &fakeSTT{text: "hi"}
	agent := &fakeAgent{response: "hello"}
	p := newTestProvider(t, Deps{
		STT: sttFake, Agent: agent,
		Config: Config{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	_ = p.SendAudio(sineChunk(200, 10000))
	_ = p.SendAudioStreamEnd()

	msgs := collectMessages(t, p, 1*time.Second)
	var hasAudio bool
	var hasOutput bool
	for _, m := range msgs {
		if len(m.Audio) > 0 {
			hasAudio = true
		}
		if m.OutputTranscript == "hello" && m.OutputTranscriptDone {
			hasOutput = true
		}
	}
	if hasAudio {
		t.Fatalf("no TTS configured; should be zero audio messages")
	}
	if !hasOutput {
		t.Fatalf("expected OutputTranscript=hello; got %+v", msgs)
	}
}

func TestChunkRMS(t *testing.T) {
	silent := silenceChunk(100)
	if rms := ChunkRMS(silent); rms != 0 {
		t.Fatalf("silent buffer RMS = %v, want 0", rms)
	}
	loud := sineChunk(100, 30000)
	if rms := ChunkRMS(loud); rms < 0.4 {
		t.Fatalf("loud sine RMS = %v, want >= 0.4", rms)
	}
}

func TestChunkAudio(t *testing.T) {
	out := ChunkAudio([]byte("1234567890"), 3)
	if len(out) != 4 {
		t.Fatalf("ChunkAudio split into %d chunks, want 4", len(out))
	}
	if string(out[3]) != "0" {
		t.Fatalf("last chunk = %q, want '0'", out[3])
	}
}

func TestPCMDurationMs(t *testing.T) {
	// 16 kHz S16 mono -> 32000 bytes per second.
	cases := []struct {
		bytes int
		ms    int64
	}{
		{0, 0},
		{32000, 1000},
		{16000, 500},
	}
	for _, c := range cases {
		got := PCMDurationMs(make([]byte, c.bytes))
		if got != c.ms {
			t.Fatalf("PCMDurationMs(%d bytes) = %d, want %d", c.bytes, got, c.ms)
		}
	}
}

// -- speaker stream reconnect ----------------------------------------

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
func (s *scriptStream) EndAudio(context.Context) error          { return nil }

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

func TestSpeakerStreamReconnectsAfterTransientDrop(t *testing.T) {
	origBackoff := speakerReconnectInitialBackoff
	speakerReconnectInitialBackoff = time.Millisecond
	t.Cleanup(func() { speakerReconnectInitialBackoff = origBackoff })

	streamer := &reconnectStreamer{}
	p := NewProvider(Deps{
		STT:             &fakeSTT{text: "x"},
		Agent:           &fakeAgent{response: "ok"},
		SpeakerStreamer: streamer,
		Config:          Config{SilenceTurnMs: 10_000, MinTurnMs: 50},
	})
	if err := p.Connect(context.Background(), SessionConfig{
		Locale:  "en",
		Speaker: speaker.Options{Diarization: true, PreferStreaming: true},
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	s0 := waitForStream(t, streamer, 0)
	s0.frames <- &speaker.SpeakerFrame{Provider: "fake", Text: "before drop",
		Segment: &speaker.SpeakerSegment{SpeakerLabel: "speaker_A"}}
	if got := receiveTranscript(t, p); got != "before drop" {
		t.Fatalf("first transcript = %q, want %q", got, "before drop")
	}

	// Drop the stream with a transient (non-EOF, non-cancel) error. The loop
	// must reconnect instead of dying for the rest of the session.
	s0.errc <- errors.New("websocket: connection reset by peer")
	waitForCount(t, streamer, 2)
	if !s0.isClosed() {
		t.Fatal("dropped stream was not closed before reconnect")
	}

	s1 := waitForStream(t, streamer, 1)
	s1.frames <- &speaker.SpeakerFrame{Provider: "fake", Text: "after reconnect",
		Segment: &speaker.SpeakerSegment{SpeakerLabel: "speaker_B"}}
	if got := receiveTranscript(t, p); got != "after reconnect" {
		t.Fatalf("post-reconnect transcript = %q, want %q", got, "after reconnect")
	}
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
