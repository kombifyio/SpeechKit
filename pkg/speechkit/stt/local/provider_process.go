package local

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/procguard"
)

// StartServer starts the whisper.cpp server subprocess. Blocks until ready or context cancelled.
func (p *Provider) StartServer(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("whisper-server process context is required")
	}
	generation, startDone, err := p.beginProcessStart()
	if err != nil {
		return err
	}
	defer p.finishProcessStart(generation, startDone)

	binaryPath, err := findWhisperBinary()
	if err != nil {
		return fmt.Errorf("whisper binary: %w", err)
	}

	if err := ValidateModelPath(p.ModelPath); err != nil {
		return fmt.Errorf("validate whisper model path: %w", err)
	}
	if _, err := os.Stat(p.ModelPath); err != nil {
		return fmt.Errorf("model not found: %s", p.ModelPath)
	}
	if err := verifyReadableModelFile(p.ModelPath); err != nil {
		return fmt.Errorf("model not readable: %s: %w", p.ModelPath, err)
	}
	if err := p.processStartCanceled(ctx, generation); err != nil {
		return err
	}

	threads := defaultWhisperThreads()
	modelArg, workDir := whisperModelArgument(p.ModelPath)
	args := []string{
		"--model", modelArg,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", p.Port),
		"--threads", strconv.Itoa(threads),
		"--inference-path", "/v1/audio/transcriptions",
	}
	// whisper.cpp uses GPU by default; only pass --no-gpu when explicitly disabled.
	// "auto" and "" mean let whisper.cpp decide (default behavior).
	if p.GPU == "cpu" {
		args = append(args, "--no-gpu")
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...) // #nosec G204 -- binaryPath is resolved by findWhisperBinary from bundle/managed locations or explicit dev opt-in.
	cmd.Dir = workDir
	configureHiddenProcess(cmd, subprocessPriorityLowered(p.LowerSubprocessPriority))
	// Must run before Start: on POSIX hosts this is what puts the child in
	// its own process group, and after the child has exec'd the parent can
	// no longer move it (see procguard.Prepare).
	procguard.Prepare(cmd)
	cmd.Stdout = os.Stderr // whisper-server logs to stdout
	cmd.Stderr = os.Stderr

	// gpu_mode is the *requested* mode; whether inference actually runs on a
	// GPU depends on how the bundled whisper-server was built (a CPU-only
	// build silently ignores "auto"/"cuda" and transcription scales with
	// audio length). whisper-server's own startup banner on stderr names the
	// backend it actually initialised — check it when latency looks CPU-bound.
	slog.Info("starting whisper-server", "binary", binaryPath, "args", args, "dir", workDir, "threads", threads, "gpu_mode", p.GPU)
	if modelArg == p.ModelPath && !isASCII(modelArg) {
		slog.Warn("whisper-server model path contains non-ASCII characters that cannot be passed through argv safely; move the model to an ASCII-only path if startup fails", "model", p.ModelPath)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start whisper-server: %w", err)
	}
	// Hand the child to the OS so it cannot outlive this process when the
	// host dies without running its cleanup path (crash, taskkill, dev-loop
	// rebuild). Assignment failing does not make the child unusable.
	if err := procguard.Adopt(cmd); err != nil {
		slog.Warn("whisper-server not adopted into the kill-on-exit job", "error", err, "pid", cmd.Process.Pid)
	}
	processDone := make(chan struct{})
	p.processMu.Lock()
	if p.generation != generation {
		p.processMu.Unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("whisper-server startup generation changed unexpectedly")
	}
	p.cmd = cmd
	p.processDone = processDone
	p.processErr = nil
	stopAfterPublish := p.stopRequested || ctx.Err() != nil
	p.processMu.Unlock()
	go p.waitForProcess(ctx, cmd, processDone, generation)
	if stopAfterPublish {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server startup was stopped")
	}

	if err := p.waitForReady(ctx); err != nil {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server health probe never returned ready: %w", err)
	}
	if err := p.waitForInferenceReady(ctx); err != nil {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server inference probe never returned ready: %w", err)
	}

	if err := p.markProcessReady(generation, processDone); err != nil {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server stopped before readiness: %w", err)
	}
	slog.Info("whisper-server ready", "url", p.BaseURL)
	return nil
}

func (p *Provider) beginProcessStart() (uint64, chan struct{}, error) {
	done := make(chan struct{})
	p.processMu.Lock()
	defer p.processMu.Unlock()
	switch {
	case p.stopping:
		return 0, nil, fmt.Errorf("whisper-server is stopping")
	case p.starting || p.cmd != nil:
		return 0, nil, fmt.Errorf("whisper-server is already starting or running")
	}
	p.generation++
	p.starting = true
	p.stopRequested = false
	p.processDone = nil
	p.processErr = nil
	p.startDone = done
	p.ready.Store(false)
	return p.generation, done, nil
}

func (p *Provider) finishProcessStart(generation uint64, done chan struct{}) {
	p.processMu.Lock()
	if p.generation == generation {
		p.starting = false
	}
	close(done)
	p.processMu.Unlock()
}

func (p *Provider) processStartCanceled(ctx context.Context, generation uint64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("whisper-server startup canceled: %w", err)
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.generation != generation || p.stopRequested || p.stopping {
		return fmt.Errorf("whisper-server startup was stopped")
	}
	return nil
}

func (p *Provider) markProcessReady(generation uint64, done chan struct{}) error {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.generation != generation || p.processDone != done {
		return fmt.Errorf("whisper-server startup generation is no longer active")
	}
	select {
	case <-done:
		if p.processErr != nil {
			return p.processErr
		}
		return fmt.Errorf("whisper-server stopped")
	default:
	}
	if p.stopRequested || p.stopping {
		return fmt.Errorf("whisper-server startup was stopped")
	}
	p.ready.Store(true)
	return nil
}

func (p *Provider) waitForProcess(ctx context.Context, cmd *exec.Cmd, done chan struct{}, generation uint64) {
	waitErr := cmd.Wait()
	p.recordProcessExit(ctx, cmd, done, generation, waitErr)
}

func (p *Provider) recordProcessExit(ctx context.Context, cmd *exec.Cmd, done chan struct{}, generation uint64, waitErr error) {
	p.processMu.Lock()
	current := p.generation == generation && p.cmd == cmd && p.processDone == done
	expected := !current || p.stopRequested || ctx.Err() != nil
	if current {
		p.ready.Store(false)
		p.cmd = nil
		if expected {
			p.processErr = nil
		} else if waitErr != nil {
			if cause := describeProcessExit(waitErr); cause != "" {
				p.processErr = fmt.Errorf("whisper-server exited unexpectedly (%s): %w", cause, waitErr)
			} else {
				p.processErr = fmt.Errorf("whisper-server exited unexpectedly: %w", waitErr)
			}
		} else {
			p.processErr = fmt.Errorf("whisper-server exited unexpectedly")
		}
	}
	close(done)
	p.processMu.Unlock()
}

func (p *Provider) stopStartedProcess(generation uint64, cmd *exec.Cmd, done <-chan struct{}) {
	p.processMu.Lock()
	if p.generation == generation {
		p.stopRequested = true
	}
	p.processMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if done != nil {
		<-done
	}
}

func defaultWhisperThreads() int {
	if raw := strings.TrimSpace(os.Getenv("SPEECHKIT_WHISPER_THREADS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

// StopServer terminates the whisper-server subprocess.
func (p *Provider) StopServer() {
	p.stopMu.Lock()
	defer p.stopMu.Unlock()
	cmd, processDone, startDone, starting := p.beginProcessStop()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if processDone != nil {
		<-processDone
	}
	if starting && startDone != nil {
		<-startDone
	}
	p.finishProcessStop()
}

func (p *Provider) beginProcessStop() (*exec.Cmd, <-chan struct{}, <-chan struct{}, bool) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.stopping = true
	p.stopRequested = true
	p.ready.Store(false)
	cmd := p.cmd
	processDone := p.processDone
	startDone := p.startDone
	starting := p.starting
	return cmd, processDone, startDone, starting
}

func (p *Provider) finishProcessStop() {
	p.processMu.Lock()
	p.stopping = false
	p.processMu.Unlock()
}

// RuntimeDone closes whenever the owned whisper-server subprocess exits.
// RuntimeError then reports an unexpected exit and stays nil for an explicit
// StopServer call or process-context cancellation.
func (p *Provider) RuntimeDone() <-chan struct{} {
	if p == nil {
		return nil
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	return p.processDone
}

// RuntimeError returns why the current whisper-server child exited, once
// [Provider.RuntimeDone] has closed. It is nil while the child runs, after an
// explicit [Provider.StopServer], when the process context was cancelled, or
// on a nil receiver.
func (p *Provider) RuntimeError() error {
	if p == nil {
		return nil
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	return p.processErr
}

func (p *Provider) runtimeExitError() error {
	done := p.RuntimeDone()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		if err := p.RuntimeError(); err != nil {
			return err
		}
		return fmt.Errorf("whisper-server stopped")
	default:
		return nil
	}
}
