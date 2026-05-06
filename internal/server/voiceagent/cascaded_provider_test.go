//go:build linux

package voiceagent

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// â”€â”€ fakes â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

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
	lastIn   flows.AgentInput
	calls    int
}

func (f *fakeAgent) Run(_ context.Context, in flows.AgentInput) (flows.AgentOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastIn = in
	if f.err != nil {
		return flows.AgentOutput{}, f.err
	}
	return flows.AgentOutput{Text: f.response, Action: "display"}, nil
}

type fakeTTS struct {
	mu    sync.Mutex
	audio []byte
	err   error
	calls int
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

// â”€â”€ helpers â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

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

func newTestProvider(t *testing.T, deps CascadedDeps) *CascadedProvider {
	t.Helper()
	// Short silence threshold so the tests don't sleep for a second per turn.
	if deps.Config.SilenceTurnMs == 0 {
		deps.Config.SilenceTurnMs = 100
	}
	if deps.Config.MinTurnMs == 0 {
		deps.Config.MinTurnMs = 50
	}
	p := NewCascadedProvider(deps)
	if err := p.Connect(context.Background(), LiveConfigFrame{Locale: "en"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// collectMessages drains Receive() for up to `timeout`, returning all
// messages it read.
func collectMessages(t *testing.T, p *CascadedProvider, timeout time.Duration) []*LiveMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	out := []*LiveMessage{}
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

// â”€â”€ tests â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

func TestCascaded_ConnectRequiresSTT(t *testing.T) {
	p := NewCascadedProvider(CascadedDeps{Agent: &fakeAgent{}, TTS: &fakeTTS{}})
	err := p.Connect(context.Background(), LiveConfigFrame{})
	if err == nil {
		t.Fatalf("expected Connect error without STT")
	}
}

func TestCascaded_ConnectRequiresAgent(t *testing.T) {
	p := NewCascadedProvider(CascadedDeps{STT: &fakeSTT{}, TTS: &fakeTTS{}})
	err := p.Connect(context.Background(), LiveConfigFrame{})
	if err == nil {
		t.Fatalf("expected Connect error without Agent")
	}
}

func TestCascaded_ConnectAllowsNilTTS(t *testing.T) {
	p := NewCascadedProvider(CascadedDeps{STT: &fakeSTT{}, Agent: &fakeAgent{}})
	if err := p.Connect(context.Background(), LiveConfigFrame{Locale: "en"}); err != nil {
		t.Fatalf("Connect should allow nil TTS (text-only mode): %v", err)
	}
	_ = p.Close()
}

func TestCascaded_SendTextProducesFullRoundTrip(t *testing.T) {
	sttFake := &fakeSTT{}
	agent := &fakeAgent{response: "Hello back!"}
	ttsFake := &fakeTTS{audio: make([]byte, 4096)}
	p := newTestProvider(t, CascadedDeps{STT: sttFake, Agent: agent, TTS: ttsFake})

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
	// STT should NOT be called for text-only input.
	if sttFake.calls != 0 {
		t.Fatalf("STT should NOT be called for SendText; got %d", sttFake.calls)
	}
}

func TestCascaded_SendAudioTriggersAfterSilence(t *testing.T) {
	sttFake := &fakeSTT{text: "hello there"}
	agent := &fakeAgent{response: "hi!"}
	ttsFake := &fakeTTS{audio: []byte("audio-bytes")}
	p := newTestProvider(t, CascadedDeps{
		STT: sttFake, Agent: agent, TTS: ttsFake,
		Config: CascadedConfig{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	// Voice, then silence longer than SilenceTurnMs.
	_ = p.SendAudio(sineChunk(200, 10000))
	time.Sleep(60 * time.Millisecond) // exceed silence threshold
	// One more chunk to trigger maybeTriggerLocked.
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

func TestCascaded_AudioEndForcesImmediateTurn(t *testing.T) {
	sttFake := &fakeSTT{text: "explicit turn"}
	agent := &fakeAgent{response: "ok"}
	p := newTestProvider(t, CascadedDeps{
		STT: sttFake, Agent: agent,
		// No TTS â†’ pure transcript mode.
		Config: CascadedConfig{SilenceTurnMs: 10_000 /* irrelevantly large */, MinTurnMs: 50},
	})

	_ = p.SendAudio(sineChunk(200, 12000))
	_ = p.SendAudioStreamEnd()

	// Expect STT to fire even though silence never tripped.
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

func TestCascaded_EmptySTTResultIsDropped(t *testing.T) {
	sttFake := &fakeSTT{text: ""} // silence in, nothing out
	agent := &fakeAgent{response: "ignored"}
	p := newTestProvider(t, CascadedDeps{
		STT: sttFake, Agent: agent,
		Config: CascadedConfig{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	_ = p.SendAudio(sineChunk(200, 8000))
	_ = p.SendAudioStreamEnd()

	time.Sleep(200 * time.Millisecond)
	if agent.calls != 0 {
		t.Fatalf("agent must NOT be called when STT returns empty text")
	}
}

func TestCascaded_AgentErrorSurfacesAsMessage(t *testing.T) {
	sttFake := &fakeSTT{text: "hello"}
	agent := &fakeAgent{err: errors.New("llm down")}
	p := newTestProvider(t, CascadedDeps{
		STT: sttFake, Agent: agent,
		Config: CascadedConfig{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	_ = p.SendAudioStreamEnd() // empty buffer, no-op
	_ = p.SendAudio(sineChunk(150, 10000))
	_ = p.SendAudioStreamEnd()

	msgs := collectMessages(t, p, 1*time.Second)
	var sawError bool
	for _, m := range msgs {
		if m.OutputTranscriptDone &&
			contains(m.OutputTranscript, "turn_failed") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected turn_failed error in output; got %+v", msgs)
	}
}

func TestCascaded_HistoryIsFedToAgent(t *testing.T) {
	sttFake := &fakeSTT{}
	agent := &fakeAgent{response: "fine"}
	p := newTestProvider(t, CascadedDeps{
		STT: sttFake, Agent: agent,
		Config: CascadedConfig{HistoryTurns: 3, SilenceTurnMs: 50, MinTurnMs: 50},
	})

	// First turn
	_ = p.SendText("hello")
	// Drain the resulting messages so the processor queue clears.
	_ = collectMessages(t, p, 500*time.Millisecond)

	// Second turn â€” agent input should carry prior turn in LastTranscription.
	_ = p.SendText("how are you")
	_ = collectMessages(t, p, 500*time.Millisecond)

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.calls < 2 {
		t.Fatalf("agent should have been called twice; got %d", agent.calls)
	}
	if !contains(agent.lastIn.LastTranscription, "User: hello") {
		t.Fatalf("history not propagated to agent; got LastTranscription=%q", agent.lastIn.LastTranscription)
	}
	if !contains(agent.lastIn.LastTranscription, "Assistant: fine") {
		t.Fatalf("history missing assistant reply; got %q", agent.lastIn.LastTranscription)
	}
}

func TestCascaded_UpdateInstructionsAffectsFutureAgentTurns(t *testing.T) {
	sttFake := &fakeSTT{}
	agent := &fakeAgent{response: "roger"}
	p := NewCascadedProvider(CascadedDeps{STT: sttFake, Agent: agent})
	if err := p.Connect(context.Background(), LiveConfigFrame{Locale: "en", SystemPrompt: "Original role."}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	if err := p.UpdateInstructions(context.Background(), LiveConfigFrame{
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
	if !contains(agent.lastIn.SystemPrompt, "Drive a decision.") {
		t.Fatalf("SystemPrompt not passed to agent: %+v", agent.lastIn)
	}
}

func TestCascaded_TTSAbsentStillReturnsTranscript(t *testing.T) {
	sttFake := &fakeSTT{text: "hi"}
	agent := &fakeAgent{response: "hello"}
	p := newTestProvider(t, CascadedDeps{
		STT: sttFake, Agent: agent,
		// No TTS â†’ transcript-only
		Config: CascadedConfig{SilenceTurnMs: 50, MinTurnMs: 50},
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

func TestCascaded_ChunkRMS(t *testing.T) {
	silent := silenceChunk(100)
	if rms := chunkRMS(silent); rms != 0 {
		t.Fatalf("silent buffer RMS = %v, want 0", rms)
	}
	loud := sineChunk(100, 30000)
	if rms := chunkRMS(loud); rms < 0.4 {
		t.Fatalf("loud sine RMS = %v, want â‰¥0.4", rms)
	}
}

func TestCascaded_ChunkAudio(t *testing.T) {
	out := chunkAudio([]byte("1234567890"), 3)
	if len(out) != 4 {
		t.Fatalf("chunkAudio split into %d chunks, want 4", len(out))
	}
	if string(out[3]) != "0" {
		t.Fatalf("last chunk = %q, want '0'", out[3])
	}
}

// â”€â”€ utils â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

func contains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	// Tiny helper so tests don't need to import strings in every case.
	// strings.Contains would be fine too; indexOf lets us assert position
	// if we ever need it.
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return i
		}
	}
	return -1
}
