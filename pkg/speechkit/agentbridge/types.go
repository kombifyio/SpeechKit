// Package agentbridge defines the framework-neutral seam through which a
// SpeechKit host fronts an external coding agent (adopted 2026-08-10,
// AI-VOICE-SPEECHKIT-TARGET.md "External Coding Agent Bridge"). The first
// implementation drives the official OpenAI Codex binary
// (agentbridge/codex); a Claude Code adapter can implement the same seam
// later. The seam abstracts at the "coding agent session" level —
// start/steer/interrupt/status/events/approvals — never at the wire-protocol
// level of any one CLI.
//
// Security posture is fail-closed by design: the bridge only *detects* the
// agent CLI's own login, never implements or refreshes OAuth material;
// side-effectful sandbox levels require explicit per-project configuration;
// and approval decisions are host-UI actions, deliberately not part of any
// model-invocable tool surface.
package agentbridge

import (
	"context"
	"errors"
)

// AuthMethod describes how the external agent CLI is signed in.
type AuthMethod string

const (
	AuthChatGPT AuthMethod = "chatgpt" // agent CLI holds a ChatGPT-subscription login
	AuthAPIKey  AuthMethod = "api_key" // agent CLI is configured with a platform API key
	AuthNone    AuthMethod = "none"    // installed but not signed in
)

// Mode is the transport the bridge is currently able to use.
type Mode string

const (
	ModeAppServer   Mode = "app_server"  // stateful JSON-RPC control surface (M2)
	ModeExec        Mode = "exec"        // one-shot non-interactive turns
	ModeUnavailable Mode = "unavailable" // not installed / not signed in / disabled
)

// SandboxMode is the execution sandbox ceiling for agent turns.
// "danger-full-access" is deliberately unrepresentable.
type SandboxMode string

const (
	SandboxReadOnly       SandboxMode = "read-only"
	SandboxWorkspaceWrite SandboxMode = "workspace-write"
)

// Decision answers an ApprovalRequest.
type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionDeny    Decision = "deny"
)

// Status reports the bridge's current capability, in terms a host can render
// or speak verbatim. It never contains token material.
type Status struct {
	Installed  bool
	BinaryPath string
	Version    string
	Auth       AuthMethod
	Plan       string // best-effort plan label ("plus", "pro", ...); empty when unknown
	Mode       Mode
	Detail     string // human-readable degradation reason; speakable
}

// Project is an allowlisted working directory the agent may operate in.
type Project struct {
	Alias   string // spoken-friendly unique name ("speechkit")
	Path    string // absolute directory
	Sandbox SandboxMode
}

// TurnRequest starts (or continues) one agent turn.
type TurnRequest struct {
	Project  Project
	Prompt   string
	ThreadID string // non-empty = resume/continue that thread
	Sandbox  SandboxMode
}

// ThreadRef identifies the turn that was started. ThreadID may be empty on a
// fresh thread until the implementation reports it via EventThreadStarted.
type ThreadRef struct {
	ThreadID string
	TurnID   string
}

// EventType enumerates normalized bridge events.
type EventType string

const (
	EventThreadStarted     EventType = "thread_started"
	EventTurnStarted       EventType = "turn_started"
	EventItemStarted       EventType = "item_started"
	EventItemCompleted     EventType = "item_completed"
	EventTurnCompleted     EventType = "turn_completed"
	EventApprovalRequested EventType = "approval_requested"
	EventBridgeState       EventType = "bridge_state"
	EventError             EventType = "error"
)

// Item is one unit of agent work (message, reasoning, command, file change).
type Item struct {
	Kind    string // agent_message | reasoning | command_execution | file_change | mcp_tool_call | web_search | ...
	Summary string // short human-readable description of the item
}

// ApprovalRequest asks the host to approve a side effect. The host renders
// Command/Summary verbatim from agent-delivered data — never from model
// paraphrase — and answers via Agent.RespondApproval.
type ApprovalRequest struct {
	ID      string
	Kind    string // "command" | "patch"
	Command string // exact command line for the command kind
	Summary string
	Cwd     string
}

// Event is the normalized stream a host consumes.
type Event struct {
	Type     EventType
	ThreadID string
	TurnID   string
	Item     *Item
	Approval *ApprovalRequest
	Err      string
}

// Sentinel errors implementations return so hosts can degrade with honest,
// speakable messages.
var (
	ErrNotInstalled = errors.New("agentbridge: agent binary not installed")
	ErrNotSignedIn  = errors.New("agentbridge: agent binary installed but not signed in")
	ErrUnsupported  = errors.New("agentbridge: operation not supported in the current mode")
	ErrBusy         = errors.New("agentbridge: another turn is already running")
)

// Agent is the external-coding-agent seam. Implementations must be safe for
// concurrent use by one host.
type Agent interface {
	// Status reports installation, auth, and transport capability.
	Status(ctx context.Context) Status
	// StartTurn acknowledges fast (well under a second) while the turn keeps
	// running asynchronously; progress arrives on Events.
	StartTurn(ctx context.Context, req TurnRequest) (ThreadRef, error)
	// Steer injects mid-turn guidance. Exec-mode implementations return
	// ErrUnsupported.
	Steer(ctx context.Context, ref ThreadRef, text string) error
	// Interrupt stops the running turn.
	Interrupt(ctx context.Context, ref ThreadRef) error
	// RespondApproval answers a pending ApprovalRequest by ID.
	RespondApproval(ctx context.Context, id string, d Decision) error
	// Events returns the normalized event stream. Closed by Close.
	Events() <-chan Event
	Close() error
}
