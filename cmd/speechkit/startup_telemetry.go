package main

import (
	"log/slog"
	"sync"
	"time"
)

// startupTracker emits structured "desktop.startup" log events with monotonic
// elapsed_ms timing so the full bootstrap sequence can be reconstructed from
// the log file when diagnosing single-instance or window-lifecycle issues.
type startupTracker struct {
	start time.Time
	mu    sync.Mutex
	last  time.Time
}

func newStartupTracker() *startupTracker {
	now := time.Now()
	return &startupTracker{start: now, last: now}
}

// stage logs a startup milestone. elapsed_ms is total time since tracker
// creation; delta_ms is time since the previous stage call so slow steps
// stand out when scanning logs.
func (t *startupTracker) stage(name string, attrs ...any) {
	if t == nil {
		return
	}
	t.mu.Lock()
	now := time.Now()
	elapsed := now.Sub(t.start)
	delta := now.Sub(t.last)
	t.last = now
	t.mu.Unlock()

	logAttrs := append([]any{
		"stage", name,
		"elapsed_ms", elapsed.Milliseconds(),
		"delta_ms", delta.Milliseconds(),
	}, attrs...)
	slog.Info("desktop.startup", logAttrs...)
}
