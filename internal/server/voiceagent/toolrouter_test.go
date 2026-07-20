//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeToolRouter is a controllable SessionToolRouter for adapter tests.
type fakeToolRouter struct {
	mu       sync.Mutex
	defs     []ToolDefinitionFrame
	defsSeen []LiveConfigFrame
	executed []ToolCall
	result   map[string]any
	handled  bool
	delay    time.Duration
}

func (r *fakeToolRouter) Definitions(_ context.Context, _ *ManagedSession, cfg LiveConfigFrame) []ToolDefinitionFrame {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defsSeen = append(r.defsSeen, cfg)
	return r.defs
}

func (r *fakeToolRouter) Execute(ctx context.Context, _ *ManagedSession, call ToolCall) (map[string]any, bool) {
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return nil, false
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executed = append(r.executed, call)
	return r.result, r.handled
}

func (r *fakeToolRouter) executedCalls() []ToolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ToolCall(nil), r.executed...)
}

// startAdapterEnvWithToolRouter mirrors startAdapterEnv but injects a
// SessionToolRouter, exercising the server-side tool bridge dispatch path.
func startAdapterEnvWithToolRouter(t *testing.T, provider *fakeProvider, resolver *fakeResolver, router SessionToolRouter) *adapterTestEnv {
	t.Helper()
	env := &adapterTestEnv{
		provider: provider,
		resolver: resolver,
		done:     make(chan struct{}),
		idle:     time.Minute,
	}
	env.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("server accept: %v", err)
			return
		}
		conn.SetReadLimit(1 << 20)
		adapter := &Adapter{
			Session:     &ManagedSession{ID: "tool-session", Owner: Identity{UserID: "u1"}, BridgeCredential: "secret-credential"},
			Conn:        conn,
			Provider:    provider,
			Persona:     resolver,
			IdleTimeout: time.Minute,
			ToolRouter:  router,
			OnClose:     func() { close(env.done) },
		}
		adapter.Run(r.Context())
	}))
	t.Cleanup(func() { env.srv.Close() })

	wsURL := "ws" + strings.TrimPrefix(env.srv.URL, "http")
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	env.conn = conn
	t.Cleanup(func() { _ = conn.CloseNow() })
	return env
}

// readFrames reads text frames until match returns true or the timeout
// elapses; returns the matched frame.
func readFrameUntil(t *testing.T, conn *websocket.Conn, timeout time.Duration, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("timed out waiting for frame")
		}
		ctx, cancel := context.WithTimeout(context.Background(), remaining)
		typ, data, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if match(frame) {
			return frame
		}
	}
}

func TestAdapter_ToolRouterDefinitionsMergedIntoConnectConfig(t *testing.T) {
	provider := newFakeProvider()
	router := &fakeToolRouter{
		defs: []ToolDefinitionFrame{
			{Name: "kombify_knowledge_search", Behavior: "blocking", TimeoutMs: 10000},
			{Name: "kombify_docs_lookup", Behavior: "blocking"},
		},
	}
	env := startAdapterEnvWithToolRouter(t, provider, &fakeResolver{}, router)
	sendStart(t, env.conn, StartFrame{})

	readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgState
	})
	provider.mu.Lock()
	cfg := provider.connectCfg
	provider.mu.Unlock()
	if cfg == nil {
		t.Fatal("provider never connected")
	}
	if len(cfg.Tools) != 2 {
		t.Fatalf("Connect cfg.Tools = %d entries, want 2", len(cfg.Tools))
	}
	if cfg.Tools[0].Name != "kombify_knowledge_search" || cfg.Tools[0].Behavior != "blocking" {
		t.Fatalf("unexpected first tool: %+v", cfg.Tools[0])
	}
}

func TestAdapter_ClaimedToolCallExecutedServerSide(t *testing.T) {
	provider := newFakeProvider()
	router := &fakeToolRouter{
		defs:    []ToolDefinitionFrame{{Name: "kombify_knowledge_search", Behavior: "blocking"}},
		result:  map[string]any{"result": "TechStack bundles reproducible homelab stacks."},
		handled: true,
	}
	env := startAdapterEnvWithToolRouter(t, provider, &fakeResolver{}, router)
	sendStart(t, env.conn, StartFrame{})
	readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgState
	})

	provider.push(&LiveMessage{ToolCalls: []ToolCall{{
		ID:   "call-1",
		Name: "kombify_knowledge_search",
		Args: map[string]any{"query": "what is techstack"},
	}}})

	// 1) Client still sees the tool_call frame, tagged execution=server.
	toolCall := readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgToolCall
	})
	metadata, _ := toolCall["provider_metadata"].(map[string]any)
	if metadata["execution"] != "server" {
		t.Fatalf("tool_call provider_metadata.execution = %v, want server", metadata["execution"])
	}

	// 2) Client receives the tool_result_ack event with id/status.
	ack := readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgEvent && frame["event_type"] == EventToolResultAck
	})
	ackMeta, _ := ack["provider_metadata"].(map[string]any)
	if ackMeta["id"] != "call-1" || ackMeta["status"] != "ok" {
		t.Fatalf("tool_result_ack metadata = %#v, want id=call-1 status=ok", ackMeta)
	}

	// 3) The provider received the router's result via SendToolResponse —
	//    no client tool_response was needed.
	deadline := time.Now().Add(2 * time.Second)
	for {
		provider.mu.Lock()
		responses := append([]ToolResponseFrame(nil), provider.toolResponses...)
		provider.mu.Unlock()
		if len(responses) == 1 {
			if responses[0].ID != "call-1" {
				t.Fatalf("tool response ID = %q, want call-1", responses[0].ID)
			}
			if responses[0].Response["result"] != "TechStack bundles reproducible homelab stacks." {
				t.Fatalf("tool response payload = %#v", responses[0].Response)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider tool responses = %d, want 1", len(responses))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls := router.executedCalls(); len(calls) != 1 || calls[0].Name != "kombify_knowledge_search" {
		t.Fatalf("router executed = %#v, want one kombify_knowledge_search call", calls)
	}
}

func TestAdapter_UnclaimedToolCallKeepsClientPassThrough(t *testing.T) {
	provider := newFakeProvider()
	router := &fakeToolRouter{
		defs:    []ToolDefinitionFrame{{Name: "kombify_knowledge_search", Behavior: "blocking"}},
		result:  map[string]any{"result": "x"},
		handled: true,
	}
	env := startAdapterEnvWithToolRouter(t, provider, &fakeResolver{}, router)
	sendStart(t, env.conn, StartFrame{})
	readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgState
	})

	provider.push(&LiveMessage{ToolCalls: []ToolCall{{ID: "call-2", Name: "client_side_tool"}}})

	toolCall := readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgToolCall
	})
	if metadata, ok := toolCall["provider_metadata"].(map[string]any); ok {
		if metadata["execution"] == "server" {
			t.Fatal("unclaimed tool must not be tagged execution=server")
		}
	}
	// Router must not have executed anything; the wire contract for
	// unclaimed tools is unchanged (client answers with tool_response).
	if calls := router.executedCalls(); len(calls) != 0 {
		t.Fatalf("router executed %#v for an unclaimed tool", calls)
	}
	provider.mu.Lock()
	responses := len(provider.toolResponses)
	provider.mu.Unlock()
	if responses != 0 {
		t.Fatalf("provider received %d tool responses, want 0 (client pass-through)", responses)
	}
}

func TestAdapter_ToolRouterFailureDegradesToToolLess(t *testing.T) {
	provider := newFakeProvider()
	router := &fakeToolRouter{defs: nil} // no definitions: bridge down or no credential
	env := startAdapterEnvWithToolRouter(t, provider, &fakeResolver{}, router)
	sendStart(t, env.conn, StartFrame{})

	// The informational event frame is emitted before session_ready.
	event := readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgEvent && frame["event_type"] == EventToolBridgeUnavailable
	})
	if event == nil {
		t.Fatal("expected tool_bridge_unavailable event frame")
	}
	readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgState
	})
	provider.mu.Lock()
	cfg := provider.connectCfg
	provider.mu.Unlock()
	if cfg == nil {
		t.Fatal("provider never connected")
	}
	if len(cfg.Tools) != 0 {
		t.Fatalf("cfg.Tools = %d entries, want 0 (tool-less)", len(cfg.Tools))
	}
}

func TestAdapter_ToolRouterUnhandledExecuteYieldsErrorResponse(t *testing.T) {
	provider := newFakeProvider()
	router := &fakeToolRouter{
		defs:    []ToolDefinitionFrame{{Name: "kombify_knowledge_search", Behavior: "blocking"}},
		handled: false, // execution failure
	}
	env := startAdapterEnvWithToolRouter(t, provider, &fakeResolver{}, router)
	sendStart(t, env.conn, StartFrame{})
	readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgState
	})

	provider.push(&LiveMessage{ToolCalls: []ToolCall{{ID: "call-3", Name: "kombify_knowledge_search"}}})

	ack := readFrameUntil(t, env.conn, 3*time.Second, func(frame map[string]any) bool {
		return frame["type"] == MsgEvent && frame["event_type"] == EventToolResultAck
	})
	ackMeta, _ := ack["provider_metadata"].(map[string]any)
	if ackMeta["status"] != "error" {
		t.Fatalf("ack status = %v, want error", ackMeta["status"])
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		provider.mu.Lock()
		responses := append([]ToolResponseFrame(nil), provider.toolResponses...)
		provider.mu.Unlock()
		if len(responses) == 1 {
			errObj, ok := responses[0].Response["error"].(map[string]any)
			if !ok || errObj["code"] != "tool_bridge_unavailable" {
				t.Fatalf("error response = %#v, want tool_bridge_unavailable", responses[0].Response)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("provider tool responses = %d, want 1", len(responses))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
