package cascaded

import (
	"errors"
	"strings"
	"testing"
	"time"
)

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
