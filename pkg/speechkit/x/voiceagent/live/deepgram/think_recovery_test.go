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
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
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
// re-asked on the same model, then on the catalog fallback, and only the
// exhausted chain reports a recoverable turn failure with a public code.
func TestDeepgramThinkErrorRetriesThenFallsBackInsideTheTurn(t *testing.T) {
	p, frames := thinkRecoveryPeer(t)
	chain := catalog.VoiceAgentThinkChain(deepgramVoiceAgentProfileID)
	thinkError := map[string]any{"type": "Error", "code": "THINK_FAILED", "description": "upstream 503"}

	mustParse(t, p, map[string]any{"type": "SettingsApplied"})
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "wie spät ist es"})

	// 1. Retry on the default model: the turn is injected again, nothing else.
	if _, swallow := mustParse(t, p, thinkError); !swallow {
		t.Fatal("first think error must be recovered, not surfaced")
	}
	if frame := nextFrame(t, frames); frame["type"] != "InjectUserMessage" || frame["content"] != "wie spät ist es" {
		t.Fatalf("retry frame = %#v", frame)
	}
	// The re-asked turn echoes as user text: swallowed, and the budget stays.
	if _, swallow := mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "wie spät ist es"}); !swallow {
		t.Fatal("the echo of a re-asked turn must not reach the client again")
	}

	// 2. Fallback: UpdateThink to the next catalog model, then the turn again.
	for i := 1; i < len(chain); i++ {
		if _, swallow := mustParse(t, p, thinkError); !swallow {
			t.Fatalf("think error %d must move to the fallback", i+1)
		}
		update := nextFrame(t, frames)
		think, _ := update["think"].(map[string]any)
		provider, _ := think["provider"].(map[string]any)
		if update["type"] != "UpdateThink" || provider["type"] != chain[i].Provider || provider["model"] != chain[i].Model {
			t.Fatalf("fallback frame = %#v, want %v", update, chain[i])
		}
		if think["prompt"] != "be brief" {
			t.Fatalf("fallback lost the session prompt: %#v", think)
		}
		if frame := nextFrame(t, frames); frame["type"] != "InjectUserMessage" {
			t.Fatalf("fallback re-ask frame = %#v", frame)
		}
		mustParse(t, p, map[string]any{"type": "ThinkUpdated"})
		mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "wie spät ist es"})
	}

	// 3. Exhausted chain: a recoverable turn failure, never a Go error.
	msg, swallow := mustParse(t, p, thinkError)
	if swallow || msg == nil || msg.TurnError == nil {
		t.Fatalf("exhausted chain must report a turn failure, got %#v swallow=%v", msg, swallow)
	}
	if coded, ok := msg.TurnError.(interface{ Code() string }); !ok || coded.Code() != "provider_failed" {
		t.Fatalf("turn failure code = %#v", msg.TurnError)
	}

	// The next turn is answered by the fallback model: Settings carry it now.
	settings := p.buildSettings(live.LiveConfig{})
	agent, _ := settings["agent"].(map[string]any)
	think, _ := agent["think"].(map[string]any)
	provider, _ := think["provider"].(map[string]any)
	if last := chain[len(chain)-1]; provider["model"] != last.Model {
		t.Fatalf("session must stay on the fallback, think = %#v", think)
	}
}

// A bring-your-own endpoint is retried but never moved to a managed model.
func TestDeepgramThinkErrorNeverLeavesAnOwnEndpoint(t *testing.T) {
	p, frames := thinkRecoveryPeer(t)
	p.ConfigureThink("open_ai", "agent-x", "https://agent.example/llm/chat/completions", "")
	thinkError := map[string]any{"type": "Error", "code": "THINK_FAILED", "description": "502"}

	mustParse(t, p, map[string]any{"type": "SettingsApplied"})
	mustParse(t, p, map[string]any{"type": "ConversationText", "role": "user", "content": "hello"})
	if _, swallow := mustParse(t, p, thinkError); !swallow {
		t.Fatal("first error must be retried on the endpoint")
	}
	if frame := nextFrame(t, frames); frame["type"] != "InjectUserMessage" {
		t.Fatalf("retry frame = %#v", frame)
	}
	msg, swallow := mustParse(t, p, thinkError)
	if swallow || msg == nil || msg.TurnError == nil {
		t.Fatalf("second error must fail the turn, got %#v", msg)
	}
	select {
	case frame := <-frames:
		t.Fatalf("no UpdateThink may leave the endpoint, got %#v", frame)
	case <-time.After(100 * time.Millisecond):
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

// deepgram_think_endpoint_url set: the endpoint is the whole chain even
// without an explicit model or provider.
func TestDeepgramOwnEndpointIsTheWholeChain(t *testing.T) {
	p := &Provider{}
	p.ConfigureThink("", "", "https://agent.example/llm/chat/completions", "")
	if got := p.thinkCandidates(); len(got) != 1 {
		t.Fatalf("endpoint chain = %#v, want exactly one candidate", got)
	}
}
