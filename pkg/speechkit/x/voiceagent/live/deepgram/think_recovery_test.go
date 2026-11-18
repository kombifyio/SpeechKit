package deepgram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// thinkRecoveryPeer records the control frames the provider writes after a
// think error: the re-asked turn and any UpdateThink.
func thinkRecoveryPeer(t *testing.T) (*Provider, <-chan map[string]any) {
	t.Helper()
	frames := make(chan map[string]any, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow() //nolint:errcheck // test server teardown
		for {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			_, payload, err := conn.Read(ctx)
			cancel()
			if err != nil {
				return
			}
			var frame map[string]any
			if json.Unmarshal(payload, &frame) == nil {
				frames <- frame
			}
		}
	}))
	t.Cleanup(srv.Close)
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	p := &Provider{}
	p.ConfigureThink("open_ai", "agent-x", "", "")
	p.conn = conn
	p.lastConfig = &live.LiveConfig{FrameworkPrompt: "be brief"}
	return p, frames
}

func nextFrame(t *testing.T, frames <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case frame := <-frames:
		return frame
	case <-time.After(2 * time.Second):
		t.Fatal("no control frame written")
		return nil
	}
}

func mustParse(t *testing.T, p *Provider, event map[string]any) (*live.LiveMessage, bool) {
	t.Helper()
	msg, swallow, err := p.parseEvent(mustMarshal(t, event))
	if err != nil {
		t.Fatalf("parse %v: %v", event["type"], err)
	}
	return msg, swallow
}

// A think-provider error inside a turn never ends the session: the turn is
// re-asked once on the same think route, and a second failure reports a
// recoverable turn failure with a public code. No UpdateThink ever moves the
// turn to another model: the route alone decides which model answers.
func TestDeepgramThinkErrorRetriesOnceThenFailsTheTurn(t *testing.T) {
	p, frames := thinkRecoveryPeer(t)
	p.ConfigureThink("open_ai", "mode:default", "https://callback.example/llm/chat/completions", "session-token")
	thinkError := map[string]any{"type": "Error", "code": "THINK_FAILED", "description": "upstream 503"}

	mustParse(t, p, map[string]any{"type": "SettingsApplied"})
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "wie spät ist es"})

	if _, swallow := mustParse(t, p, thinkError); !swallow {
		t.Fatal("first think error must be recovered, not surfaced")
	}
	if frame := nextFrame(t, frames); frame["type"] != "InjectUserMessage" || frame["content"] != "wie spät ist es" {
		t.Fatalf("retry frame = %#v", frame)
	}
	// The re-asked turn echoes as user text: swallowed.
	if _, swallow := mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "wie spät ist es"}); !swallow {
		t.Fatal("the echo of a re-asked turn must not reach the client again")
	}

	msg, swallow := mustParse(t, p, thinkError)
	if swallow || msg == nil || msg.TurnError == nil {
		t.Fatalf("second error must report a turn failure, got %#v swallow=%v", msg, swallow)
	}
	if coded, ok := msg.TurnError.(interface{ Code() string }); !ok || coded.Code() != "provider_failed" {
		t.Fatalf("turn failure code = %#v", msg.TurnError)
	}
	select {
	case frame := <-frames:
		t.Fatalf("no UpdateThink may move the turn to another model, got %#v", frame)
	case <-time.After(100 * time.Millisecond):
	}
}

// A turn the think route denied for good (consent, plan, credit, a revoked session) is never re-asked, and
// Deepgram's own error wording (here an authorization word that would otherwise end the session) cannot end
// the session: it fails the turn with the typed public code.
func TestDeepgramThinkRefusalIsTypedAndNeverRetried(t *testing.T) {
	p, frames := thinkRecoveryPeer(t)
	mustParse(t, p, map[string]any{"type": "SettingsApplied"})
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "hello"})
	p.NoteThinkRefusal("permission_denied")
	msg, swallow := mustParse(t, p, map[string]any{"type": "Error", "code": "THINK_FAILED", "description": "upstream returned 403 forbidden"})
	if swallow || msg == nil || msg.TurnError == nil {
		t.Fatalf("a refused turn must fail with a turn error, got %#v", msg)
	}
	if coded, ok := msg.TurnError.(interface{ Code() string }); !ok || coded.Code() != "permission_denied" {
		t.Fatalf("turn failure code = %#v, want permission_denied", msg.TurnError)
	}
	select {
	case frame := <-frames:
		t.Fatalf("a denied turn must not be re-asked, got %#v", frame)
	case <-time.After(100 * time.Millisecond):
	}
	// The next, transient failure still gets its one re-ask: a refusal does not outlive its turn.
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "again"})
	if _, swallow := mustParse(t, p, map[string]any{"type": "Error", "code": "THINK_FAILED", "description": "upstream 503"}); !swallow {
		t.Fatal("a transient failure after a refused turn must be re-asked")
	}
	if frame := nextFrame(t, frames); frame["type"] != "InjectUserMessage" {
		t.Fatalf("retry frame = %#v", frame)
	}
}

// Once a tool ran in the turn, a think error never re-asks it: the tool may
// have had an effect. The turn fails with a recoverable outage.
func TestDeepgramThinkErrorAfterToolCallFailsTheTurn(t *testing.T) {
	p, frames := thinkRecoveryPeer(t)
	mustParse(t, p, map[string]any{"type": "SettingsApplied"})
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "turn the light on"})
	mustParse(t, p, map[string]any{"type": "FunctionCallRequest", "function_call_id": "c1", "function_name": "light", "input": map[string]any{}})
	msg, swallow := mustParse(t, p, map[string]any{"type": "Error", "code": "THINK_FAILED", "description": "llm timeout"})
	if swallow || msg == nil || msg.TurnError == nil {
		t.Fatalf("think error after a tool must fail the turn, got %#v", msg)
	}
	select {
	case frame := <-frames:
		t.Fatalf("no re-ask after a tool, got %#v", frame)
	case <-time.After(100 * time.Millisecond):
	}
}

// Account and credit failures end the session; other non-think errors fail
// the turn without a re-ask.
func TestDeepgramErrorKinds(t *testing.T) {
	p, frames := thinkRecoveryPeer(t)
	mustParse(t, p, map[string]any{"type": "SettingsApplied"})
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "hello"})
	if _, _, err := p.parseEvent(mustMarshal(t, map[string]any{"type": "Error", "code": "INSUFFICIENT_CREDITS", "description": "balance"})); err == nil {
		t.Fatal("a credit failure must end the session")
	}
	msg, swallow := mustParse(t, p, map[string]any{"type": "Error", "code": "UNPARSABLE_CLIENT_MESSAGE", "description": "bad frame"})
	if swallow || msg == nil || msg.TurnError == nil {
		t.Fatalf("a client-message error fails the turn, got %#v", msg)
	}
	select {
	case frame := <-frames:
		t.Fatalf("no re-ask for a client-message error, got %#v", frame)
	case <-time.After(100 * time.Millisecond):
	}
}

// A refused re-ask fails the turn non-fatally and clears the recovery.
func TestDeepgramInjectionRefusedDuringRecovery(t *testing.T) {
	p, frames := thinkRecoveryPeer(t)
	mustParse(t, p, map[string]any{"type": "SettingsApplied"})
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "hello"})
	mustParse(t, p, map[string]any{"type": "Error", "code": "THINK_FAILED", "description": "llm 503"})
	nextFrame(t, frames)
	msg, swallow := mustParse(t, p, map[string]any{"type": "InjectionRefused"})
	if swallow || msg == nil || msg.TurnError == nil {
		t.Fatalf("a refused re-ask must fail the turn, got %#v", msg)
	}
	if _, swallow := mustParse(t, p, map[string]any{"type": "InjectionRefused"}); !swallow {
		t.Fatal("a refusal outside recovery stays silent")
	}
}

// A provider with neither a think model nor an endpoint refuses to connect:
// the package holds no model of its own.
func TestDeepgramThinkRouteIsExplicit(t *testing.T) {
	p := &Provider{}
	if _, err := p.thinkRoute(); err != ErrThinkNotConfigured {
		t.Fatalf("unconfigured route error = %v, want ErrThinkNotConfigured", err)
	}
	p.ConfigureThink("", "mode:default", "https://callback.example/llm/chat/completions", "")
	route, err := p.thinkRoute()
	if err != nil || route.provider != "open_ai" || route.model != "mode:default" {
		t.Fatalf("explicit route = %#v, %v", route, err)
	}
}
