package codex

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

// newAppServerBridge builds fakecodex, points auth at a signed-in fake home,
// and returns a bridge pinned to app-server mode.
func newAppServerBridge(t *testing.T, mode string) *Bridge {
	t.Helper()
	binary := buildFakeCodex(t)
	home := t.TempDir()
	writeAuthFile(t, home, `{"OPENAI_API_KEY":null,"tokens":{"id_token":"`+fakeIDToken(t, "pro")+`"}}`)
	bridge := New(Config{BinaryPath: binary, CodexHome: home, Mode: mode})
	t.Cleanup(func() { bridge.Close() })
	return bridge
}

// collect drains events until the given type arrives (inclusive) or the
// deadline hits.
func collect(t *testing.T, bridge *Bridge, until agentbridge.EventType, timeout time.Duration) []agentbridge.Event {
	t.Helper()
	var got []agentbridge.Event
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-bridge.Events():
			got = append(got, ev)
			if ev.Type == until {
				return got
			}
		case <-deadline:
			t.Fatalf("no %s within %s; events so far: %+v", until, timeout, got)
		}
	}
}

func startTestTurn(t *testing.T, bridge *Bridge, prompt string) agentbridge.ThreadRef {
	t.Helper()
	ref, err := bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir(), Sandbox: agentbridge.SandboxReadOnly},
		Prompt:  prompt,
	})
	if err != nil {
		t.Fatalf("start turn: %v", err)
	}
	return ref
}

func TestAppServerTurnLifecycle(t *testing.T) {
	bridge := newAppServerBridge(t, "app_server")

	ref := startTestTurn(t, bridge, "hello")
	if ref.ThreadID != "th_as_1" || ref.TurnID != "turn_1" {
		t.Fatalf("ref = %+v, want th_as_1/turn_1", ref)
	}

	events := collect(t, bridge, agentbridge.EventTurnCompleted, 10*time.Second)
	var kinds []agentbridge.EventType
	items := 0
	for _, ev := range events {
		kinds = append(kinds, ev.Type)
		if ev.Item != nil {
			items++
		}
	}
	if kinds[0] != agentbridge.EventThreadStarted {
		t.Fatalf("first event = %s, want thread_started (%v)", kinds[0], kinds)
	}
	if items < 2 {
		t.Fatalf("expected at least reasoning start+complete items, got %d (%v)", items, kinds)
	}

	// The session survives the turn: a follow-up turn reuses the thread.
	ref2 := startTestTurn(t, bridge, "again")
	if ref2.TurnID != "turn_2" {
		t.Fatalf("second turn id = %q, want turn_2", ref2.TurnID)
	}
	collect(t, bridge, agentbridge.EventTurnCompleted, 10*time.Second)
}

func TestAppServerApprovalApproveAndDeny(t *testing.T) {
	t.Setenv("FAKECODEX_APPROVAL", "1")
	bridge := newAppServerBridge(t, "app_server")

	// Round 1: approve.
	startTestTurn(t, bridge, "install deps")
	events := collect(t, bridge, agentbridge.EventApprovalRequested, 10*time.Second)
	appr := events[len(events)-1].Approval
	if appr == nil || appr.Kind != "command" || appr.Command != "npm install" {
		t.Fatalf("approval = %+v, want command npm install", appr)
	}
	if err := bridge.RespondApproval(context.Background(), appr.ID, agentbridge.DecisionApprove); err != nil {
		t.Fatalf("respond approval: %v", err)
	}
	events = collect(t, bridge, agentbridge.EventTurnCompleted, 10*time.Second)
	if !hasItemSummary(events, "npm install (exit 0)") {
		t.Fatalf("approved command result missing; events: %+v", events)
	}

	// Round 2: deny.
	startTestTurn(t, bridge, "install more")
	events = collect(t, bridge, agentbridge.EventApprovalRequested, 10*time.Second)
	appr = events[len(events)-1].Approval
	if err := bridge.RespondApproval(context.Background(), appr.ID, agentbridge.DecisionDeny); err != nil {
		t.Fatalf("respond deny: %v", err)
	}
	events = collect(t, bridge, agentbridge.EventTurnCompleted, 10*time.Second)
	if !hasItemSummary(events, "command declined; skipping install") {
		t.Fatalf("denied path missing; events: %+v", events)
	}

	// Answering twice fails closed.
	if err := bridge.RespondApproval(context.Background(), appr.ID, agentbridge.DecisionApprove); err == nil {
		t.Fatal("second answer to the same approval must fail")
	}
}

func TestAppServerSteer(t *testing.T) {
	bridge := newAppServerBridge(t, "app_server")
	ref := startTestTurn(t, bridge, "long task")
	collect(t, bridge, agentbridge.EventTurnCompleted, 10*time.Second)

	if err := bridge.Steer(context.Background(), ref, "focus on tests"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	events := collect(t, bridge, agentbridge.EventItemCompleted, 10*time.Second)
	if !hasItemSummary(events, "steered: focus on tests") {
		t.Fatalf("steer item missing; events: %+v", events)
	}
}

func TestAppServerInterrupt(t *testing.T) {
	t.Setenv("FAKECODEX_AS_DELAY_MS", "300")
	bridge := newAppServerBridge(t, "app_server")
	ref := startTestTurn(t, bridge, "slow task")
	if err := bridge.Interrupt(context.Background(), ref); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	events := collect(t, bridge, agentbridge.EventTurnCompleted, 10*time.Second)
	_ = events // aborted turns still close with turn/completed (status aborted)
}

func TestAppServerRestartBudgetDegradesToExec(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("testdata", "simple-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKECODEX_SCRIPT", script)
	bridge := newAppServerBridge(t, "auto")

	// Burn the spawn budget: every round kills the session afterwards.
	for i := 0; i < maxSpawnAttempts; i++ {
		startTestTurn(t, bridge, "turn")
		collect(t, bridge, agentbridge.EventTurnCompleted, 10*time.Second)
		bridge.mu.Lock()
		session := bridge.session
		bridge.mu.Unlock()
		if session == nil {
			t.Fatalf("round %d: no live session", i)
		}
		session.Close()
		select {
		case <-session.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("session did not die")
		}
	}

	// Budget exhausted: the next turn must degrade to exec (bridge_state
	// event + golden exec thread id) instead of spawning a fourth server.
	startTestTurn(t, bridge, "after budget")
	events := collect(t, bridge, agentbridge.EventTurnCompleted, 15*time.Second)
	sawState := false
	sawExecThread := false
	for _, ev := range events {
		if ev.Type == agentbridge.EventBridgeState {
			sawState = true
		}
		if ev.ThreadID == "th_fake123" {
			sawExecThread = true
		}
	}
	if !sawState {
		t.Fatalf("expected bridge_state degradation event; events: %+v", events)
	}
	if !sawExecThread {
		t.Fatalf("expected exec-mode golden thread id; events: %+v", events)
	}
	if got := bridge.effectiveMode(); got != agentbridge.ModeExec {
		t.Fatalf("effective mode after degradation = %s, want exec", got)
	}
}

func TestAppServerForcedModeFailsWithoutFallback(t *testing.T) {
	t.Setenv("FAKECODEX_AS_FAIL", "1")
	bridge := newAppServerBridge(t, "app_server")
	_, err := bridge.StartTurn(context.Background(), agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: "tmp", Path: t.TempDir()},
		Prompt:  "x",
	})
	if err == nil {
		t.Fatal("forced app_server mode must not silently fall back to exec")
	}
}

func hasItemSummary(events []agentbridge.Event, summary string) bool {
	for _, ev := range events {
		if ev.Item != nil && ev.Item.Summary == summary {
			return true
		}
	}
	return false
}
