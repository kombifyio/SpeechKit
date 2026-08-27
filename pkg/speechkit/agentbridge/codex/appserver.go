package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/procguard"
)

// Wire method names, pinned against `codex app-server generate-json-schema`
// from codex-cli 0.147.0 (v2 protocol). Unknown notifications are ignored so
// newer Codex versions degrade gracefully.
const (
	methodInitialize    = "initialize"
	methodInitialized   = "initialized"
	methodThreadStart   = "thread/start"
	methodThreadResume  = "thread/resume"
	methodTurnStart     = "turn/start"
	methodTurnSteer     = "turn/steer"
	methodTurnInterrupt = "turn/interrupt"

	notifyThreadStarted = "thread/started"
	notifyTurnStarted   = "turn/started"
	notifyTurnCompleted = "turn/completed"
	notifyItemStarted   = "item/started"
	notifyItemCompleted = "item/completed"
	notifyError         = "error"

	// Server -> client approval requests (v2 names + v1 compatibility).
	reqCommandApproval     = "item/commandExecution/requestApproval"
	reqFileChangeApproval  = "item/fileChange/requestApproval"
	reqPermissionsApproval = "item/permissions/requestApproval"
	reqExecApprovalV1      = "execCommandApproval"
	reqApplyPatchV1        = "applyPatchApproval"

	// Decision values on the approval response wire (CommandExecution /
	// FileChange approval decision enums, codex-cli 0.147.0).
	wireDecisionAccept  = "accept"
	wireDecisionDecline = "decline"
)

// appServerSession owns one `codex app-server` child process and its JSON-RPC
// connection. All exported calls are safe for concurrent use.
type appServerSession struct {
	cmd    *exec.Cmd
	conn   *rpcConn
	logger *slog.Logger
	emit   func(agentbridge.Event)

	mu               sync.Mutex
	pendingApprovals map[string]chan agentbridge.Decision
	approvalSeq      int
	currentThreadID  string
	currentTurnID    string
}

// startAppServer spawns `codex app-server` on stdio and completes the
// mandatory initialize handshake.
func startAppServer(ctx context.Context, binary, clientVersion string, logger *slog.Logger, emit func(agentbridge.Event)) (*appServerSession, error) {
	// The app-server outlives individual turn contexts by design; its
	// lifecycle is owned by session.Close/kill, not ctx cancellation.
	cmd := exec.Command(binary, "app-server") //nolint:noctx // detached long-lived child, closed via session lifecycle
	configureSysProcAttr(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("app-server stdout: %w", err)
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("app-server start: %w", err)
	}
	// Hand the child to the OS so it cannot outlive this process when the
	// host dies without running its cleanup path (crash, taskkill, dev-loop
	// rebuild). Assignment failing does not make the child unusable.
	if err := procguard.Adopt(cmd); err != nil {
		logger.Warn("codex app-server not adopted into the kill-on-exit job", "error", err, "pid", cmd.Process.Pid)
	}

	s := &appServerSession{
		cmd:              cmd,
		logger:           logger,
		emit:             emit,
		pendingApprovals: map[string]chan agentbridge.Decision{},
	}
	s.conn = newRPCConn(stdout, stdin, s.handleNotification, s.handleServerRequest)

	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err = s.conn.Call(initCtx, methodInitialize, map[string]any{
		"clientInfo": map[string]any{
			"name":    "kombify-speechkit",
			"title":   "SpeechKit Voice Agent",
			"version": clientVersion,
		},
	})
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("app-server initialize: %w", err)
	}
	if err := s.conn.Notify(methodInitialized, nil); err != nil {
		s.Close()
		return nil, fmt.Errorf("app-server initialized notify: %w", err)
	}
	// Reap the child once the connection ends so a dead app-server never
	// leaves a zombie.
	go func() {
		<-s.conn.Done()
		_ = cmd.Wait()
	}()
	return s, nil
}

// Done reports process/connection end.
func (s *appServerSession) Done() <-chan struct{} { return s.conn.Done() }

// Close kills the child, denies every pending approval, and closes the
// connection.
func (s *appServerSession) Close() {
	s.mu.Lock()
	for id, ch := range s.pendingApprovals {
		select {
		case ch <- agentbridge.DecisionDeny:
		default:
		}
		delete(s.pendingApprovals, id)
	}
	s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	s.conn.Close()
}

// StartThread starts (or resumes) a thread in cwd and returns its id.
func (s *appServerSession) StartThread(ctx context.Context, threadID, cwd string, sandbox agentbridge.SandboxMode) (string, error) {
	method := methodThreadStart
	params := map[string]any{"cwd": cwd, "sandbox": string(sandbox)}
	if strings.TrimSpace(threadID) != "" {
		method = methodThreadResume
		params["threadId"] = threadID
	}
	raw, err := s.conn.Call(ctx, method, params)
	if err != nil {
		return "", err
	}
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("thread response decode: %w", err)
	}
	if resp.Thread.ID == "" && threadID != "" {
		return threadID, nil
	}
	s.mu.Lock()
	s.currentThreadID = resp.Thread.ID
	s.mu.Unlock()
	return resp.Thread.ID, nil
}

// StartTurn submits one user text input on the thread and returns the turn id.
func (s *appServerSession) StartTurn(ctx context.Context, threadID, prompt string) (string, error) {
	raw, err := s.conn.Call(ctx, methodTurnStart, map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": prompt}},
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("turn response decode: %w", err)
	}
	s.mu.Lock()
	s.currentTurnID = resp.Turn.ID
	s.mu.Unlock()
	return resp.Turn.ID, nil
}

// SteerTurn injects mid-turn guidance into the running turn.
func (s *appServerSession) SteerTurn(ctx context.Context, threadID, turnID, text string) error {
	_, err := s.conn.Call(ctx, methodTurnSteer, map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input":          []map[string]any{{"type": "text", "text": text}},
	})
	return err
}

// InterruptTurn stops the running turn.
func (s *appServerSession) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	_, err := s.conn.Call(ctx, methodTurnInterrupt, map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})
	return err
}

// RespondApproval answers a pending approval request by bridge-assigned id.
func (s *appServerSession) RespondApproval(id string, d agentbridge.Decision) error {
	s.mu.Lock()
	ch, ok := s.pendingApprovals[id]
	if ok {
		delete(s.pendingApprovals, id)
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending approval %q", id)
	}
	ch <- d
	return nil
}

// CurrentRefs returns the last thread/turn ids observed via responses and
// notifications — the default targets for voice-driven steer/stop.
func (s *appServerSession) CurrentRefs() (threadID, turnID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentThreadID, s.currentTurnID
}

// handleNotification normalizes app-server notifications into bridge events.
// Delta streams (item/agentMessage/delta, reasoning deltas, ...) are ignored:
// the voice layer narrates completed items, not token streams.
func (s *appServerSession) handleNotification(method string, params json.RawMessage) {
	switch method {
	case notifyThreadStarted:
		var p struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		_ = json.Unmarshal(params, &p)
		s.mu.Lock()
		s.currentThreadID = p.Thread.ID
		s.mu.Unlock()
		s.emit(agentbridge.Event{Type: agentbridge.EventThreadStarted, ThreadID: p.Thread.ID})
	case notifyTurnStarted:
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(params, &p)
		s.mu.Lock()
		s.currentTurnID = p.Turn.ID
		s.mu.Unlock()
		s.emit(agentbridge.Event{Type: agentbridge.EventTurnStarted, ThreadID: p.ThreadID, TurnID: p.Turn.ID})
	case notifyTurnCompleted:
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(params, &p)
		if p.Turn.Error != nil && p.Turn.Error.Message != "" {
			s.emit(agentbridge.Event{Type: agentbridge.EventError, ThreadID: p.ThreadID, TurnID: p.Turn.ID, Err: p.Turn.Error.Message})
			return
		}
		s.emit(agentbridge.Event{Type: agentbridge.EventTurnCompleted, ThreadID: p.ThreadID, TurnID: p.Turn.ID})
	case notifyItemStarted, notifyItemCompleted:
		var p struct {
			ThreadID string         `json:"threadId"`
			TurnID   string         `json:"turnId"`
			Item     map[string]any `json:"item"`
		}
		_ = json.Unmarshal(params, &p)
		t := agentbridge.EventItemStarted
		if method == notifyItemCompleted {
			t = agentbridge.EventItemCompleted
		}
		s.emit(agentbridge.Event{Type: t, ThreadID: p.ThreadID, TurnID: p.TurnID, Item: normalizeItem(p.Item)})
	case notifyError:
		var p struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(params, &p)
		if p.Message == "" {
			p.Message = "app-server error"
		}
		s.emit(agentbridge.Event{Type: agentbridge.EventError, Err: p.Message})
	default:
		// Deltas and unrecognized notifications: intentionally ignored.
	}
}

// handleServerRequest serves server->client requests. Approval requests
// surface as EventApprovalRequested and BLOCK until the host answers via
// RespondApproval (or the session closes -> deny). Anything else is
// method-not-found so Codex applies its own fallback behavior.
func (s *appServerSession) handleServerRequest(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case reqCommandApproval, reqExecApprovalV1:
		return s.blockOnApproval(ctx, "command", params)
	case reqFileChangeApproval, reqApplyPatchV1:
		return s.blockOnApproval(ctx, "patch", params)
	case reqPermissionsApproval:
		return s.blockOnApproval(ctx, "permissions", params)
	default:
		s.logger.Debug("agentbridge: unsupported app-server request", "method", method)
		return nil, &rpcError{Code: rpcCodeMethodNotFound, Message: fmt.Sprintf("method %q not supported by this client", method)}
	}
}

func (s *appServerSession) blockOnApproval(ctx context.Context, kind string, params json.RawMessage) (any, *rpcError) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		ItemID   string `json:"itemId"`
		Command  string `json:"command"`
		Cwd      string `json:"cwd"`
		Reason   string `json:"reason"`
	}
	_ = json.Unmarshal(params, &p)

	ch := make(chan agentbridge.Decision, 1)
	s.mu.Lock()
	s.approvalSeq++
	id := fmt.Sprintf("approval-%d", s.approvalSeq)
	s.pendingApprovals[id] = ch
	s.mu.Unlock()

	summary := p.Reason
	if summary == "" {
		summary = p.Command
	}
	s.emit(agentbridge.Event{
		Type:     agentbridge.EventApprovalRequested,
		ThreadID: p.ThreadID,
		TurnID:   p.TurnID,
		Approval: &agentbridge.ApprovalRequest{ID: id, Kind: kind, Command: p.Command, Summary: summary, Cwd: p.Cwd},
	})
	s.logger.Info("agentbridge: approval requested", "id", id, "kind", kind)

	select {
	case d := <-ch:
		decision := wireDecisionDecline
		if d == agentbridge.DecisionApprove {
			decision = wireDecisionAccept
		}
		s.logger.Info("agentbridge: approval answered", "id", id, "decision", decision)
		return map[string]any{"decision": decision}, nil
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pendingApprovals, id)
		s.mu.Unlock()
		return map[string]any{"decision": wireDecisionDecline}, nil
	}
}
