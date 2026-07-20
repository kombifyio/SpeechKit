package agentkit

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

type testLiveProvider struct {
	cfg       LiveConfig
	messages  chan *live.LiveMessage
	responses chan ToolResponse
}

func TestAgentSessionAuthorizesAndNarrowsToolArgsBeforeInvoke(t *testing.T) {
	provider := newTestLiveProvider()
	registry := NewRegistry()
	invoked := make(chan map[string]any, 1)
	if err := registry.Register(&FuncTool{
		ToolName:        "home_assistant",
		ToolDescription: "Test host-authorized tool",
		ToolSchema:      Schema{"type": "object"},
		Fn: func(_ context.Context, args map[string]any) (map[string]any, error) {
			invoked <- args
			return map[string]any{"ok": true}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	observed := make(chan ToolCall, 1)
	session := NewAgentSession(provider, Callbacks{}, registry, LifecycleHooks{
		AuthorizeToolCall: func(_ context.Context, _ SessionContext, call ToolCall) (map[string]any, error) {
			if call.Args["query"] != "model-expanded query" {
				t.Fatalf("authorizer saw args = %#v", call.Args)
			}
			return map[string]any{"query": "final host transcript", "locale": "en-US"}, nil
		},
		OnToolCall: func(_ context.Context, _ SessionContext, call ToolCall) {
			observed <- call
		},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{}, live.DefaultIdleConfig()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Stop()

	provider.messages <- &live.LiveMessage{ToolCalls: []ToolCall{{
		ID:   "authorized-call",
		Name: "home_assistant",
		Args: map[string]any{"query": "model-expanded query", "extra": "drop me"},
	}}}

	select {
	case call := <-observed:
		if call.Args["query"] != "final host transcript" || call.Args["locale"] != "en-US" {
			t.Fatalf("OnToolCall args = %#v", call.Args)
		}
		if _, exists := call.Args["extra"]; exists {
			t.Fatalf("OnToolCall retained unauthorized args: %#v", call.Args)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized OnToolCall did not fire")
	}

	select {
	case args := <-invoked:
		if args["query"] != "final host transcript" || args["locale"] != "en-US" {
			t.Fatalf("tool invoked with args = %#v", args)
		}
		if _, exists := args["extra"]; exists {
			t.Fatalf("tool received unauthorized args: %#v", args)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized tool was not invoked")
	}
}

func TestAgentSessionAuthorizationCompletesBeforeDispatchReturns(t *testing.T) {
	provider := newTestLiveProvider()
	registry := NewRegistry()
	invoked := make(chan struct{}, 1)
	if err := registry.Register(&FuncTool{
		ToolName:        "home_assistant",
		ToolDescription: "Test synchronous event authorization",
		ToolSchema:      Schema{"type": "object"},
		Fn: func(context.Context, map[string]any) (map[string]any, error) {
			invoked <- struct{}{}
			return map[string]any{"ok": true}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	authorizerEntered := make(chan struct{})
	releaseAuthorizer := make(chan struct{})
	session := NewAgentSession(provider, Callbacks{}, registry, LifecycleHooks{
		AuthorizeToolCall: func(_ context.Context, _ SessionContext, call ToolCall) (map[string]any, error) {
			close(authorizerEntered)
			<-releaseAuthorizer
			return call.Args, nil
		},
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{}, live.DefaultIdleConfig()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Stop()

	dispatchReturned := make(chan struct{})
	go func() {
		session.dispatchTool(ToolCall{
			ID:   "arrival-bound-call",
			Name: "home_assistant",
			Args: map[string]any{"query": "turn on the kitchen light"},
		})
		close(dispatchReturned)
	}()

	select {
	case <-authorizerEntered:
	case <-time.After(time.Second):
		t.Fatal("authorizer did not run")
	}
	select {
	case <-dispatchReturned:
		t.Fatal("dispatch returned before synchronous authorization completed")
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case <-invoked:
		t.Fatal("tool invoked while synchronous authorization was blocked")
	default:
	}

	close(releaseAuthorizer)
	select {
	case <-dispatchReturned:
	case <-time.After(time.Second):
		t.Fatal("dispatch did not return after authorization completed")
	}
	select {
	case <-invoked:
	case <-time.After(time.Second):
		t.Fatal("authorized tool was not invoked")
	}
}

func TestAgentSessionAuthorizesAtEventArrivalBeforeRawObserver(t *testing.T) {
	provider := newTestLiveProvider()
	registry := NewRegistry()
	invoked := make(chan struct{}, 1)
	if err := registry.Register(&FuncTool{
		ToolName:        "home_assistant",
		ToolDescription: "Test receive-callback ordering",
		ToolSchema:      Schema{"type": "object"},
		Fn: func(context.Context, map[string]any) (map[string]any, error) {
			invoked <- struct{}{}
			return map[string]any{"ok": true}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	authorized := make(chan struct{})
	rawObserverEntered := make(chan struct{})
	releaseRawObserver := make(chan struct{})
	session := NewAgentSession(provider, Callbacks{
		OnToolCall: func(ToolCall) {
			close(rawObserverEntered)
			<-releaseRawObserver
		},
	}, registry, LifecycleHooks{
		AuthorizeToolCall: func(_ context.Context, _ SessionContext, call ToolCall) (map[string]any, error) {
			close(authorized)
			return call.Args, nil
		},
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{}, live.DefaultIdleConfig()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Stop()
	defer func() {
		select {
		case <-releaseRawObserver:
		default:
			close(releaseRawObserver)
		}
	}()

	provider.messages <- &live.LiveMessage{ToolCalls: []ToolCall{{
		ID:   "event-arrival-call",
		Name: "home_assistant",
		Args: map[string]any{"query": "turn on the kitchen light"},
	}}}
	select {
	case <-rawObserverEntered:
	case <-time.After(time.Second):
		t.Fatal("raw observer did not receive the tool event")
	}
	select {
	case <-authorized:
	default:
		t.Fatal("raw observer ran before event-arrival authorization")
	}
	select {
	case <-invoked:
		t.Fatal("tool invoked before the raw event observer returned")
	default:
	}

	close(releaseRawObserver)
	select {
	case <-invoked:
	case <-time.After(time.Second):
		t.Fatal("tool was not invoked after the raw observer returned")
	}
}

func TestAgentSessionDeniedToolCallNeverInvokesTool(t *testing.T) {
	provider := newTestLiveProvider()
	registry := NewRegistry()
	var invokes atomic.Int32
	if err := registry.Register(&FuncTool{
		ToolName:        "home_assistant",
		ToolDescription: "Test host-authorized tool",
		ToolSchema:      Schema{"type": "object"},
		Fn: func(context.Context, map[string]any) (map[string]any, error) {
			invokes.Add(1)
			return map[string]any{"ok": true}, nil
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	var observedCalls atomic.Int32
	var observedResults atomic.Int32
	session := NewAgentSession(provider, Callbacks{}, registry, LifecycleHooks{
		AuthorizeToolCall: func(context.Context, SessionContext, ToolCall) (map[string]any, error) {
			return nil, errors.New("test policy denial")
		},
		OnToolCall: func(context.Context, SessionContext, ToolCall) {
			observedCalls.Add(1)
		},
		OnToolResult: func(context.Context, SessionContext, ToolCall, ToolResponse) {
			observedResults.Add(1)
		},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{}, live.DefaultIdleConfig()); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer session.Stop()

	provider.messages <- &live.LiveMessage{ToolCalls: []ToolCall{{
		ID:   "denied-call",
		Name: "home_assistant",
		Args: map[string]any{"query": "turn off every light"},
	}}}

	select {
	case response := <-provider.responses:
		if response.ID != "denied-call" || response.Name != "home_assistant" ||
			response.Response["error"] != "tool call denied by host policy" {
			t.Fatalf("denied response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("denied tool response was not sent")
	}
	if invokes.Load() != 0 || observedCalls.Load() != 0 || observedResults.Load() != 0 {
		t.Fatalf("denied call invoked=%d observed_calls=%d observed_results=%d",
			invokes.Load(), observedCalls.Load(), observedResults.Load())
	}
}

func newTestLiveProvider() *testLiveProvider {
	return &testLiveProvider{
		messages:  make(chan *live.LiveMessage, 4),
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

func (p *testLiveProvider) Receive(ctx context.Context) (*live.LiveMessage, error) {
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
	toolResults := make(chan ToolResponse, 1)
	session := NewAgentSession(provider, Callbacks{}, registry, LifecycleHooks{
		OnSessionStart: func(_ context.Context, sc SessionContext) { started <- sc },
		OnToolCall:     func(_ context.Context, _ SessionContext, call ToolCall) { toolCalls <- call },
		OnToolResult: func(_ context.Context, _ SessionContext, call ToolCall, response ToolResponse) {
			if call.ID != response.ID {
				t.Errorf("tool result call ID = %q, response ID = %q", call.ID, response.ID)
			}
			toolResults <- response
		},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{}, live.DefaultIdleConfig()); err != nil {
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

	provider.messages <- &live.LiveMessage{ToolCalls: []ToolCall{{
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
	case response := <-toolResults:
		if response.ID != "call-1" || response.Name != "lookup_note" || response.Response["note"] != "n1" {
			t.Fatalf("OnToolResult response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("OnToolResult did not fire")
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
