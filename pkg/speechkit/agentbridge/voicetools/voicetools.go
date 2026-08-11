// Package voicetools binds an agentbridge.Agent to the voice agent's tool
// surface with the "Call GPT" semantics (owner decision 2026-08-10,
// AI-VOICE-SPEECHKIT-TARGET.md External Coding Agent Bridge):
//
//   - The bridge is NEVER a standing default. No agent process exists until
//     the user explicitly places the call ("Call GPT") mid-conversation.
//   - Task tools (gpt_task/gpt_status/gpt_steer/gpt_stop) work only during
//     an active call and fail closed with a speakable hint otherwise.
//   - hang_up_gpt — or the idle timeout — tears the agent process down
//     again. Threads stay resumable across calls.
//   - Approval decisions are deliberately NOT a tool: the model can announce
//     a pending approval, but only the host UI answers it
//     (Coordinator.RespondApproval, wired to the overlay card in the
//     device adapter).
package voicetools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
)

// Tool names the model sees. call_gpt is the only entry point; everything
// else requires the active call.
const (
	ToolCallGPT   = "call_gpt"
	ToolHangUpGPT = "hang_up_gpt"
	ToolGPTTask   = "gpt_task"
	ToolGPTStatus = "gpt_status"
	ToolGPTSteer  = "gpt_steer"
	ToolGPTStop   = "gpt_stop"
)

// Notifier is the host sink for narration and approval announcements. The
// device adapter delivers Narrate as an agent_progress host prompt only while
// the voice session is listening; AnnounceApproval renders the overlay card.
type Notifier interface {
	Narrate(text string)
	AnnounceApproval(agentbridge.ApprovalRequest)
}

// Project is one allowlisted working directory (mirrors the
// [agent_bridge.codex.projects] config entries).
type Project struct {
	Alias   string
	Path    string
	Sandbox agentbridge.SandboxMode
}

// Policy is the fail-closed gate for the tool surface.
type Policy struct {
	// Enabled mirrors the double config gate; false hides nothing but makes
	// call_gpt refuse with a speakable reason (the tool list is static per
	// session on most realtime providers).
	Enabled bool
	// Projects is the allowlist; gpt_task resolves aliases against it and
	// refuses anything else.
	Projects []Project
	// DefaultSandbox caps every turn; effective = min(default, project).
	DefaultSandbox agentbridge.SandboxMode
	// CallIdleHangup ends the call after inactivity (default 10 minutes).
	CallIdleHangup time.Duration
	// NarrationMinInterval rate-limits progress narration (default 20s).
	// Terminal events (done, error, approval) always narrate.
	NarrationMinInterval time.Duration
}

func (p *Policy) normalize() {
	if p.DefaultSandbox == "" {
		p.DefaultSandbox = agentbridge.SandboxReadOnly
	}
	if p.CallIdleHangup <= 0 {
		p.CallIdleHangup = 10 * time.Minute
	}
	if p.NarrationMinInterval <= 0 {
		p.NarrationMinInterval = 20 * time.Second
	}
}

// Coordinator owns the call lifecycle: one optional agent process, its event
// pump, the idle timer, and the state gpt_status reports.
type Coordinator struct {
	policy   Policy
	newAgent func() agentbridge.Agent
	notifier Notifier
	logger   *slog.Logger

	mu           sync.Mutex
	agent        agentbridge.Agent
	pumpDone     chan struct{}
	busy         bool
	phase        string
	lastThread   agentbridge.ThreadRef
	recent       []string
	pendingAppr  *agentbridge.ApprovalRequest
	lastNarrated time.Time
	idleTimer    *time.Timer
}

// New wires a Coordinator. newAgent is invoked once per call (and the agent
// closed on hang-up), so no process outlives a call.
func New(policy Policy, newAgent func() agentbridge.Agent, notifier Notifier, logger *slog.Logger) *Coordinator {
	policy.normalize()
	if logger == nil {
		logger = slog.Default()
	}
	return &Coordinator{policy: policy, newAgent: newAgent, notifier: notifier, logger: logger, phase: "no_call"}
}

// CallActive reports whether a call is currently open.
func (c *Coordinator) CallActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.agent != nil
}

// RespondApproval answers a pending approval from the HOST UI (overlay
// card). Deliberately not exposed as a model tool.
func (c *Coordinator) RespondApproval(ctx context.Context, id string, d agentbridge.Decision) error {
	c.mu.Lock()
	agent := c.agent
	if c.pendingAppr != nil && c.pendingAppr.ID == id {
		c.pendingAppr = nil
	}
	c.mu.Unlock()
	if agent == nil {
		return fmt.Errorf("no active call")
	}
	return agent.RespondApproval(ctx, id, d)
}

// Close hangs up if needed. Safe to call repeatedly.
func (c *Coordinator) Close() { c.hangUp("session closed") }

// Tools returns the six-call tool surface for agentkit registration.
func (c *Coordinator) Tools() []agentkit.Tool {
	return []agentkit.Tool{
		&agentkit.FuncTool{
			ToolName: ToolCallGPT,
			ToolDescription: "Place the call to GPT/Codex, the external coding agent. Use ONLY when the user " +
				"explicitly asks to call GPT (\"call GPT\", \"ruf ChatGPT an\"). Reports connection, plan, and available projects.",
			Fn: c.callGPT,
		},
		&agentkit.FuncTool{
			ToolName:        ToolHangUpGPT,
			ToolDescription: "End the GPT call. Interrupts any running task; the conversation thread stays resumable for a later call.",
			Fn:              c.hangUpTool,
		},
		&agentkit.FuncTool{
			ToolName:        ToolGPTTask,
			ToolDescription: "During an active GPT call: hand a coding task to the agent in one of the allowlisted projects. Returns immediately; progress is narrated.",
			ToolSchema: agentkit.Schema{
				"type": "object",
				"properties": map[string]any{
					"project":         map[string]any{"type": "string", "description": "Allowlisted project alias"},
					"prompt":          map[string]any{"type": "string", "description": "The task for the coding agent"},
					"continue_thread": map[string]any{"type": "boolean", "description": "Continue the previous conversation thread instead of starting fresh"},
				},
				"required": []string{"project", "prompt"},
			},
			Fn: c.task,
		},
		&agentkit.FuncTool{
			ToolName:        ToolGPTStatus,
			ToolDescription: "During an active GPT call: report what the coding agent is doing right now.",
			Fn:              c.status,
		},
		&agentkit.FuncTool{
			ToolName:        ToolGPTSteer,
			ToolDescription: "During an active GPT call: inject guidance into the running task without stopping it.",
			ToolSchema: agentkit.Schema{
				"type":       "object",
				"properties": map[string]any{"instruction": map[string]any{"type": "string"}},
				"required":   []string{"instruction"},
			},
			Fn: c.steer,
		},
		&agentkit.FuncTool{
			ToolName:        ToolGPTStop,
			ToolDescription: "During an active GPT call: stop the running task (the call stays open).",
			Fn:              c.stop,
		},
	}
}

func (c *Coordinator) callGPT(ctx context.Context, _ map[string]any) (map[string]any, error) {
	if !c.policy.Enabled {
		return nil, fmt.Errorf("the coding bridge is disabled in Settings — enable agent_bridge to use Call GPT")
	}
	c.mu.Lock()
	if c.agent != nil {
		c.mu.Unlock()
		return map[string]any{"status": "already_connected", "hint": "the GPT call is already active"}, nil
	}
	c.mu.Unlock()

	agent := c.newAgent()
	st := agent.Status(ctx)
	if st.Mode == agentbridge.ModeUnavailable {
		_ = agent.Close()
		detail := st.Detail
		if detail == "" {
			detail = "the coding agent is unavailable"
		}
		return map[string]any{"status": "unavailable", "detail": detail}, nil
	}

	pump := make(chan struct{})
	c.mu.Lock()
	c.agent = agent
	c.pumpDone = pump
	c.busy = false
	c.phase = "connected"
	c.pendingAppr = nil
	c.recent = nil
	c.mu.Unlock()
	go c.pump(agent, pump) //nolint:contextcheck // detached cleanup: hangUp must run to completion on its own timeout, not the caller ctx
	c.armIdleTimer()       //nolint:contextcheck // detached cleanup: hangUp must run to completion on its own timeout, not the caller ctx
	c.logger.Info("voicetools: gpt call opened", "auth", string(st.Auth), "plan", st.Plan, "mode", string(st.Mode))

	aliases := make([]string, 0, len(c.policy.Projects))
	for _, p := range c.policy.Projects {
		aliases = append(aliases, p.Alias)
	}
	return map[string]any{
		"status":   "connected",
		"agent":    "codex",
		"auth":     string(st.Auth),
		"plan":     st.Plan,
		"mode":     string(st.Mode),
		"projects": aliases,
	}, nil
}

func (c *Coordinator) hangUpTool(context.Context, map[string]any) (map[string]any, error) {
	c.mu.Lock()
	active := c.agent != nil
	thread := c.lastThread.ThreadID
	c.mu.Unlock()
	if !active {
		return map[string]any{"status": "no_call"}, nil
	}
	c.hangUp("hang up requested") //nolint:contextcheck // detached cleanup: hangUp must run to completion on its own timeout, not the caller ctx
	return map[string]any{"status": "ended", "thread_id": thread, "hint": "call again to resume the thread"}, nil
}

func (c *Coordinator) task(ctx context.Context, args map[string]any) (map[string]any, error) {
	agent, err := c.requireCall()
	if err != nil {
		return nil, err
	}
	alias, _ := args["project"].(string)
	prompt, _ := args["prompt"].(string)
	cont, _ := args["continue_thread"].(bool)
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("the task prompt is empty")
	}
	project, ok := c.projectByAlias(alias)
	if !ok {
		return nil, fmt.Errorf("project %q is not allowlisted — available: %s", alias, strings.Join(c.projectAliases(), ", "))
	}
	req := agentbridge.TurnRequest{
		Project: agentbridge.Project{Alias: project.Alias, Path: project.Path, Sandbox: project.Sandbox},
		Prompt:  prompt,
		Sandbox: minSandbox(c.policy.DefaultSandbox, project.Sandbox),
	}
	if cont {
		c.mu.Lock()
		req.ThreadID = c.lastThread.ThreadID
		c.mu.Unlock()
	}
	ref, err := agent.StartTurn(ctx, req)
	if err != nil {
		if errors.Is(err, agentbridge.ErrBusy) {
			return nil, fmt.Errorf("the coding agent is already working — steer it, stop it, or ask for status")
		}
		return nil, err
	}
	c.mu.Lock()
	c.busy = true
	c.phase = "working"
	if ref.ThreadID != "" {
		c.lastThread = ref
	}
	c.mu.Unlock()
	c.armIdleTimer() //nolint:contextcheck // detached cleanup: hangUp must run to completion on its own timeout, not the caller ctx
	return map[string]any{"status": "started", "project": project.Alias, "sandbox": string(req.Sandbox), "thread_id": ref.ThreadID}, nil
}

func (c *Coordinator) status(context.Context, map[string]any) (map[string]any, error) {
	if _, err := c.requireCall(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]any{
		"phase":     c.phase,
		"busy":      c.busy,
		"thread_id": c.lastThread.ThreadID,
		"recent":    append([]string(nil), c.recent...),
	}
	if c.pendingAppr != nil {
		out["pending_approval"] = fmt.Sprintf("%s: %s (answer on screen, not by voice)", c.pendingAppr.Kind, c.pendingAppr.Summary)
	}
	return out, nil
}

func (c *Coordinator) steer(ctx context.Context, args map[string]any) (map[string]any, error) {
	agent, err := c.requireCall()
	if err != nil {
		return nil, err
	}
	instruction, _ := args["instruction"].(string)
	if strings.TrimSpace(instruction) == "" {
		return nil, fmt.Errorf("the steering instruction is empty")
	}
	c.mu.Lock()
	ref := c.lastThread
	c.mu.Unlock()
	if err := agent.Steer(ctx, ref, instruction); err != nil {
		if errors.Is(err, agentbridge.ErrUnsupported) {
			return nil, fmt.Errorf("steering is not available in the current mode — stop the task and start a new one instead")
		}
		return nil, err
	}
	c.armIdleTimer() //nolint:contextcheck // detached cleanup: hangUp must run to completion on its own timeout, not the caller ctx
	return map[string]any{"status": "steered"}, nil
}

func (c *Coordinator) stop(ctx context.Context, _ map[string]any) (map[string]any, error) {
	agent, err := c.requireCall()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	ref := c.lastThread
	c.mu.Unlock()
	if err := agent.Interrupt(ctx, ref); err != nil {
		return nil, err
	}
	c.armIdleTimer() //nolint:contextcheck // detached cleanup: hangUp must run to completion on its own timeout, not the caller ctx
	return map[string]any{"status": "stopping"}, nil
}

func (c *Coordinator) requireCall() (agentbridge.Agent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.agent == nil {
		return nil, fmt.Errorf("no active GPT call — say \"Call GPT\" first")
	}
	return c.agent, nil
}

func (c *Coordinator) projectByAlias(alias string) (Project, bool) {
	alias = strings.TrimSpace(alias)
	for _, p := range c.policy.Projects {
		if strings.EqualFold(p.Alias, alias) {
			if p.Sandbox == "" {
				p.Sandbox = agentbridge.SandboxReadOnly
			}
			return p, true
		}
	}
	return Project{}, false
}

func (c *Coordinator) projectAliases() []string {
	out := make([]string, 0, len(c.policy.Projects))
	for _, p := range c.policy.Projects {
		out = append(out, p.Alias)
	}
	return out
}

// hangUp tears the call down: interrupt best-effort, close the agent
// process, stop the pump, reset state.
func (c *Coordinator) hangUp(reason string) {
	c.mu.Lock()
	agent := c.agent
	pump := c.pumpDone
	timer := c.idleTimer
	c.agent = nil
	c.pumpDone = nil
	c.idleTimer = nil
	c.busy = false
	c.phase = "no_call"
	c.pendingAppr = nil
	c.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if agent == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = agent.Interrupt(ctx, agentbridge.ThreadRef{})
	cancel()
	_ = agent.Close()
	if pump != nil {
		<-pump
	}
	c.logger.Info("voicetools: gpt call ended", "reason", reason)
}

// armIdleTimer (re)starts the inactivity hang-up.
func (c *Coordinator) armIdleTimer() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.agent == nil {
		return
	}
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	c.idleTimer = time.AfterFunc(c.policy.CallIdleHangup, func() {
		if c.CallActive() {
			c.hangUp("idle timeout")
			c.notifier.Narrate("The GPT call ended after inactivity. Say \"Call GPT\" to reconnect.")
		}
	})
}

// pump drains agent events into state + narration until the agent closes.
func (c *Coordinator) pump(agent agentbridge.Agent, done chan struct{}) {
	defer close(done)
	for ev := range agent.Events() {
		c.observe(ev)
	}
}

func (c *Coordinator) observe(ev agentbridge.Event) {
	c.mu.Lock()
	if ev.ThreadID != "" {
		c.lastThread = agentbridge.ThreadRef{ThreadID: ev.ThreadID, TurnID: ev.TurnID}
	}
	var narration string
	force := false
	switch ev.Type {
	case agentbridge.EventItemCompleted:
		if ev.Item != nil && ev.Item.Summary != "" {
			c.pushRecent(ev.Item.Kind + ": " + ev.Item.Summary)
			if ev.Item.Kind == "command_execution" || ev.Item.Kind == "file_change" {
				narration = "GPT: " + ev.Item.Summary
			}
		}
	case agentbridge.EventTurnCompleted:
		c.busy = false
		c.phase = "idle"
		narration = "GPT is done"
		if last := c.lastAgentMessage(); last != "" {
			narration = "GPT is done: " + last
		}
		force = true
	case agentbridge.EventError:
		c.busy = false
		c.phase = "error"
		narration = "GPT ran into a problem: " + ev.Err
		force = true
	case agentbridge.EventApprovalRequested:
		if ev.Approval != nil {
			appr := *ev.Approval
			c.pendingAppr = &appr
			c.phase = "awaiting_approval"
			c.mu.Unlock()
			c.notifier.AnnounceApproval(appr)
			c.notifier.Narrate("GPT wants to run: " + appr.Summary + " — there is an approval card on screen.")
			c.armIdleTimer()
			return
		}
	case agentbridge.EventBridgeState:
		narration = ev.Err
		force = true
	case agentbridge.EventThreadStarted, agentbridge.EventTurnStarted, agentbridge.EventItemStarted:
		// Lifecycle bookkeeping only — the thread ref was captured above and
		// starts are not narration-worthy (completions are).
	}
	now := time.Now()
	if narration != "" && (force || now.Sub(c.lastNarrated) >= c.policy.NarrationMinInterval) {
		c.lastNarrated = now
		c.mu.Unlock()
		c.notifier.Narrate(narration)
		c.armIdleTimer()
		return
	}
	c.mu.Unlock()
	c.armIdleTimer()
}

func (c *Coordinator) pushRecent(entry string) {
	c.recent = append(c.recent, entry)
	if len(c.recent) > 5 {
		c.recent = c.recent[len(c.recent)-5:]
	}
}

func (c *Coordinator) lastAgentMessage() string {
	for i := len(c.recent) - 1; i >= 0; i-- {
		if strings.HasPrefix(c.recent[i], "agent_message: ") {
			return strings.TrimPrefix(c.recent[i], "agent_message: ")
		}
	}
	return ""
}

func minSandbox(a, b agentbridge.SandboxMode) agentbridge.SandboxMode {
	if a == agentbridge.SandboxWorkspaceWrite && b == agentbridge.SandboxWorkspaceWrite {
		return agentbridge.SandboxWorkspaceWrite
	}
	return agentbridge.SandboxReadOnly
}
