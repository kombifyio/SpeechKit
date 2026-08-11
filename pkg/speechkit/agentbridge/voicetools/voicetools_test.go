package voicetools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
)

// fakeAgent is an in-memory agentbridge.Agent for coordinator tests.
type fakeAgent struct {
	mu         sync.Mutex
	status     agentbridge.Status
	events     chan agentbridge.Event
	closed     bool
	starts     []agentbridge.TurnRequest
	steers     []string
	interrupts int
	busy       bool
	approvals  map[string]agentbridge.Decision
}

func newFakeAgent(mode agentbridge.Mode) *fakeAgent {
	return &fakeAgent{
		status: agentbridge.Status{
			Installed: true, Auth: agentbridge.AuthChatGPT, Plan: "pro", Mode: mode,
			Detail: map[bool]string{true: "", false: "Codex is installed but not signed in — run 'codex login' once"}[mode != agentbridge.ModeUnavailable],
		},
		events:    make(chan agentbridge.Event, 32),
		approvals: map[string]agentbridge.Decision{},
	}
}

func (f *fakeAgent) Status(context.Context) agentbridge.Status { return f.status }
func (f *fakeAgent) StartTurn(_ context.Context, req agentbridge.TurnRequest) (agentbridge.ThreadRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.busy {
		return agentbridge.ThreadRef{}, agentbridge.ErrBusy
	}
	f.busy = true
	f.starts = append(f.starts, req)
	return agentbridge.ThreadRef{ThreadID: "th_fake", TurnID: "turn_fake"}, nil
}
func (f *fakeAgent) Steer(_ context.Context, _ agentbridge.ThreadRef, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steers = append(f.steers, text)
	return nil
}
func (f *fakeAgent) Interrupt(context.Context, agentbridge.ThreadRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interrupts++
	return nil
}
func (f *fakeAgent) RespondApproval(_ context.Context, id string, d agentbridge.Decision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approvals[id] = d
	return nil
}
func (f *fakeAgent) Events() <-chan agentbridge.Event { return f.events }
func (f *fakeAgent) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.events)
	}
	return nil
}
func (f *fakeAgent) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// recordingNotifier captures narration for assertions.
type recordingNotifier struct {
	mu        sync.Mutex
	narrated  []string
	approvals []agentbridge.ApprovalRequest
}

func (n *recordingNotifier) Narrate(text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.narrated = append(n.narrated, text)
}
func (n *recordingNotifier) AnnounceApproval(a agentbridge.ApprovalRequest) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.approvals = append(n.approvals, a)
}
func (n *recordingNotifier) snapshot() ([]string, []agentbridge.ApprovalRequest) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.narrated...), append([]agentbridge.ApprovalRequest(nil), n.approvals...)
}

func toolByName(t *testing.T, c *Coordinator, name string) agentkit.Tool {
	t.Helper()
	for _, tool := range c.Tools() {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return nil
}

func testPolicy() Policy {
	return Policy{
		Enabled:        true,
		Projects:       []Project{{Alias: "speechkit", Path: "/tmp/sk", Sandbox: agentbridge.SandboxReadOnly}},
		DefaultSandbox: agentbridge.SandboxReadOnly,
	}
}

func TestCallGateBlocksTaskToolsWithoutCall(t *testing.T) {
	c := New(testPolicy(), func() agentbridge.Agent { return newFakeAgent(agentbridge.ModeAppServer) }, &recordingNotifier{}, nil)
	defer c.Close()
	for _, name := range []string{ToolGPTTask, ToolGPTStatus, ToolGPTSteer, ToolGPTStop} {
		_, err := toolByName(t, c, name).Invoke(context.Background(), map[string]any{"project": "speechkit", "prompt": "x", "instruction": "x"})
		if err == nil || !strings.Contains(err.Error(), `say "Call GPT" first`) {
			t.Fatalf("%s without a call: err = %v, want speakable call-gate hint", name, err)
		}
	}
}

func TestCallGPTDisabledPolicyRefuses(t *testing.T) {
	p := testPolicy()
	p.Enabled = false
	spawned := false
	c := New(p, func() agentbridge.Agent { spawned = true; return newFakeAgent(agentbridge.ModeAppServer) }, &recordingNotifier{}, nil)
	defer c.Close()
	_, err := toolByName(t, c, ToolCallGPT).Invoke(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "disabled in Settings") {
		t.Fatalf("disabled policy: err = %v", err)
	}
	if spawned {
		t.Fatal("disabled policy must never spawn an agent process")
	}
}

func TestCallGPTUnavailableAgentReportsAndClosesProcess(t *testing.T) {
	agent := newFakeAgent(agentbridge.ModeUnavailable)
	agent.status.Auth = agentbridge.AuthNone
	c := New(testPolicy(), func() agentbridge.Agent { return agent }, &recordingNotifier{}, nil)
	defer c.Close()
	out, err := toolByName(t, c, ToolCallGPT).Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("unavailable must be a speakable status, not an error: %v", err)
	}
	if out["status"] != "unavailable" || !strings.Contains(out["detail"].(string), "codex login") {
		t.Fatalf("out = %v", out)
	}
	if !agent.isClosed() {
		t.Fatal("probe agent must be closed when the call cannot be placed")
	}
	if c.CallActive() {
		t.Fatal("no call may be active after an unavailable probe")
	}
}

func TestCallTaskLifecycleAndHangUp(t *testing.T) {
	agent := newFakeAgent(agentbridge.ModeAppServer)
	notifier := &recordingNotifier{}
	c := New(testPolicy(), func() agentbridge.Agent { return agent }, notifier, nil)
	defer c.Close()

	out, err := toolByName(t, c, ToolCallGPT).Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out["status"] != "connected" || out["plan"] != "pro" {
		t.Fatalf("call out = %v", out)
	}
	if !c.CallActive() {
		t.Fatal("call must be active")
	}

	// Unknown project fails closed and names the allowlist.
	_, err = toolByName(t, c, ToolGPTTask).Invoke(context.Background(), map[string]any{"project": "ghost", "prompt": "do things"})
	if err == nil || !strings.Contains(err.Error(), "speechkit") {
		t.Fatalf("unknown project err = %v", err)
	}

	out, err = toolByName(t, c, ToolGPTTask).Invoke(context.Background(), map[string]any{"project": "speechkit", "prompt": "fix the bug"})
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if out["status"] != "started" || out["sandbox"] != string(agentbridge.SandboxReadOnly) {
		t.Fatalf("task out = %v", out)
	}

	// Busy agent yields a speakable hint.
	_, err = toolByName(t, c, ToolGPTTask).Invoke(context.Background(), map[string]any{"project": "speechkit", "prompt": "more"})
	if err == nil || !strings.Contains(err.Error(), "already working") {
		t.Fatalf("busy err = %v", err)
	}

	// Turn completion narrates and frees the busy state.
	agent.events <- agentbridge.Event{Type: agentbridge.EventItemCompleted, Item: &agentbridge.Item{Kind: "agent_message", Summary: "patched the parser"}}
	agent.events <- agentbridge.Event{Type: agentbridge.EventTurnCompleted, ThreadID: "th_fake"}
	waitFor(t, func() bool {
		narrated, _ := notifier.snapshot()
		return len(narrated) > 0 && strings.Contains(narrated[len(narrated)-1], "patched the parser")
	})

	st, err := toolByName(t, c, ToolGPTStatus).Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st["busy"] != false || st["phase"] != "idle" {
		t.Fatalf("status out = %v", st)
	}

	// Steer + stop pass through.
	if _, err := toolByName(t, c, ToolGPTSteer).Invoke(context.Background(), map[string]any{"instruction": "add tests"}); err != nil {
		t.Fatalf("steer: %v", err)
	}
	if _, err := toolByName(t, c, ToolGPTStop).Invoke(context.Background(), nil); err != nil {
		t.Fatalf("stop: %v", err)
	}

	out, err = toolByName(t, c, ToolHangUpGPT).Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("hang up: %v", err)
	}
	if out["status"] != "ended" || out["thread_id"] != "th_fake" {
		t.Fatalf("hang up out = %v", out)
	}
	if c.CallActive() || !agent.isClosed() {
		t.Fatal("hang up must close the agent process and end the call")
	}
}

func TestApprovalAnnouncedNeverModelDecidable(t *testing.T) {
	agent := newFakeAgent(agentbridge.ModeAppServer)
	notifier := &recordingNotifier{}
	c := New(testPolicy(), func() agentbridge.Agent { return agent }, notifier, nil)
	defer c.Close()
	if _, err := toolByName(t, c, ToolCallGPT).Invoke(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// No approval tool exists on the surface.
	for _, tool := range c.Tools() {
		if strings.Contains(tool.Name(), "approve") {
			t.Fatalf("approval must not be model-invocable, found tool %s", tool.Name())
		}
	}

	agent.events <- agentbridge.Event{Type: agentbridge.EventApprovalRequested, Approval: &agentbridge.ApprovalRequest{ID: "approval-1", Kind: "command", Command: "npm install", Summary: "npm install"}}
	waitFor(t, func() bool {
		_, approvals := notifier.snapshot()
		return len(approvals) == 1
	})

	st, _ := toolByName(t, c, ToolGPTStatus).Invoke(context.Background(), nil)
	if st["phase"] != "awaiting_approval" || !strings.Contains(st["pending_approval"].(string), "npm install") {
		t.Fatalf("status out = %v", st)
	}

	// The HOST answers; the fake records the decision.
	if err := c.RespondApproval(context.Background(), "approval-1", agentbridge.DecisionApprove); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if agent.approvals["approval-1"] != agentbridge.DecisionApprove {
		t.Fatalf("approval not forwarded: %v", agent.approvals)
	}
}

func TestIdleTimeoutHangsUp(t *testing.T) {
	p := testPolicy()
	p.CallIdleHangup = 150 * time.Millisecond
	agent := newFakeAgent(agentbridge.ModeAppServer)
	notifier := &recordingNotifier{}
	c := New(p, func() agentbridge.Agent { return agent }, notifier, nil)
	defer c.Close()
	if _, err := toolByName(t, c, ToolCallGPT).Invoke(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	// The narration is emitted after hangUp() has already cleared the call
	// state and closed the agent, so waiting on those flags races the
	// notifier. Wait on the assertion's own observable instead.
	idleNarrated := func() bool {
		narrated, _ := notifier.snapshot()
		for _, n := range narrated {
			if strings.Contains(n, "ended after inactivity") {
				return true
			}
		}
		return false
	}
	waitFor(t, func() bool { return !c.CallActive() && agent.isClosed() && idleNarrated() })
	if !idleNarrated() {
		narrated, _ := notifier.snapshot()
		t.Fatalf("idle hang-up must narrate; got %v", narrated)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}
