package codex

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentbridge"
)

// execTurn is one running `codex exec --json` invocation. Exec mode has no
// mid-turn steer; Interrupt kills the process.
type execTurn struct {
	cmd  *exec.Cmd
	done chan struct{}

	mu       sync.Mutex
	threadID string
}

// startExecTurn spawns the codex binary for one turn and streams normalized
// events into emit. It returns once the process is started (fast-ack); the
// turn continues in background goroutines until the process exits.
func startExecTurn(ctx context.Context, binary string, req agentbridge.TurnRequest, emit func(agentbridge.Event)) (*execTurn, error) {
	// Exec mode runs without any approval round-trip, so it only ever gets
	// the read-only sandbox; side-effectful turns need app-server mode.
	if req.Sandbox != agentbridge.SandboxReadOnly {
		return nil, fmt.Errorf("%w: exec mode has no approvals, so only the read-only sandbox is allowed (requested %q)", agentbridge.ErrUnsupported, req.Sandbox)
	}
	args := []string{"exec", "--json", "--sandbox", string(req.Sandbox)}
	if threadID := strings.TrimSpace(req.ThreadID); threadID != "" {
		if !validThreadID(threadID) {
			return nil, fmt.Errorf("codex exec: refusing malformed thread id")
		}
		args = append(args, "resume", threadID)
	}
	// The prompt is model-authored text. It never goes on the command line:
	// "-" makes codex read it from stdin, so neither option parsing nor a
	// script host's quoting (cmd.exe for a .cmd shim) can interpret it.
	args = append(args, "-")

	// The working directory IS the project allowlist enforcement point for
	// exec mode: the caller passes only allowlisted paths (policy lives in
	// the host/voicetools layer, validated again by app/internal/config).
	cmd := exec.CommandContext(ctx, binary, args...) // #nosec G204 -- binary comes from resolveBinary (absolute, .exe-only on Windows); args are fixed flags plus a validated thread id, and the prompt travels on stdin.
	cmd.Dir = req.Project.Path
	cmd.Env = codexEnv(binary)
	cmd.Stdin = strings.NewReader(req.Prompt)
	configureSysProcAttr(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex exec stdout pipe: %w", err)
	}
	var stderrTail strings.Builder
	cmd.Stderr = &boundedWriter{max: 4096, b: &stderrTail}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex exec start: %w", err)
	}

	turn := &execTurn{cmd: cmd, done: make(chan struct{})}

	go func() {
		defer close(turn.done)
		sawCompletion := false
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			ev, ok := parseExecEvent(scanner.Bytes())
			if !ok {
				continue
			}
			if ev.Type == agentbridge.EventThreadStarted && ev.ThreadID != "" {
				turn.mu.Lock()
				turn.threadID = ev.ThreadID
				turn.mu.Unlock()
			}
			if ev.Type == agentbridge.EventTurnCompleted || ev.Type == agentbridge.EventError {
				sawCompletion = true
			}
			emit(ev)
		}
		err := cmd.Wait()
		if err != nil && !sawCompletion {
			msg := strings.TrimSpace(stderrTail.String())
			if msg == "" {
				msg = err.Error()
			}
			emit(agentbridge.Event{Type: agentbridge.EventError, ThreadID: turn.ThreadID(), Err: msg})
		}
	}()

	return turn, nil
}

// validThreadID accepts the identifier shapes codex reports (UUIDs and
// similar token strings) and nothing that could be read as an option.
func validThreadID(id string) bool {
	if id == "" || len(id) > 128 || id[0] == '-' {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// ThreadID returns the thread id reported by the running turn, if any yet.
func (t *execTurn) ThreadID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.threadID
}

// interrupt kills the exec process — the only stop signal exec mode has.
func (t *execTurn) interrupt() error {
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Kill()
	}
	return nil
}

// wait blocks until the turn's pump goroutine finished.
func (t *execTurn) wait() { <-t.done }

// boundedWriter keeps only the first max bytes — enough stderr context for an
// error message without unbounded growth.
type boundedWriter struct {
	max int
	b   *strings.Builder
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if w.b.Len() < w.max {
		room := w.max - w.b.Len()
		if room > len(p) {
			room = len(p)
		}
		w.b.Write(p[:room])
	}
	return len(p), nil
}
