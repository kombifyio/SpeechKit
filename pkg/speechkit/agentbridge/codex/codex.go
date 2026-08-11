// Package codex drives the official OpenAI Codex binary as an
// agentbridge.Agent. Two transports behind one seam: the stateful
// `codex app-server` JSON-RPC connection (primary — steer, interrupt, and
// approval round-trips) and `codex exec --json` one-shot turns (fallback).
// Wire shapes are pinned against `codex app-server generate-json-schema`
// from codex-cli 0.147.0; unknown notifications and fields are tolerated.
//
// Auth is detection-only: the user signs the Codex CLI in themselves
// (ChatGPT subscription or API key), the bridge reads nothing beyond the
// sign-in method and plan label, and a policy reversal on OpenAI's side
// degrades to API-key-authed Codex with zero code change here.
package codex

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

// Config configures the bridge. All fields are optional.
type Config struct {
	// BinaryPath pins the codex binary; empty means PATH lookup.
	BinaryPath string
	// CodexHome overrides the auth-detection directory (tests); empty means
	// $CODEX_HOME or ~/.codex.
	CodexHome string
	// Mode selects the transport: "auto" (default; app-server with exec
	// fallback), "app_server", or "exec".
	Mode string
	// ClientVersion is reported in the app-server initialize handshake.
	ClientVersion string
	// EventBuffer sizes the Events channel; default 64.
	EventBuffer int
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// Restart budget for the app-server child: after maxSpawnAttempts failed or
// died sessions inside spawnWindow the bridge degrades to exec mode for its
// lifetime (announced via EventBridgeState).
const (
	maxSpawnAttempts = 3
	spawnWindow      = 5 * time.Minute
)

// Bridge implements agentbridge.Agent against the codex CLI. One turn runs
// at a time; concurrent StartTurn returns ErrBusy.
type Bridge struct {
	cfg    Config
	logger *slog.Logger
	events chan agentbridge.Event

	mu           sync.Mutex
	closed       bool
	current      *execTurn // exec-mode turn
	session      *appServerSession
	sessionGen   int
	asBusy       bool // app-server turn in flight
	execDegraded bool
	spawnTimes   []time.Time
	lastRef      agentbridge.ThreadRef
}

var _ agentbridge.Agent = (*Bridge)(nil)

// New constructs the bridge. Construction never fails — capability is
// reported honestly through Status so hosts can render/speak the reason.
func New(cfg Config) *Bridge {
	if cfg.EventBuffer <= 0 {
		cfg.EventBuffer = 64
	}
	if cfg.Mode == "" {
		cfg.Mode = "auto"
	}
	if cfg.ClientVersion == "" {
		cfg.ClientVersion = "dev"
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{cfg: cfg, logger: logger, events: make(chan agentbridge.Event, cfg.EventBuffer)}
}

// effectiveMode resolves the transport the bridge would use right now.
func (b *Bridge) effectiveMode() agentbridge.Mode {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cfg.Mode == "exec" || b.execDegraded {
		return agentbridge.ModeExec
	}
	return agentbridge.ModeAppServer
}

// Status implements agentbridge.Agent.
func (b *Bridge) Status(ctx context.Context) agentbridge.Status {
	st := agentbridge.Status{Mode: agentbridge.ModeUnavailable, Auth: agentbridge.AuthNone}
	binary, err := resolveBinary(b.cfg.BinaryPath)
	if err != nil {
		st.Detail = "Codex is not installed — install the official codex CLI to enable the coding bridge"
		return st
	}
	st.Installed = true
	st.BinaryPath = binary
	if version, err := probeVersion(ctx, binary); err == nil {
		st.Version = version
	} else {
		st.Detail = fmt.Sprintf("codex --version failed: %v", err)
	}
	auth := detectAuth(codexHome(b.cfg.CodexHome))
	st.Auth = auth.Method
	st.Plan = auth.Plan
	if auth.Method == agentbridge.AuthNone {
		st.Detail = "Codex is installed but not signed in — run 'codex login' once"
		return st
	}
	st.Mode = b.effectiveMode()
	return st
}

// StartTurn implements agentbridge.Agent (fast-ack; the turn continues in
// the background and progress arrives on Events).
func (b *Bridge) StartTurn(ctx context.Context, req agentbridge.TurnRequest) (agentbridge.ThreadRef, error) {
	st := b.Status(ctx)
	switch {
	case !st.Installed:
		return agentbridge.ThreadRef{}, agentbridge.ErrNotInstalled
	case st.Auth == agentbridge.AuthNone:
		return agentbridge.ThreadRef{}, agentbridge.ErrNotSignedIn
	}
	if req.Sandbox == "" {
		req.Sandbox = agentbridge.SandboxReadOnly
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return agentbridge.ThreadRef{}, agentbridge.ErrUnsupported
	}
	if b.asBusy {
		b.mu.Unlock()
		return agentbridge.ThreadRef{}, agentbridge.ErrBusy
	}
	if b.current != nil {
		select {
		case <-b.current.done:
			b.current = nil
		default:
			b.mu.Unlock()
			return agentbridge.ThreadRef{}, agentbridge.ErrBusy
		}
	}
	b.mu.Unlock()

	if b.effectiveMode() == agentbridge.ModeAppServer {
		ref, err := b.startAppServerTurn(ctx, st.BinaryPath, req)
		if err == nil {
			return ref, nil
		}
		if b.cfg.Mode == "app_server" {
			// Forced app-server mode: no silent transport change.
			return agentbridge.ThreadRef{}, err
		}
		b.logger.Warn("agentbridge: app-server unavailable, falling back to exec for this turn", "err", err)
	}
	return b.startExecModeTurn(req, st.BinaryPath) //nolint:contextcheck // fast-ack: the turn deliberately outlives the caller's ctx (see startExecModeTurn)
}

// startAppServerTurn ensures a live app-server session and submits the turn.
func (b *Bridge) startAppServerTurn(ctx context.Context, binary string, req agentbridge.TurnRequest) (agentbridge.ThreadRef, error) {
	session, err := b.ensureSession(ctx, binary)
	if err != nil {
		return agentbridge.ThreadRef{}, err
	}
	threadID, err := session.StartThread(ctx, req.ThreadID, req.Project.Path, req.Sandbox)
	if err != nil {
		return agentbridge.ThreadRef{}, fmt.Errorf("thread start: %w", err)
	}
	turnID, err := session.StartTurn(ctx, threadID, req.Prompt)
	if err != nil {
		return agentbridge.ThreadRef{}, fmt.Errorf("turn start: %w", err)
	}
	ref := agentbridge.ThreadRef{ThreadID: threadID, TurnID: turnID}
	b.mu.Lock()
	b.asBusy = true
	b.lastRef = ref
	b.mu.Unlock()
	b.logger.Info("agentbridge: codex app-server turn started",
		"project", req.Project.Alias, "sandbox", string(req.Sandbox), "thread", threadID)
	return ref, nil
}

// startExecModeTurn runs the one-shot fallback transport.
func (b *Bridge) startExecModeTurn(req agentbridge.TurnRequest, binary string) (agentbridge.ThreadRef, error) {
	// The turn must outlive the StartTurn call context: fast-ack semantics
	// mean the caller's ctx may end long before the turn does.
	turn, err := startExecTurn(context.Background(), binary, req, b.emit)
	if err != nil {
		return agentbridge.ThreadRef{}, err
	}
	b.logger.Info("agentbridge: codex exec turn started",
		"project", req.Project.Alias, "sandbox", string(req.Sandbox), "resume", req.ThreadID != "")
	b.mu.Lock()
	b.current = turn
	ref := agentbridge.ThreadRef{ThreadID: req.ThreadID}
	b.lastRef = ref
	b.mu.Unlock()
	return ref, nil
}

// ensureSession returns the live app-server session, spawning one under the
// restart budget. Budget exhaustion flips the bridge into exec mode for its
// lifetime and announces it as a bridge_state event.
func (b *Bridge) ensureSession(ctx context.Context, binary string) (*appServerSession, error) {
	b.mu.Lock()
	if b.session != nil {
		select {
		case <-b.session.Done():
			b.session = nil // died since last use; fall through to respawn
		default:
			s := b.session
			b.mu.Unlock()
			return s, nil
		}
	}
	now := time.Now()
	recent := b.spawnTimes[:0]
	for _, t := range b.spawnTimes {
		if now.Sub(t) < spawnWindow {
			recent = append(recent, t)
		}
	}
	b.spawnTimes = recent
	if len(b.spawnTimes) >= maxSpawnAttempts {
		b.execDegraded = true
		b.mu.Unlock()
		b.emit(agentbridge.Event{Type: agentbridge.EventBridgeState,
			Err: fmt.Sprintf("codex app-server failed %d times within %s — bridge degraded to exec mode (no steer/approvals)", maxSpawnAttempts, spawnWindow)})
		return nil, fmt.Errorf("app-server restart budget exhausted")
	}
	b.spawnTimes = append(b.spawnTimes, now)
	gen := b.sessionGen + 1
	b.sessionGen = gen
	b.mu.Unlock()

	session, err := startAppServer(ctx, binary, b.cfg.ClientVersion, b.logger, b.observeSessionEvent)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.session = session
	b.mu.Unlock()
	// Watch for unexpected death so an in-flight turn fails loudly instead
	// of hanging silently.
	go func() {
		<-session.Done()
		b.mu.Lock()
		wasBusy := b.asBusy && b.session == session && b.sessionGen == gen
		if b.session == session {
			b.session = nil
			b.asBusy = false
		}
		b.mu.Unlock()
		if wasBusy {
			b.emit(agentbridge.Event{Type: agentbridge.EventError, Err: "codex app-server exited during the turn"})
		}
	}()
	return session, nil
}

// observeSessionEvent tracks turn lifecycle for the busy guard and forwards
// every session event to the host stream.
func (b *Bridge) observeSessionEvent(ev agentbridge.Event) {
	if ev.Type == agentbridge.EventTurnCompleted || ev.Type == agentbridge.EventError {
		b.mu.Lock()
		b.asBusy = false
		b.mu.Unlock()
	}
	b.emit(ev)
}

// Steer implements agentbridge.Agent. Exec mode cannot steer mid-turn.
func (b *Bridge) Steer(ctx context.Context, ref agentbridge.ThreadRef, text string) error {
	b.mu.Lock()
	session := b.session
	b.mu.Unlock()
	if session == nil {
		return agentbridge.ErrUnsupported
	}
	threadID, turnID := ref.ThreadID, ref.TurnID
	if threadID == "" || turnID == "" {
		threadID, turnID = session.CurrentRefs()
	}
	return session.SteerTurn(ctx, threadID, turnID, text)
}

// Interrupt implements agentbridge.Agent.
func (b *Bridge) Interrupt(ctx context.Context, ref agentbridge.ThreadRef) error {
	b.mu.Lock()
	session := b.session
	turn := b.current
	b.mu.Unlock()
	if session != nil {
		threadID, turnID := ref.ThreadID, ref.TurnID
		if threadID == "" || turnID == "" {
			threadID, turnID = session.CurrentRefs()
		}
		return session.InterruptTurn(ctx, threadID, turnID)
	}
	if turn != nil {
		return turn.interrupt()
	}
	return nil
}

// RespondApproval implements agentbridge.Agent. Only app-server mode raises
// approvals; exec mode runs non-interactively.
func (b *Bridge) RespondApproval(_ context.Context, id string, d agentbridge.Decision) error {
	b.mu.Lock()
	session := b.session
	b.mu.Unlock()
	if session == nil {
		return agentbridge.ErrUnsupported
	}
	return session.RespondApproval(id, d)
}

// Events implements agentbridge.Agent.
func (b *Bridge) Events() <-chan agentbridge.Event { return b.events }

// Close implements agentbridge.Agent: interrupts running work, denies
// pending approvals, and closes the event stream.
func (b *Bridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	turn := b.current
	session := b.session
	b.session = nil
	b.mu.Unlock()
	if session != nil {
		session.Close()
	}
	if turn != nil {
		_ = turn.interrupt()
		turn.wait()
	}
	close(b.events)
	return nil
}

// emit forwards an event without ever blocking the pump: when the host is
// not draining, the oldest signal is dropped in favor of the newest.
func (b *Bridge) emit(ev agentbridge.Event) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if ev.ThreadID != "" {
		b.lastRef = agentbridge.ThreadRef{ThreadID: ev.ThreadID, TurnID: ev.TurnID}
	}
	b.mu.Unlock()
	select {
	case b.events <- ev:
	default:
		select {
		case <-b.events:
		default:
		}
		select {
		case b.events <- ev:
		default:
		}
	}
}

// LastThreadRef returns the most recently observed thread reference — the
// default target for "continue working on that" follow-up turns.
func (b *Bridge) LastThreadRef() agentbridge.ThreadRef {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastRef
}
