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

func (p *testLiveProvider) SendAudio([]byte) error                { return nil }
func (p *testLiveProvider) SendAudioStreamEnd() error             { return nil }
func (p *testLiveProvider) SendText(string) error                 { return nil }
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
