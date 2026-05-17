package wakeword

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// HotkeyEvent is the minimal shape required to bridge a wake DetectionEvent
// into the existing mode-hotkey dispatch channel. It intentionally mirrors
// the Type+Binding pair used by internal/hotkey.Event without importing
// that package, so the kernel module stays platform-neutral.
type HotkeyEvent struct {
	KeyDown bool   // true = press, false = release
	Binding string // mode name: "dictate" | "assist" | "voice_agent"
}

// HotkeySink consumes synthetic key events. The Device-Target supplies an
// adapter that pushes into the modeHotkeyManager events channel.
type HotkeySink interface {
	Submit(HotkeyEvent)
}

// HotkeySinkFunc adapts a function to HotkeySink.
type HotkeySinkFunc func(HotkeyEvent)

// Submit calls f.
func (f HotkeySinkFunc) Submit(ev HotkeyEvent) { f(ev) }

// Dispatcher converts a DetectionEvent into a synthetic KeyDown event on
// the configured DefaultMode binding. By default it emits *only* a press
// (no release), which the downstream mode controller interprets as a
// session-start request in either PTT or Toggle behaviour:
//
//   - Toggle: first press starts the session; the user toggles it off
//     later via the actual hotkey or it auto-stops on silence.
//   - PTT: the press starts the session; auto-stop on silence (see
//     desktopAudioLevelHandler.FastModeSilenceMs) closes it. Wake-trigger
//     plus PTT works best with FastModeSilenceMs set, otherwise the user
//     must press the actual hotkey to release.
//
// SyntheticRelease can be enabled if a downstream consumer specifically
// requires a balanced press+release pair; the release fires after ReleaseAfter.
type Dispatcher struct {
	sink             HotkeySink
	logger           *slog.Logger
	syntheticRelease bool
	releaseAfter     time.Duration

	mu       sync.Mutex
	closed   bool
	inFlight sync.WaitGroup
}

// DispatcherOptions tweaks Dispatcher behaviour.
type DispatcherOptions struct {
	// SyntheticRelease causes the dispatcher to emit a KeyUp event
	// ReleaseAfter the KeyDown. Off by default — most desktop consumers
	// want toggle/auto-stop semantics rather than a fast tap.
	SyntheticRelease bool

	// ReleaseAfter is the delay between synthesized KeyDown and KeyUp
	// when SyntheticRelease is true. Defaults to 150ms.
	ReleaseAfter time.Duration

	// Logger is used for structured logs. Defaults to slog.Default().
	Logger *slog.Logger
}

// NewDispatcher constructs a Dispatcher with the given sink and options.
func NewDispatcher(sink HotkeySink, opts DispatcherOptions) *Dispatcher {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.ReleaseAfter <= 0 {
		opts.ReleaseAfter = 150 * time.Millisecond
	}
	return &Dispatcher{
		sink:             sink,
		logger:           opts.Logger,
		syntheticRelease: opts.SyntheticRelease,
		releaseAfter:     opts.ReleaseAfter,
	}
}

// Emit implements Sink. Called by the wake pipeline when a phrase fires.
// Non-blocking: actual sink submission runs in a goroutine so a slow
// downstream channel cannot stall PCM ingestion.
func (d *Dispatcher) Emit(ev DetectionEvent) {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	if ev.Mode == "" {
		d.mu.Unlock()
		d.logger.Warn("wake event without mode; ignoring", "phrase", ev.Phrase)
		return
	}
	d.inFlight.Add(1)
	d.mu.Unlock()

	d.logger.Info("wakeword fired",
		"phrase", ev.Phrase,
		"mode", ev.Mode,
		"probability", ev.Probability)

	go d.dispatch(ev)
}

// Close blocks further events and waits for in-flight dispatch goroutines
// (or until ctx is cancelled).
func (d *Dispatcher) Close(ctx context.Context) error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	done := make(chan struct{})
	go func() {
		d.inFlight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Dispatcher) dispatch(ev DetectionEvent) {
	defer d.inFlight.Done()
	d.sink.Submit(HotkeyEvent{KeyDown: true, Binding: ev.Mode})
	if !d.syntheticRelease {
		return
	}
	// Close() waits for in-flight dispatches via inFlight; an in-flight
	// goroutine therefore always completes its press/release pair even if
	// Close arrived in the middle. We do not re-check d.closed here on
	// purpose — that would let Close swallow the release half of a pair
	// and leave downstream state thinking a key is still held.
	time.Sleep(d.releaseAfter)
	d.sink.Submit(HotkeyEvent{KeyDown: false, Binding: ev.Mode})
}
