package main

import (
	"log/slog"
	"time"
)

// desktopCleanupStack registers tear-down callbacks during desktop
// startup and runs them in reverse order when the Wails app exits.
//
// The stack is the single source of truth for shutdown coverage: every
// long-lived resource the desktop client opens (singleton mutex, audio
// capture, voice-agent sessions, overlay windows, tray icons, provider
// HTTP clients, wake-word sidecars, hotkey listeners) must register a
// cleanup callback so a graceful shutdown releases them deterministically.
//
// Close emits a single structured log line — `desktop.shutdown.complete`
// — carrying the total cleanup-call count, the count with explicit
// names, the count of callbacks that panicked, and the total duration
// in milliseconds. Operators reading post-shutdown logs can verify the
// stack ran in full without needing to grep individual entries.
//
// Acceptance: kombify-SpeechKit-hfj.
type desktopCleanupStack struct {
	funcs []cleanupEntry
}

type cleanupEntry struct {
	name string
	fn   func()
}

// Add registers an unnamed cleanup callback. Prefer AddNamed so the
// shutdown audit trail can attribute panics back to a specific
// subsystem; Add stays for backward compatibility with existing
// callsites that don't yet pass a name.
func (s *desktopCleanupStack) Add(fn func()) {
	s.AddNamed("", fn)
}

// AddNamed registers a cleanup callback with a stable subsystem name.
// The name is logged on panic so an operator can map the failure to
// the registering call site without re-running with a debugger.
func (s *desktopCleanupStack) AddNamed(name string, fn func()) {
	if fn == nil {
		return
	}
	s.funcs = append(s.funcs, cleanupEntry{name: name, fn: fn})
}

// Close runs every registered cleanup callback in LIFO order, recovers
// from any panics so a single failing subsystem cannot orphan later
// resources, and emits a single structured `desktop.shutdown.complete`
// log line summarising the run. The stack empties itself so a repeated
// Close is a no-op (`cleanup_calls=0` summary still emitted).
func (s *desktopCleanupStack) Close() {
	start := time.Now()
	total := len(s.funcs)
	namedCount := 0
	errorCount := 0
	for i := total - 1; i >= 0; i-- {
		entry := s.funcs[i]
		if entry.name != "" {
			namedCount++
		}
		errorCount += runCleanupEntry(entry)
	}
	s.funcs = nil
	slog.Info("desktop.shutdown.complete",
		"cleanup_calls", total,
		"named_calls", namedCount,
		"errors", errorCount,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// runCleanupEntry executes one cleanup callback under a defer/recover
// guard. The named return is set to 1 when a panic is recovered and 0
// on clean exit, giving Close a simple aggregate count for the
// shutdown summary log line.
func runCleanupEntry(entry cleanupEntry) (panics int) {
	defer func() {
		if r := recover(); r != nil {
			panics = 1
			slog.Error("desktop.shutdown.entry_panic",
				"name", entry.name,
				"panic", r,
			)
		}
	}()
	entry.fn()
	return 0
}
