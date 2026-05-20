package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
)

// SpeechKit's Windows reference adapter wires the platform-neutral
// wakeword kernel (internal/wakeword) into the desktop's hotkey bus via a
// **sidecar process**. The sidecar binary speechkit-wakeword.exe is
// generated from cmd/speechkit-wakeword and ships next to SpeechKit.exe.
//
// Wake-word is a CLIENT concern — this file is the demo of how a client
// embeds the framework module. Other client targets (Local-Target Go CLI,
// Android via gomobile, future iOS/Web bindings) bring their own adapter;
// they all consume the same internal/wakeword kernel.
//
// Why a sidecar: github.com/k2-fsa/sherpa-onnx-go's native init races with
// the Wails-alpha runtime when both live in the same process and aborts
// the host. Running the spotter in a sibling process isolates the failure
// and mirrors the architecture LiveKit uses for livekit-wakeword. A
// sidecar crash never takes the main desktop app down.

const (
	defaultWakewordModelDirName  = "wakeword-kws"
	defaultWakewordSidecarBinary = "speechkit-wakeword.exe"
)

// desktopWakeRuntime owns the spawned sidecar process and the goroutine
// that pumps its stdout/stderr. Close() asks the sidecar to shut down
// gracefully via stdin and falls back to Kill on timeout.
type desktopWakeRuntime struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stdin  io.WriteCloser
	mu     sync.Mutex
	closed bool
	doneCh chan struct{}
}

// Close sends a shutdown command on stdin, waits briefly, then kills if
// the process is still alive. Idempotent — safe to call multiple times.
func (r *desktopWakeRuntime) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	stdin := r.stdin
	cmd := r.cmd
	cancel := r.cancel
	done := r.doneCh
	r.mu.Unlock()

	if stdin != nil {
		_, _ = stdin.Write([]byte(`{"type":"shutdown"}` + "\n"))
		_ = stdin.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		}
	}
	if cancel != nil {
		cancel()
	}
}

// startDesktopWakeword spawns the sidecar process when wake-word is
// enabled, parses its stdout JSON event stream, and forwards detection
// events to the desktop's hotkey bus. Returns nil with no error when the
// feature is disabled or the assets are missing — wake-word must never
// block desktop startup.
func startDesktopWakeword(ctx context.Context, cfg *config.Config, state *appState, hkManager *modeHotkeyManager, cleanup *desktopCleanupStack) (rt *desktopWakeRuntime) {
	if cleanup != nil {
		cleanup.Add(func() {
			if rt := state.takeWakewordRuntime(); rt != nil {
				rt.Close()
			}
		})
	}

	defer func() {
		if r := recover(); r != nil {
			slog.Error("wakeword: spawn panicked — disabling feature", "panic", r)
			state.addLog(fmt.Sprintf("Wake-word disabled (spawn panic): %v", r), "error")
			state.setWakewordStatus(fmt.Sprintf("Disabled (spawn panic): %v", r))
			rt = nil
		}
	}()

	if cfg == nil || !cfg.Wakeword.Enabled {
		state.setWakewordStatus("Wake-word disabled")
		return nil
	}
	if hkManager == nil {
		slog.Warn("wakeword: skipped — no hotkey manager available")
		state.setWakewordStatus("Wake-word unavailable: hotkey manager missing")
		return nil
	}

	resolved, err := resolveWakeword(cfg)
	if err != nil {
		slog.Warn("wakeword: disabled — assets missing", "err", err)
		state.addLog(fmt.Sprintf("Wake-word disabled: %v", err), "warn")
		state.setWakewordStatus(fmt.Sprintf("Disabled: %v", err))
		return nil
	}

	runtime, err := launchWakewordSidecar(ctx, state, hkManager, cfg, resolved)
	if err != nil {
		slog.Warn("wakeword: sidecar launch failed", "err", err)
		state.addLog(fmt.Sprintf("Wake-word disabled: %v", err), "warn")
		state.setWakewordStatus(fmt.Sprintf("Disabled: %v", err))
		return nil
	}

	state.setWakewordRuntime(runtime)
	state.setWakewordStatus(fmt.Sprintf("Listening for \"%s\" → %s (sidecar PID %d)", resolved.DisplayPhrase, resolved.DefaultMode, runtime.cmd.Process.Pid))
	if state.appTray != nil {
		state.appTray.SetWakeListening(true)
	}
	state.addLog(fmt.Sprintf("Wake-word ready: \"%s\" → %s", resolved.DisplayPhrase, resolved.DefaultMode), "info")
	return runtime
}

// execCommandContext is the injection point for exec.CommandContext used by
// launchWakewordSidecar. Production code uses the stdlib function; tests
// replace it with a recorder to assert the sidecar exec ctx is decoupled
// from any short-lived caller context.
var execCommandContext = exec.CommandContext

// sidecarParentContext returns the base context the sidecar's
// exec.CommandContext is rooted in. It is intentionally context.Background()
// so that no HTTP-request-scoped or otherwise short-lived caller ctx can
// cancel the long-lived sidecar — see launchWakewordSidecar for the bug
// history (v0.35.6 "Wake-word sidecar exited (code 1)" toast).
func sidecarParentContext() context.Context { return context.Background() }

// launchWakewordSidecar starts the sidecar process, sets up the stdout
// event pump and stderr log fan-out, and returns the runtime handle.
//
// The sidecar's exec context is deliberately rooted in sidecarParentContext()
// (a fresh Background) and NOT in the caller's ctx. The sidecar's lifetime is
// owned by desktopWakeRuntime.Close (stdin shutdown + Kill fallback) and by
// the cleanup stack registered at app startup — not by callers.
//
// Until v0.35.6 this used the caller's parentCtx, which silently broke every
// runtime path that flowed restartDesktopWakeword(r.Context(), ...) — i.e. the
// "Enable wake-word" wizard button and every Settings save that touched a
// wake-word field. Those callers pass an HTTP-request context that cancels
// the instant the response is written, so exec.CommandContext killed the
// freshly-launched sidecar via Windows TerminateProcess(handle, 1). The
// user-visible symptom was the toast pair
//
//	Wake-word ready: "<phrase>" → <mode>
//	Wake-word sidecar exited (code 1): exit status 1
//
// emitted in the same second. Background() severs the link.
func launchWakewordSidecar(_ context.Context, state *appState, hkManager *modeHotkeyManager, cfg *config.Config, resolved resolvedWakewordAssets) (*desktopWakeRuntime, error) {
	ctx, cancel := context.WithCancel(sidecarParentContext())
	cmd := execCommandContext(ctx, resolved.SidecarPath,
		"--model-dir", resolved.ModelDir,
		"--keywords-file", resolved.KeywordsFile,
		"--phrase-id", strings.TrimSpace(cfg.Wakeword.PhraseID),
		"--phrase", resolved.DisplayPhrase,
		"--default-mode", resolved.DefaultMode,
		"--threshold", strconv.FormatFloat(float64(cfg.Wakeword.Threshold), 'f', -1, 32),
		"--min-consecutive-frames", strconv.Itoa(maxInt(cfg.Wakeword.MinConsecutiveFrames, 1)),
		"--cooldown-ms", strconv.Itoa(maxInt(cfg.Wakeword.CooldownMs, 1500)),
		"--audio-backend", string(cfg.Audio.Backend),
		"--audio-device-id", cfg.Audio.DeviceID,
	)
	configureHiddenProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return nil, fmt.Errorf("start sidecar: %w", err)
	}

	rt := &desktopWakeRuntime{
		cmd:    cmd,
		cancel: cancel,
		stdin:  stdin,
		doneCh: make(chan struct{}),
	}

	go pumpSidecarStderr(stderr, state)
	go pumpSidecarStdout(stdout, state, hkManager, resolved)
	go func() {
		err := cmd.Wait()
		close(rt.doneCh)
		rt.mu.Lock()
		alreadyClosed := rt.closed
		rt.mu.Unlock()
		if alreadyClosed {
			return
		}
		// Unexpected exit — surface the cause and clear the runtime so the
		// settings UI can show the sidecar is offline. Auto-restart is not
		// implemented in this first iteration; it's tracked as a follow-up
		// (the harness here makes it a small addition).
		slog.Warn("wakeword: sidecar exited unexpectedly", "err", err, "exit_code", cmd.ProcessState.ExitCode())
		state.addLog(fmt.Sprintf("Wake-word sidecar exited (code %d): %v", cmd.ProcessState.ExitCode(), err), "warn")
		state.setWakewordStatus(fmt.Sprintf("Wake-word offline (sidecar exited code %d)", cmd.ProcessState.ExitCode()))
		if state.appTray != nil {
			state.appTray.SetWakeListening(false)
		}
		if rt := state.takeWakewordRuntime(); rt != nil && rt.cmd == cmd {
			// nothing to do — already taken off the state field
		}
	}()

	return rt, nil
}

// pumpSidecarStdout reads one JSON event per line and dispatches based on
// the type discriminator. Unknown event types are ignored so future
// sidecar versions remain compatible with this adapter.
func pumpSidecarStdout(stdout io.Reader, state *appState, hkManager *modeHotkeyManager, resolved resolvedWakewordAssets) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev sidecarEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			slog.Warn("wakeword: invalid sidecar event", "err", err, "line", line)
			continue
		}
		handleSidecarEvent(ev, state, hkManager, resolved)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		slog.Debug("wakeword: stdout scanner exit", "err", err)
	}
}

// pumpSidecarStderr fans the sidecar's structured slog stream into the
// main app's log feed at INFO level — useful for the user-facing status
// view because the sidecar logs which audio device opened and any sherpa
// runtime warnings.
func pumpSidecarStderr(stderr io.Reader, state *appState) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err == nil {
			level, _ := rec["level"].(string)
			msg, _ := rec["msg"].(string)
			if level == "ERROR" || level == "error" {
				state.addLog(fmt.Sprintf("wakeword-sidecar: %s", msg), "error")
			} else if level == "WARN" || level == "warn" {
				state.addLog(fmt.Sprintf("wakeword-sidecar: %s", msg), "warn")
			} else {
				slog.Debug("wakeword-sidecar", "line", line)
			}
			continue
		}
		slog.Debug("wakeword-sidecar", "raw", line)
	}
}

func handleSidecarEvent(ev sidecarEvent, state *appState, hkManager *modeHotkeyManager, resolved resolvedWakewordAssets) {
	switch ev.Type {
	case "ready":
		slog.Info("wakeword: sidecar ready", "phrase", ev.Phrase, "mode", ev.Mode, "backend", ev.Backend, "version", ev.Version)
		state.addLog(fmt.Sprintf("Wake-word listening for \"%s\" → %s", ev.Phrase, ev.Mode), "info")
	case "detection":
		mode := strings.TrimSpace(ev.Mode)
		if mode == "" {
			mode = resolved.DefaultMode
		}
		slog.Info("wakeword: detection", "keyword", ev.Keyword, "phrase", ev.Phrase, "mode", mode)
		state.addLog(fmt.Sprintf("Wake-word fired: \"%s\" → %s", ev.Phrase, mode), "info")

		// Build a per-detection AutoEndPolicy from the live wake-word
		// config and park it in the appState slot so routeVoiceAgentHotkey
		// can claim it when the synthesized KeyDown arrives. We do this
		// BEFORE submitting the event so the input-controller cannot race
		// us and look for the policy in an empty slot.
		policy := wakeword.NewAutoEndPolicy(autoEndConfigFromTOML(resolved.AutoEnd), slog.Default())
		state.setWakewordSessionPolicy(policy)

		hkManager.Submit(hotkey.Event{
			Type:    hotkey.EventKeyDown,
			Binding: mode,
			Source:  hotkey.EventSourceWakeword,
		})
	case "heartbeat":
		slog.Info("wakeword: heartbeat",
			"bytes_in_30s", ev.BytesIn,
			"decodes_30s", ev.DecodesIn,
			"uptime_s", ev.UptimeSec)
	case "log":
		// Already fanned via stderr — duplicate slog only at debug level.
		slog.Debug("wakeword: sidecar log", "level", ev.Level, "msg", ev.Msg)
	case "error":
		slog.Warn("wakeword: sidecar error", "msg", ev.Msg)
		state.addLog(fmt.Sprintf("Wake-word sidecar error: %s", ev.Msg), "warn")
	case "shutdown":
		slog.Info("wakeword: sidecar shutting down on command")
	default:
		slog.Debug("wakeword: unknown sidecar event", "type", ev.Type)
	}
}

// sidecarEvent mirrors cmd/speechkit-wakeword/ipc.go Event with `omitempty`
// fields. Decoded with map-style fields so the adapter compiles even when
// the sidecar adds new optional fields ahead of an adapter update.
type sidecarEvent struct {
	Type        string    `json:"type"`
	At          time.Time `json:"at"`
	Version     string    `json:"version,omitempty"`
	Backend     string    `json:"backend,omitempty"`
	PhraseID    string    `json:"phraseId,omitempty"`
	Keyword     string    `json:"keyword,omitempty"`
	Probability float32   `json:"probability,omitempty"`
	Phrase      string    `json:"phrase,omitempty"`
	Mode        string    `json:"mode,omitempty"`
	Threshold   float32   `json:"threshold,omitempty"`
	BytesIn     int64     `json:"bytesIn,omitempty"`
	DecodesIn   int64     `json:"decodesIn,omitempty"`
	UptimeSec   int64     `json:"uptimeSec,omitempty"`
	Level       string    `json:"level,omitempty"`
	Msg         string    `json:"msg,omitempty"`
}

// restartDesktopWakeword stops the current sidecar (if any) and spawns a
// fresh one against cfg. Used by the Settings save path after WakewordConfig
// fields change. Returns the new runtime or nil if disabled / failed.
func restartDesktopWakeword(ctx context.Context, cfg *config.Config, state *appState, hkManager *modeHotkeyManager) *desktopWakeRuntime {
	if rt := state.takeWakewordRuntime(); rt != nil {
		rt.Close()
	}
	return startDesktopWakeword(ctx, cfg, state, hkManager, nil)
}

// wakewordConfigEquals reports whether two wake-word configs share every
// field that affects the running sidecar. Used by the Settings apply path
// to decide whether a restart is needed.
func wakewordConfigEquals(a, b config.WakewordConfig) bool {
	return a.Enabled == b.Enabled &&
		a.PhraseID == b.PhraseID &&
		a.Phrase == b.Phrase &&
		a.ModelPath == b.ModelPath &&
		a.MelspecModelPath == b.MelspecModelPath &&
		a.EmbeddingModelPath == b.EmbeddingModelPath &&
		a.DefaultMode == b.DefaultMode &&
		a.Threshold == b.Threshold &&
		a.MinConsecutiveFrames == b.MinConsecutiveFrames &&
		a.CooldownMs == b.CooldownMs
}

// resolvedWakewordAssets bundles every on-disk input the sidecar needs,
// plus the resolved auto-end config the desktop adapter uses when wiring
// session auto-end on a detection.
type resolvedWakewordAssets struct {
	SidecarPath   string
	ModelDir      string
	KeywordsFile  string
	DefaultMode   string
	DisplayPhrase string

	// AutoEnd is a copy of cfg.Wakeword.AutoEnd captured at sidecar-launch
	// time. Held here so handleSidecarEvent can build a fresh policy per
	// detection without re-reading the live config (which may have been
	// edited concurrently in the settings UI).
	AutoEnd config.WakewordAutoEndConfig
}

func resolveWakeword(cfg *config.Config) (resolvedWakewordAssets, error) {
	var out resolvedWakewordAssets

	exe, err := os.Executable()
	if err != nil {
		return out, fmt.Errorf("resolve executable path: %w", err)
	}
	exeDir := filepath.Dir(exe)

	out.SidecarPath = filepath.Join(exeDir, defaultWakewordSidecarBinary)
	out.ModelDir = filepath.Join(exeDir, defaultWakewordModelDirName)
	if v := strings.TrimSpace(cfg.Wakeword.ModelPath); v != "" && strings.HasSuffix(strings.ToLower(v), ".txt") {
		out.KeywordsFile = v
	} else {
		out.KeywordsFile = filepath.Join(out.ModelDir, "keywords.txt")
	}

	for label, p := range map[string]string{
		"sidecar":  out.SidecarPath,
		"modelDir": out.ModelDir,
		"keywords": out.KeywordsFile,
	} {
		if !pathIsRegularFileOrDir(p) {
			return resolvedWakewordAssets{}, fmt.Errorf("wake-word %s asset missing at %s (rebuild the SpeechKit bundle via scripts/build.ps1)", label, p)
		}
	}

	out.DefaultMode = config.NormalizeWakewordDefaultMode(cfg.Wakeword.DefaultMode)
	out.AutoEnd = cfg.Wakeword.AutoEnd
	out.DisplayPhrase = strings.TrimSpace(cfg.Wakeword.Phrase)
	if id := strings.TrimSpace(cfg.Wakeword.PhraseID); id != "" {
		if entry := wakeword.LookupPhrase(id); entry != nil && out.DisplayPhrase == "" {
			out.DisplayPhrase = entry.DisplayName
		}
	}
	if out.DisplayPhrase == "" {
		out.DisplayPhrase = "Wake phrase"
	}
	return out, nil
}

func pathIsRegularFileOrDir(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// autoEndConfigFromTOML converts the TOML surface (whole seconds + raw
// phrase slice) into the kernel's AutoEndConfig (time.Duration). A
// zero-value input is returned as-is — wakeword.NewAutoEndPolicy then
// substitutes DefaultAutoEndConfig so a fresh install gets the framework
// baseline without the client adapter needing to know what the baseline
// numbers are.
func autoEndConfigFromTOML(in config.WakewordAutoEndConfig) wakeword.AutoEndConfig {
	out := wakeword.AutoEndConfig{}
	if in.SilenceCutoffSec > 0 {
		out.SilenceCutoff = time.Duration(in.SilenceCutoffSec) * time.Second
	}
	if len(in.ExitPhrases) > 0 {
		out.ExitPhrases = append([]string(nil), in.ExitPhrases...)
	}
	return out
}
