package cascaded

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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
