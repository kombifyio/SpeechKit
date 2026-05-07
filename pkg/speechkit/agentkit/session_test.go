package agentkit

import (
	"context"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

type testLiveProvider struct {
	cfg       LiveConfig
	messages  chan *voiceagent.LiveMessage
	responses chan ToolResponse
	audio     [][]byte
	text      []string
	ended     int
}

func newTestLiveProvider() *testLiveProvider {
	return &testLiveProvider{
		messages:  make(chan *voiceagent.LiveMessage, 4),
		responses: make(chan ToolResponse, 4),
	}
}

func (p *testLiveProvider) Connect(_ context.Context, cfg LiveConfig) error {
	p.cfg = cfg
	return nil
}

func (p *testLiveProvider) SendAudio(chunk []byte) error {
	p.audio = append(p.audio, append([]byte(nil), chunk...))
	return nil
}
func (p *testLiveProvider) SendAudioStreamEnd() error             { p.ended++; return nil }
func (p *testLiveProvider) SendText(text string) error            { p.text = append(p.text, text); return nil }
func (p *testLiveProvider) Close() error                          { return nil }
func (p *testLiveProvider) Name() string                          { return "test" }
func (p *testLiveProvider) SendToolResponse(r ToolResponse) error { p.responses <- r; return nil }

func (p *testLiveProvider) Receive(ctx context.Context) (*voiceagent.LiveMessage, error) {
	select {
	case msg := <-p.messages:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestAgentSessionAccessorsForwardingAndTranscriptHooks(t *testing.T) {
	provider := newTestLiveProvider()
	registry := NewRegistry()
	memory := NewInMemory()
	userInput := make(chan string, 1)
	agentOutput := make(chan string, 1)

	session := NewAgentSession(provider, Callbacks{}, registry, LifecycleHooks{
		OnUserMessage:  func(_ context.Context, _ SessionContext, text string) { userInput <- text },
		OnAgentMessage: func(_ context.Context, _ SessionContext, text string) { agentOutput <- text },
	}, memory)

	if session.SessionID() == "" {
		t.Fatal("SessionID is empty")
	}
	if session.Registry() != registry {
		t.Fatal("Registry accessor did not return supplied registry")
	}
	if session.Memory() != memory {
		t.Fatal("Memory accessor did not return supplied memory")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{}, voiceagent.DefaultIdleConfig()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer session.Stop()

	if err := session.SendAudio([]byte{1, 2, 3}); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	if err := session.SendText("hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if err := session.EndAudioStream(); err != nil {
		t.Fatalf("EndAudioStream: %v", err)
	}
	if len(provider.audio) != 1 || string(provider.audio[0]) != string([]byte{1, 2, 3}) {
		t.Fatalf("forwarded audio = %#v", provider.audio)
	}
	if len(provider.text) != 1 || provider.text[0] != "hello" {
		t.Fatalf("forwarded text = %#v", provider.text)
	}
	if provider.ended != 1 {
		t.Fatalf("audio stream end count = %d, want 1", provider.ended)
	}

	provider.messages <- &voiceagent.LiveMessage{InputTranscript: "hello ", InputTranscriptDone: false}
	provider.messages <- &voiceagent.LiveMessage{InputTranscript: "world", InputTranscriptDone: true}
	provider.messages <- &voiceagent.LiveMessage{OutputTranscript: "agent ", OutputTranscriptDone: false}
	provider.messages <- &voiceagent.LiveMessage{OutputTranscript: "reply", OutputTranscriptDone: true}

	select {
	case got := <-userInput:
		if got != "hello world" {
			t.Fatalf("user transcript = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("OnUserMessage did not fire")
	}
	select {
	case got := <-agentOutput:
		if got != "agent reply" {
			t.Fatalf("agent transcript = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("OnAgentMessage did not fire")
	}
}

func TestAgentSessionInjectsToolsAndDispatchesToolCalls(t *testing.T) {
	provider := newTestLiveProvider()
	registry := NewRegistry()
	if err := registry.Register(&FuncTool{
		ToolName:        "lookup_note",
		ToolDescription: "Read a note",
		ToolSchema:      Schema{"type": "object"},
		Fn: func(_ context.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"note": args["id"]}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	started := make(chan SessionContext, 1)
	toolCalls := make(chan ToolCall, 1)
	session := NewAgentSession(provider, Callbacks{}, registry, LifecycleHooks{
		OnSessionStart: func(_ context.Context, sc SessionContext) { started <- sc },
		OnToolCall:     func(_ context.Context, _ SessionContext, call ToolCall) { toolCalls <- call },
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{}, voiceagent.DefaultIdleConfig()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Stop()

	select {
	case sc := <-started:
		if sc.SessionID == "" || sc.Registry != registry {
			t.Fatalf("session context = %#v", sc)
		}
	case <-time.After(time.Second):
		t.Fatal("OnSessionStart did not fire")
	}

	if len(provider.cfg.Tools) != 1 || provider.cfg.Tools[0].Name != "lookup_note" {
		t.Fatalf("tools injected into config = %#v", provider.cfg.Tools)
	}

	provider.messages <- &voiceagent.LiveMessage{ToolCalls: []ToolCall{{
		ID:   "call-1",
		Name: "lookup_note",
		Args: map[string]any{"id": "n1"},
	}}}

	select {
	case call := <-toolCalls:
		if call.Name != "lookup_note" {
			t.Fatalf("tool call = %#v", call)
		}
	case <-time.After(time.Second):
		t.Fatal("OnToolCall did not fire")
	}

	select {
	case response := <-provider.responses:
		if response.ID != "call-1" || response.Name != "lookup_note" || response.Response["note"] != "n1" {
			t.Fatalf("tool response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("tool response was not sent")
	}
}
