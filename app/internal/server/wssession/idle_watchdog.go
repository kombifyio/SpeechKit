package wssession

import (
	"sync"
	"time"
)

// IdleWatchdog signals on its Fired channel when the configured timeout
// elapses without a Reset call. Designed to be embedded in a WebSocket
// adapter so server-side idle sessions are torn down without waiting for
// the client to disconnect.
//
// Lifecycle:
//
//	w := NewIdleWatchdog(15 * time.Minute)
//	defer w.Stop()
//	for {
//	    select {
//	    case <-frameArrived:
//	        w.Reset()
//	    case <-w.Fired():
//	        // close session with reason="idle"
//	    }
//	}
//
// Safe for concurrent Reset/Stop. A timeout of 0 returns a watchdog whose
// Fired channel never fires — useful for tests and for callers who want
// to disable the timeout via config.
type IdleWatchdog struct {
	mu      sync.Mutex
	timer   *time.Timer
	fired   chan struct{}
	timeout time.Duration
	stopped bool
	paused  bool
	epoch   uint64
}

// NewIdleWatchdog constructs a watchdog with the given timeout.
func NewIdleWatchdog(timeout time.Duration) *IdleWatchdog {
	w := &IdleWatchdog{
		fired:   make(chan struct{}, 1),
		timeout: timeout,
	}
	if timeout <= 0 {
		// No-op watchdog: Fired() returns a channel that never fires.
		return w
	}
	w.timer = time.AfterFunc(timeout, func() { w.onFire(0) })
	return w
}

// Reset restarts the timeout. No-op when the watchdog is stopped or the
// timeout has already fired (one-shot).
func (w *IdleWatchdog) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.paused || w.timer == nil {
		return
	}
	// Stop returns false if the timer has already fired or been stopped.
	// In that case Fired() has already received; resetting won't undo the
	// fire, so we return without touching the timer.
	if !w.timer.Stop() {
		return
	}
	w.timer.Reset(w.timeout)
}

// Pause suspends idle expiry while an admitted operation has its own deadline.
// Resume starts a fresh idle interval when that operation finishes.
func (w *IdleWatchdog) Pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.timer == nil {
		return
	}
	w.paused = true
	w.epoch++
	w.timer.Stop()
	select {
	case <-w.fired:
	default:
	}
}

// Resume re-arms a paused watchdog; it never revives one permanently stopped.
func (w *IdleWatchdog) Resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || !w.paused || w.timer == nil {
		return
	}
	w.paused = false
	epoch := w.epoch
	w.timer = time.AfterFunc(w.timeout, func() { w.onFire(epoch) })
}

// Stop halts the timer permanently. Idempotent. After Stop, Fired() will
// not produce any new value.
func (w *IdleWatchdog) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	w.stopped = true
	if w.timer != nil {
		w.timer.Stop()
	}
}

// Fired returns a channel that receives once when the timeout elapses
// without a Reset.
func (w *IdleWatchdog) Fired() <-chan struct{} { return w.fired }

func (w *IdleWatchdog) onFire(epoch uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped || w.paused || w.epoch != epoch {
		return
	}
	// Non-blocking send: Fired() is a buffered channel of size 1. If the
	// consumer hasn't drained it, the second fire is dropped — by design,
	// once the watchdog has fired once it stays fired until Stop.
	select {
	case w.fired <- struct{}{}:
	default:
	}
}
