package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRuntime is a deterministic ModeRuntime for tests. It records
// start/stop call counts and lets tests inject failures.
type fakeRuntime struct {
	name     ModeKey
	requires []SharedDepKey

	mu          sync.Mutex
	status      Status
	startCalls  int32
	stopCalls   int32
	startErr    error
	stopErr     error
	lastDepKeys []SharedDepKey

	// background simulates a goroutine the mode would normally own.
	background chan struct{}
	bgDone     chan struct{}
}

func newFakeRuntime(name ModeKey, requires ...SharedDepKey) *fakeRuntime {
	return &fakeRuntime{name: name, requires: requires, status: StatusStopped}
}

func (f *fakeRuntime) Name() ModeKey { return f.name }

func (f *fakeRuntime) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeRuntime) Requires() []SharedDepKey {
	out := make([]SharedDepKey, len(f.requires))
	copy(out, f.requires)
	return out
}

func (f *fakeRuntime) Start(ctx context.Context, deps Deps) error {
	atomic.AddInt32(&f.startCalls, 1)
	f.mu.Lock()
	f.lastDepKeys = make([]SharedDepKey, 0, len(deps.Values))
	for k := range deps.Values {
		f.lastDepKeys = append(f.lastDepKeys, k)
	}
	if f.startErr != nil {
		f.mu.Unlock()
		return f.startErr
	}
	f.status = StatusRunning
	// Simulate a goroutine the runtime is supposed to own. It exits
	// when Stop closes the channel. This lets goleak verify that
	// Stop actually terminates the background work.
	f.background = make(chan struct{})
	f.bgDone = make(chan struct{})
	go func(quit, done chan struct{}) {
		<-quit
		close(done)
	}(f.background, f.bgDone)
	f.mu.Unlock()
	return nil
}

func (f *fakeRuntime) Stop(ctx context.Context) error {
	atomic.AddInt32(&f.stopCalls, 1)
	f.mu.Lock()
	if f.stopErr != nil {
		err := f.stopErr
		f.mu.Unlock()
		return err
	}
	bg := f.background
	done := f.bgDone
	f.background = nil
	f.bgDone = nil
	f.status = StatusStopped
	f.mu.Unlock()

	if bg != nil {
		close(bg)
	}
	if done != nil {
		<-done
	}
	return nil
}

func TestRegistryRegisterDuplicate(t *testing.T) {
	reg := NewRegistry(nil)
	rt := newFakeRuntime(ModeDictation)
	if err := reg.Register(rt); err != nil {
		t.Fatalf("Register first: %v", err)
	}
	err := reg.Register(newFakeRuntime(ModeDictation))
	if !errors.Is(err, ErrModeAlreadyRegistered) {
		t.Fatalf("second Register err = %v; want ErrModeAlreadyRegistered", err)
	}
}

func TestRegistryDiffComputesStartStop(t *testing.T) {
	reg := NewRegistry(nil)
	dict := newFakeRuntime(ModeDictation)
	assist := newFakeRuntime(ModeAssist)
	va := newFakeRuntime(ModeVoiceAgent)

	_ = reg.Register(dict)
	_ = reg.Register(assist)
	_ = reg.Register(va)

	// Pretend Assist is already running.
	assist.mu.Lock()
	assist.status = StatusRunning
	assist.mu.Unlock()

	diff := reg.Diff(Target{
		ModeDictation:  true,
		ModeVoiceAgent: true,
	})

	if len(diff.ToStart) != 2 || diff.ToStart[0] != ModeDictation || diff.ToStart[1] != ModeVoiceAgent {
		t.Errorf("ToStart = %v; want [dictation voiceagent] (sorted)", diff.ToStart)
	}
	if len(diff.ToStop) != 1 || diff.ToStop[0] != ModeAssist {
		t.Errorf("ToStop = %v; want [assist]", diff.ToStop)
	}
}

func TestRegistryDiffEmptyWhenAligned(t *testing.T) {
	reg := NewRegistry(nil)
	rt := newFakeRuntime(ModeDictation)
	_ = reg.Register(rt)

	if !reg.Diff(Target{ModeDictation: false}).IsEmpty() {
		t.Fatalf("diff to current (stopped) state should be empty")
	}
}

func TestRegistryApplyStartsAndStopsModes(t *testing.T) {
	deps := NewSharedDepRegistry()
	deps.Register("audio.capture", func(ctx context.Context) (any, func() error, error) {
		return "audio", nil, nil
	})
	deps.Register("ai.genkit", func(ctx context.Context) (any, func() error, error) {
		return "genkit", nil, nil
	})

	reg := NewRegistry(deps)
	dict := newFakeRuntime(ModeDictation, "audio.capture")
	assist := newFakeRuntime(ModeAssist, "audio.capture", "ai.genkit")
	_ = reg.Register(dict)
	_ = reg.Register(assist)

	ctx := context.Background()
	if err := reg.Apply(ctx, Target{ModeDictation: true, ModeAssist: true}); err != nil {
		t.Fatalf("Apply on=on: %v", err)
	}
	if dict.Status() != StatusRunning || assist.Status() != StatusRunning {
		t.Fatalf("after Apply on=on; dict=%s assist=%s", dict.Status(), assist.Status())
	}

	if err := reg.Apply(ctx, Target{ModeDictation: true, ModeAssist: false}); err != nil {
		t.Fatalf("Apply assist=off: %v", err)
	}
	if assist.Status() != StatusStopped {
		t.Fatalf("assist after off = %s; want stopped", assist.Status())
	}
	if dict.Status() != StatusRunning {
		t.Fatalf("dictation after assist=off = %s; want running", dict.Status())
	}

	// Shared dep "ai.genkit" had refcount 1 (assist only). After assist
	// stops, the dep should be torn down. "audio.capture" had refcount
	// 2 (dict + assist); dict still holds it, so it stays live.
	snap := reg.Snapshot()
	if snap.Modes[ModeDictation] != StatusRunning {
		t.Errorf("snapshot.Modes[dictation] = %s; want running", snap.Modes[ModeDictation])
	}
	if snap.Modes[ModeAssist] != StatusStopped {
		t.Errorf("snapshot.Modes[assist] = %s; want stopped", snap.Modes[ModeAssist])
	}
	for _, dep := range snap.SharedDeps {
		if dep.Key == "ai.genkit" {
			t.Errorf("ai.genkit still held after assist stopped: refs=%d", dep.Refs)
		}
		if dep.Key == "audio.capture" && dep.Refs != 1 {
			t.Errorf("audio.capture refs = %d; want 1 (dictation still holds)", dep.Refs)
		}
	}
}

func TestRegistryStartReleasesOnFailure(t *testing.T) {
	deps := NewSharedDepRegistry()
	var tornDown int32
	deps.Register("audio.capture", func(ctx context.Context) (any, func() error, error) {
		return "audio", func() error { atomic.AddInt32(&tornDown, 1); return nil }, nil
	})

	reg := NewRegistry(deps)
	rt := newFakeRuntime(ModeAssist, "audio.capture")
	rt.startErr = errors.New("provider down")
	_ = reg.Register(rt)

	err := reg.Apply(context.Background(), Target{ModeAssist: true})
	if err == nil || !errors.Is(err, rt.startErr) {
		t.Fatalf("Apply with start-err = %v; want wrap of %v", err, rt.startErr)
	}
	if rt.Status() != StatusStopped {
		t.Errorf("status after failed start = %s; want stopped (runtime never flipped)", rt.Status())
	}
	if got := atomic.LoadInt32(&tornDown); got != 1 {
		t.Errorf("acquired dep teardown ran %d times; want 1 (rollback)", got)
	}
}

func TestRegistryStartReleasesOnDepFailure(t *testing.T) {
	deps := NewSharedDepRegistry()
	deps.Register("good.dep", func(ctx context.Context) (any, func() error, error) {
		return "v", nil, nil
	})
	depErr := errors.New("upstream gone")
	deps.Register("bad.dep", func(ctx context.Context) (any, func() error, error) {
		return nil, nil, depErr
	})

	reg := NewRegistry(deps)
	rt := newFakeRuntime(ModeAssist, "good.dep", "bad.dep")
	_ = reg.Register(rt)

	err := reg.Apply(context.Background(), Target{ModeAssist: true})
	if err == nil || !errors.Is(err, depErr) {
		t.Fatalf("Apply with dep-err = %v; want wrap of %v", err, depErr)
	}

	// good.dep should be released — no leaked refs in Snapshot.
	for _, d := range reg.Snapshot().SharedDeps {
		if d.Key == "good.dep" {
			t.Errorf("good.dep still held after dep failure: refs=%d", d.Refs)
		}
	}
}

func TestRegistrySubscribeEmitsTransitions(t *testing.T) {
	reg := NewRegistry(nil)
	rt := newFakeRuntime(ModeDictation)
	_ = reg.Register(rt)

	events, unsub := reg.Subscribe(16)
	defer unsub()

	if err := reg.Apply(context.Background(), Target{ModeDictation: true}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got := drainEvents(events, 2, 200*time.Millisecond)
	if len(got) < 2 {
		t.Fatalf("got %d events; want >= 2 (starting → running)", len(got))
	}
	if got[0].To != StatusStarting {
		t.Errorf("first event To = %s; want starting", got[0].To)
	}
	if got[1].To != StatusRunning {
		t.Errorf("second event To = %s; want running", got[1].To)
	}

	if err := reg.Apply(context.Background(), Target{ModeDictation: false}); err != nil {
		t.Fatalf("Apply off: %v", err)
	}
	got = drainEvents(events, 2, 200*time.Millisecond)
	if len(got) < 2 {
		t.Fatalf("after stop, got %d events; want >= 2 (stopping → stopped)", len(got))
	}
	if got[0].To != StatusStopping {
		t.Errorf("stop first To = %s; want stopping", got[0].To)
	}
	if got[1].To != StatusStopped {
		t.Errorf("stop second To = %s; want stopped", got[1].To)
	}
}

func TestRegistryUnsubscribeDoesNotBlockEmit(t *testing.T) {
	reg := NewRegistry(nil)
	rt := newFakeRuntime(ModeDictation)
	_ = reg.Register(rt)

	_, unsub := reg.Subscribe(1)
	unsub()
	// After unsubscribe, further events must not panic on a closed
	// channel — the registry must not write to closed subscribers.
	if err := reg.Apply(context.Background(), Target{ModeDictation: true}); err != nil {
		t.Fatalf("Apply after unsubscribe: %v", err)
	}
}

func TestRegistryShutdownStopsAllRunning(t *testing.T) {
	reg := NewRegistry(nil)
	dict := newFakeRuntime(ModeDictation)
	assist := newFakeRuntime(ModeAssist)
	_ = reg.Register(dict)
	_ = reg.Register(assist)

	if err := reg.Apply(context.Background(), Target{ModeDictation: true, ModeAssist: true}); err != nil {
		t.Fatalf("Apply on: %v", err)
	}
	if err := reg.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if dict.Status() != StatusStopped || assist.Status() != StatusStopped {
		t.Fatalf("after Shutdown: dict=%s assist=%s; want both stopped", dict.Status(), assist.Status())
	}
}

func TestRegistryDeterministicTimestamps(t *testing.T) {
	reg := NewRegistry(nil)
	rt := newFakeRuntime(ModeDictation)
	_ = reg.Register(rt)

	frozen := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	reg.SetClock(func() time.Time { return frozen })

	events, unsub := reg.Subscribe(8)
	defer unsub()

	if err := reg.Apply(context.Background(), Target{ModeDictation: true}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := drainEvents(events, 2, 200*time.Millisecond)
	for i, ev := range got {
		if !ev.Timestamp.Equal(frozen) {
			t.Errorf("event[%d] ts = %v; want %v", i, ev.Timestamp, frozen)
		}
	}
}

func TestRegistryApplyUnknownModeInTargetIgnored(t *testing.T) {
	reg := NewRegistry(nil)
	rt := newFakeRuntime(ModeDictation)
	_ = reg.Register(rt)

	// Target includes a mode that isn't registered. Apply must not
	// error — host builds may ship a subset of modes and still
	// receive policy from a configuration carrying all keys.
	err := reg.Apply(context.Background(), Target{
		ModeDictation:                     true,
		ModeKey("experimental.translate"): true,
	})
	if err != nil {
		t.Fatalf("Apply with unknown mode in target: %v", err)
	}
	if rt.Status() != StatusRunning {
		t.Errorf("dictation status = %s; want running", rt.Status())
	}
}

func drainEvents(ch <-chan TransitionEvent, want int, timeout time.Duration) []TransitionEvent {
	var out []TransitionEvent
	deadline := time.After(timeout)
	for len(out) < want {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
	return out
}

// Compile-time check: ensure fakeRuntime satisfies the interface.
var _ ModeRuntime = (*fakeRuntime)(nil)
