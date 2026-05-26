package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ErrModeAlreadyRegistered is returned by [Registry.Register] when a
// runtime with the same Name() is registered twice. Re-registering is
// not supported — hosts should construct the registry once at startup.
var ErrModeAlreadyRegistered = errors.New("lifecycle: mode runtime already registered")

// ErrModeUnknown is returned by single-mode operations (Start, Stop)
// when the given key has no registered runtime.
var ErrModeUnknown = errors.New("lifecycle: mode not registered")

// Target is the desired-state input to [Registry.Apply]. A mode whose
// key is true should be running; a mode whose key is false (or absent)
// should be stopped. Modes not registered with the registry are
// silently ignored — Target may safely include keys for modes the
// current host build does not ship.
type Target map[ModeKey]bool

// Diff describes the work [Registry.Apply] needs to do to reach a
// target state. ToStop is processed before ToStart so refcounted
// shared deps released by stopping a mode become available to a mode
// being started in the same transition.
type Diff struct {
	ToStart []ModeKey
	ToStop  []ModeKey
}

// IsEmpty reports whether the diff would perform no work.
func (d Diff) IsEmpty() bool { return len(d.ToStart) == 0 && len(d.ToStop) == 0 }

// Registry orchestrates mode runtimes and their shared dependencies.
//
// Typical wiring:
//
//	sharedDeps := lifecycle.NewSharedDepRegistry()
//	sharedDeps.Register("audio.capture", audioFactory)
//	sharedDeps.Register("stt.router", sttFactory)
//	// ... etc
//
//	registry := lifecycle.NewRegistry(sharedDeps)
//	_ = registry.Register(dictationRuntime)
//	_ = registry.Register(assistRuntime)
//	_ = registry.Register(voiceAgentRuntime)
//
//	// At startup or when settings change:
//	if err := registry.Apply(ctx, lifecycle.Target{
//	    lifecycle.ModeDictation:  true,
//	    lifecycle.ModeAssist:     false,
//	    lifecycle.ModeVoiceAgent: false,
//	}); err != nil { ... }
//
// Registry is safe for concurrent use; Apply calls serialise on an
// internal mutex so transitions never interleave.
type Registry struct {
	mu          sync.Mutex
	runtimes    map[ModeKey]ModeRuntime
	sharedDeps  *SharedDepRegistry
	releases    map[ModeKey][]ReleaseFunc
	subscribers []chan TransitionEvent
	clock       func() time.Time
}

// NewRegistry returns an empty registry wired to the given shared-dep
// registry. Passing nil for sharedDeps creates an empty SharedDepRegistry —
// useful for tests that don't exercise shared deps.
func NewRegistry(sharedDeps *SharedDepRegistry) *Registry {
	if sharedDeps == nil {
		sharedDeps = NewSharedDepRegistry()
	}
	return &Registry{
		runtimes:   map[ModeKey]ModeRuntime{},
		sharedDeps: sharedDeps,
		releases:   map[ModeKey][]ReleaseFunc{},
		clock:      time.Now,
	}
}

// SetClock overrides the timestamp source for [TransitionEvent].
// Tests use this to inject a deterministic clock; production leaves
// the default time.Now.
func (r *Registry) SetClock(clock func() time.Time) {
	if r == nil || clock == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clock = clock
}

// Register adds a mode runtime to the registry. Returns
// [ErrModeAlreadyRegistered] when the same Name() is used twice.
func (r *Registry) Register(rt ModeRuntime) error {
	if r == nil || rt == nil {
		return errors.New("lifecycle: Register on nil registry or runtime")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runtimes[rt.Name()]; ok {
		return fmt.Errorf("%w: %s", ErrModeAlreadyRegistered, rt.Name())
	}
	r.runtimes[rt.Name()] = rt
	return nil
}

// Apply transitions every registered mode to the state described by
// target. Stops run first, then starts. If a start fails, the
// transition aborts after the failing mode; modes earlier in the
// ToStart order remain Running. Use [TransitionEvent] subscribers to
// observe per-mode outcomes.
//
// Returns the first error encountered. Callers that want all-or-
// nothing semantics should compute the rollback themselves from
// subscriber events; the registry deliberately does not auto-roll-back
// because partial state may be desirable (e.g. Dictation stays up
// even if Voice Agent failed to acquire Gemini Live).
func (r *Registry) Apply(ctx context.Context, target Target) error {
	r.mu.Lock()
	diff := r.diffLocked(target)
	r.mu.Unlock()
	return r.transition(ctx, diff)
}

// Diff computes what Apply would do without performing it. Useful
// for tests and for UIs that want to preview the transition before
// committing.
func (r *Registry) Diff(target Target) Diff {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.diffLocked(target)
}

func (r *Registry) diffLocked(target Target) Diff {
	var toStart, toStop []ModeKey
	for mode, rt := range r.runtimes {
		want := target[mode]
		isOn := rt.Status().IsActive()
		switch {
		case want && !isOn:
			toStart = append(toStart, mode)
		case !want && isOn:
			toStop = append(toStop, mode)
		}
	}
	sort.Slice(toStart, func(i, j int) bool { return toStart[i] < toStart[j] })
	sort.Slice(toStop, func(i, j int) bool { return toStop[i] < toStop[j] })
	return Diff{ToStart: toStart, ToStop: toStop}
}

func (r *Registry) transition(ctx context.Context, diff Diff) error {
	for _, mode := range diff.ToStop {
		if err := r.stopMode(ctx, mode); err != nil {
			return err
		}
	}
	for _, mode := range diff.ToStart {
		if err := r.startMode(ctx, mode); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) startMode(ctx context.Context, mode ModeKey) error {
	r.mu.Lock()
	rt, ok := r.runtimes[mode]
	clock := r.clock
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrModeUnknown, mode)
	}

	from := rt.Status()

	// Acquire shared deps the runtime declared. If any acquire fails,
	// release everything we got so far before returning.
	deps := Deps{Values: map[SharedDepKey]any{}}
	var acquired []ReleaseFunc
	for _, key := range rt.Requires() {
		v, rel, err := r.sharedDeps.Acquire(ctx, key)
		if err != nil {
			for _, prev := range acquired {
				_ = prev()
			}
			r.emit(TransitionEvent{Mode: mode, From: from, To: StatusFailed, Err: err, Timestamp: clock()})
			return fmt.Errorf("lifecycle: start %s acquire %s: %w", mode, key, err)
		}
		deps.Values[key] = v
		acquired = append(acquired, rel)
	}

	r.emit(TransitionEvent{Mode: mode, From: from, To: StatusStarting, Timestamp: clock()})

	if err := rt.Start(ctx, deps); err != nil {
		for _, rel := range acquired {
			_ = rel()
		}
		r.emit(TransitionEvent{Mode: mode, From: StatusStarting, To: StatusFailed, Err: err, Timestamp: clock()})
		return fmt.Errorf("lifecycle: start %s: %w", mode, err)
	}

	r.mu.Lock()
	r.releases[mode] = acquired
	r.mu.Unlock()
	r.emit(TransitionEvent{Mode: mode, From: StatusStarting, To: StatusRunning, Timestamp: clock()})
	return nil
}

func (r *Registry) stopMode(ctx context.Context, mode ModeKey) error {
	r.mu.Lock()
	rt, ok := r.runtimes[mode]
	clock := r.clock
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrModeUnknown, mode)
	}

	from := rt.Status()
	r.emit(TransitionEvent{Mode: mode, From: from, To: StatusStopping, Timestamp: clock()})

	stopErr := rt.Stop(ctx)

	r.mu.Lock()
	releases := r.releases[mode]
	delete(r.releases, mode)
	r.mu.Unlock()

	// Release shared deps even if Stop returned an error — we want
	// best-effort resource release rather than leaking on a misbehaving
	// runtime. The Stop error is surfaced to the caller below.
	for _, rel := range releases {
		_ = rel()
	}

	if stopErr != nil {
		r.emit(TransitionEvent{Mode: mode, From: StatusStopping, To: StatusFailed, Err: stopErr, Timestamp: clock()})
		return fmt.Errorf("lifecycle: stop %s: %w", mode, stopErr)
	}

	r.emit(TransitionEvent{Mode: mode, From: StatusStopping, To: StatusStopped, Timestamp: clock()})
	return nil
}

// Subscribe returns a channel that receives every [TransitionEvent].
// Buffer is the channel buffer size; events are dropped (with no error
// signal) if the consumer is slower than the producer — drain in a
// goroutine. Use the returned unsubscribe func to stop receiving and
// allow garbage collection of the channel.
func (r *Registry) Subscribe(buffer int) (<-chan TransitionEvent, func()) {
	if r == nil {
		return nil, func() {}
	}
	if buffer < 1 {
		buffer = 16
	}
	ch := make(chan TransitionEvent, buffer)
	r.mu.Lock()
	r.subscribers = append(r.subscribers, ch)
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, sub := range r.subscribers {
			if sub == ch {
				r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
				close(ch)
				return
			}
		}
	}
}

func (r *Registry) emit(ev TransitionEvent) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	// Hold the registry mutex for the whole emit. Unsubscribe closes
	// channels under the same lock, so an emitter cannot observe a
	// closed channel here. Sends are non-blocking (default branch)
	// so the lock is held for nanoseconds even with many subscribers.
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sub := range r.subscribers {
		select {
		case sub <- ev:
		default:
			// Subscriber too slow; drop the event. The lifecycle
			// package keeps Subscribe DX-simple — observers that
			// can't afford drops should use a larger buffer.
		}
	}
}

// Snapshot returns the current Status of every registered mode, plus
// the active shared-dep refcounts. Used by /readyz handlers and OTel
// metric collection.
type SnapshotView struct {
	Modes      map[ModeKey]Status
	SharedDeps []SharedDepStatus
}

// Snapshot returns a point-in-time view of the registry state.
func (r *Registry) Snapshot() SnapshotView {
	if r == nil {
		return SnapshotView{}
	}
	r.mu.Lock()
	modes := make(map[ModeKey]Status, len(r.runtimes))
	for k, rt := range r.runtimes {
		modes[k] = rt.Status()
	}
	r.mu.Unlock()
	return SnapshotView{Modes: modes, SharedDeps: r.sharedDeps.Snapshot()}
}

// Shutdown stops every Running mode and releases all shared deps.
// Hosts should call this on process exit before tearing down the
// rest of the application. Returns the first stop error, if any —
// remaining modes are still attempted.
func (r *Registry) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	modes := make([]ModeKey, 0, len(r.runtimes))
	for k, rt := range r.runtimes {
		if rt.Status().IsActive() {
			modes = append(modes, k)
		}
	}
	r.mu.Unlock()
	sort.Slice(modes, func(i, j int) bool { return modes[i] < modes[j] })

	var firstErr error
	for _, mode := range modes {
		if err := r.stopMode(ctx, mode); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
