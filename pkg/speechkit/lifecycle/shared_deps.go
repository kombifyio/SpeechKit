package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrNoFactory is returned by [SharedDepRegistry.Acquire] when no
// factory has been registered for the requested key. Hosts that wire
// the registry should register every key consumed by any mode they
// also register; this error indicates a wiring bug, not a runtime
// failure.
var ErrNoFactory = errors.New("lifecycle: no factory registered for shared dep")

// SharedDepFactory produces a value plus a teardown callback the
// registry will invoke when the last holder releases the value.
//
// The teardown MAY be nil for stateless values (e.g. a pre-built
// catalog struct). Non-nil teardown errors are surfaced to the
// releasing caller; the registry still drops the entry so a fresh
// Acquire rebuilds from scratch.
type SharedDepFactory func(ctx context.Context) (value any, teardown func() error, err error)

// ReleaseFunc is returned by [SharedDepRegistry.Acquire]; calling it
// decrements the refcount and triggers teardown when the count hits
// zero. ReleaseFunc is safe to call multiple times — second and
// subsequent calls are no-ops.
type ReleaseFunc func() error

// SharedDepRegistry refcounts shared dependencies so multiple modes
// can share expensive state (Genkit runtime, STT router, audio
// capture) without duplicating it, and so a dep is released
// deterministically when no mode is using it any more.
//
// Construction sequence:
//
//  1. [NewSharedDepRegistry] creates an empty registry.
//  2. Host calls [Register] for every key it expects modes to claim.
//  3. Mode runtimes call [Acquire] from their Start methods.
//  4. Each Acquire returns a [ReleaseFunc] the runtime stashes and
//     invokes from its Stop method.
//
// The registry is safe for concurrent use.
type SharedDepRegistry struct {
	mu        sync.Mutex
	factories map[SharedDepKey]SharedDepFactory
	entries   map[SharedDepKey]*depEntry
}

type depEntry struct {
	value    any
	teardown func() error
	refs     int
}

// NewSharedDepRegistry returns an empty registry. Register factories
// on the returned value before any mode tries to acquire.
func NewSharedDepRegistry() *SharedDepRegistry {
	return &SharedDepRegistry{
		factories: map[SharedDepKey]SharedDepFactory{},
		entries:   map[SharedDepKey]*depEntry{},
	}
}

// Register associates a factory with a key. Re-registering the same
// key replaces the factory — useful in tests; production wiring should
// register each key exactly once.
func (r *SharedDepRegistry) Register(key SharedDepKey, factory SharedDepFactory) {
	if r == nil || factory == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[key] = factory
}

// Acquire returns the value associated with key, lazily constructing
// it via the registered factory if this is the first holder. The
// caller MUST invoke the returned [ReleaseFunc] exactly once when it
// no longer needs the value (typically from ModeRuntime.Stop).
//
// Concurrent Acquire calls for the same key block on the registry
// mutex; the factory runs exactly once even under contention.
func (r *SharedDepRegistry) Acquire(ctx context.Context, key SharedDepKey) (any, ReleaseFunc, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("lifecycle: Acquire(%q) on nil registry", key)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.entries[key]; ok {
		e.refs++
		return e.value, r.releaseFor(key), nil
	}

	factory, ok := r.factories[key]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %q", ErrNoFactory, key)
	}

	value, teardown, err := factory(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("lifecycle: factory %q failed: %w", key, err)
	}
	e := &depEntry{value: value, teardown: teardown, refs: 1}
	r.entries[key] = e
	return e.value, r.releaseFor(key), nil
}

// releaseFor returns a one-shot release func for the given key. The
// closure captures `released` so repeated invocations are no-ops
// rather than double-decrement bugs.
func (r *SharedDepRegistry) releaseFor(key SharedDepKey) ReleaseFunc {
	var released bool
	var releaseMu sync.Mutex
	return func() error {
		releaseMu.Lock()
		if released {
			releaseMu.Unlock()
			return nil
		}
		released = true
		releaseMu.Unlock()

		r.mu.Lock()
		e, ok := r.entries[key]
		if !ok {
			r.mu.Unlock()
			return nil
		}
		e.refs--
		if e.refs > 0 {
			r.mu.Unlock()
			return nil
		}
		delete(r.entries, key)
		teardown := e.teardown
		r.mu.Unlock()

		if teardown != nil {
			return teardown()
		}
		return nil
	}
}

// SharedDepStatus is a read-only view of one entry for observability.
type SharedDepStatus struct {
	Key  SharedDepKey
	Refs int
}

// Snapshot returns the current set of live shared deps sorted by key.
// Useful for OTel metrics emission and for debugging "why is this
// goroutine still running" investigations.
func (r *SharedDepRegistry) Snapshot() []SharedDepStatus {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SharedDepStatus, 0, len(r.entries))
	for key, e := range r.entries {
		out = append(out, SharedDepStatus{Key: key, Refs: e.refs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
