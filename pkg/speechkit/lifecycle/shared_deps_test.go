package lifecycle

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSharedDepRegistry_LazyInitOnce(t *testing.T) {
	r := NewSharedDepRegistry()
	var inits int32
	r.Register("audio", func(ctx context.Context) (any, func() error, error) {
		atomic.AddInt32(&inits, 1)
		return "audio-value", nil, nil
	})

	for i := 0; i < 5; i++ {
		v, rel, err := r.Acquire(context.Background(), "audio")
		if err != nil {
			t.Fatalf("Acquire #%d failed: %v", i, err)
		}
		if v != "audio-value" {
			t.Fatalf("Acquire #%d returned %v; want audio-value", i, v)
		}
		t.Cleanup(func() { _ = rel() })
	}

	if got := atomic.LoadInt32(&inits); got != 1 {
		t.Fatalf("factory ran %d times; want 1 (lazy single-init)", got)
	}
}

func TestSharedDepRegistry_RefcountTeardownAtZero(t *testing.T) {
	r := NewSharedDepRegistry()
	var teardowns int32
	r.Register("ai", func(ctx context.Context) (any, func() error, error) {
		return "genkit", func() error {
			atomic.AddInt32(&teardowns, 1)
			return nil
		}, nil
	})

	_, rel1, err := r.Acquire(context.Background(), "ai")
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	_, rel2, err := r.Acquire(context.Background(), "ai")
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}

	if err := rel1(); err != nil {
		t.Fatalf("rel1: %v", err)
	}
	if got := atomic.LoadInt32(&teardowns); got != 0 {
		t.Fatalf("teardown ran after first release; want defer until last release. ran=%d", got)
	}

	if err := rel2(); err != nil {
		t.Fatalf("rel2: %v", err)
	}
	if got := atomic.LoadInt32(&teardowns); got != 1 {
		t.Fatalf("teardown ran %d times after final release; want 1", got)
	}
}

func TestSharedDepRegistry_ReleaseIsIdempotent(t *testing.T) {
	r := NewSharedDepRegistry()
	var teardowns int32
	r.Register("dep", func(ctx context.Context) (any, func() error, error) {
		return struct{}{}, func() error { atomic.AddInt32(&teardowns, 1); return nil }, nil
	})

	_, rel, err := r.Acquire(context.Background(), "dep")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Calling release multiple times must not double-teardown or
	// trigger the negative-refcount path. The first call drops the
	// entry; subsequent calls are no-ops.
	if err := rel(); err != nil {
		t.Fatalf("rel call 1: %v", err)
	}
	if err := rel(); err != nil {
		t.Fatalf("rel call 2: %v", err)
	}
	if err := rel(); err != nil {
		t.Fatalf("rel call 3: %v", err)
	}

	if got := atomic.LoadInt32(&teardowns); got != 1 {
		t.Fatalf("teardown ran %d times; want exactly 1 (idempotent release)", got)
	}
}

func TestSharedDepRegistry_AcquireUnregistered(t *testing.T) {
	r := NewSharedDepRegistry()
	_, _, err := r.Acquire(context.Background(), "nope")
	if !errors.Is(err, ErrNoFactory) {
		t.Fatalf("Acquire(unregistered) err = %v; want ErrNoFactory", err)
	}
}

func TestSharedDepRegistry_FactoryError(t *testing.T) {
	r := NewSharedDepRegistry()
	factoryErr := errors.New("upstream unreachable")
	r.Register("flaky", func(ctx context.Context) (any, func() error, error) {
		return nil, nil, factoryErr
	})

	_, _, err := r.Acquire(context.Background(), "flaky")
	if !errors.Is(err, factoryErr) {
		t.Fatalf("Acquire factory-err = %v; want wrap of %v", err, factoryErr)
	}

	// A second Acquire must retry the factory (no half-initialized
	// entry left behind).
	var inits int32
	r.Register("flaky", func(ctx context.Context) (any, func() error, error) {
		atomic.AddInt32(&inits, 1)
		return "ok-now", nil, nil
	})
	v, rel, err := r.Acquire(context.Background(), "flaky")
	if err != nil {
		t.Fatalf("Acquire after factory-fix: %v", err)
	}
	if v != "ok-now" {
		t.Fatalf("Acquire after factory-fix value = %v; want ok-now", v)
	}
	t.Cleanup(func() { _ = rel() })

	if got := atomic.LoadInt32(&inits); got != 1 {
		t.Fatalf("retry factory ran %d times; want 1", got)
	}
}

func TestSharedDepRegistry_ConcurrentAcquireSingleInit(t *testing.T) {
	r := NewSharedDepRegistry()
	var inits int32
	r.Register("hot", func(ctx context.Context) (any, func() error, error) {
		atomic.AddInt32(&inits, 1)
		return "shared", nil, nil
	})

	const N = 50
	var wg sync.WaitGroup
	releases := make([]ReleaseFunc, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, rel, err := r.Acquire(context.Background(), "hot")
			if err != nil {
				t.Errorf("goroutine %d Acquire: %v", idx, err)
				return
			}
			releases[idx] = rel
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&inits); got != 1 {
		t.Fatalf("factory ran %d times under concurrent acquire; want 1", got)
	}

	for _, rel := range releases {
		if rel != nil {
			_ = rel()
		}
	}

	if got := len(r.Snapshot()); got != 0 {
		t.Fatalf("Snapshot() len = %d after all releases; want 0", got)
	}
}

func TestSharedDepRegistry_Snapshot(t *testing.T) {
	r := NewSharedDepRegistry()
	r.Register("a", func(ctx context.Context) (any, func() error, error) { return "A", nil, nil })
	r.Register("b", func(ctx context.Context) (any, func() error, error) { return "B", nil, nil })

	_, relA, _ := r.Acquire(context.Background(), "a")
	_, relA2, _ := r.Acquire(context.Background(), "a")
	_, relB, _ := r.Acquire(context.Background(), "b")
	t.Cleanup(func() {
		_ = relA()
		_ = relA2()
		_ = relB()
	})

	snap := r.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot len = %d; want 2", len(snap))
	}
	if snap[0].Key != "a" || snap[0].Refs != 2 {
		t.Errorf("Snapshot[0] = %+v; want {a,2}", snap[0])
	}
	if snap[1].Key != "b" || snap[1].Refs != 1 {
		t.Errorf("Snapshot[1] = %+v; want {b,1}", snap[1])
	}
}
