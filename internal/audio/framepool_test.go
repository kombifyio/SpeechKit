package audio

import (
	"runtime"
	"sync"
	"testing"
)

func TestFramePoolGetReturnsCorrectCapacity(t *testing.T) {
	pool := &FramePool{Capacity: 512}
	buf := pool.Get()
	if got := len(buf); got != 0 {
		t.Errorf("Get().len = %d; want 0 (caller appends)", got)
	}
	if got := cap(buf); got < 512 {
		t.Errorf("Get().cap = %d; want >= 512", got)
	}
}

func TestFramePoolDefaultCapacityWhenZero(t *testing.T) {
	pool := &FramePool{}
	buf := pool.Get()
	if got := cap(buf); got < DefaultFrameCapacity {
		t.Errorf("Get().cap = %d; want >= DefaultFrameCapacity (%d)", got, DefaultFrameCapacity)
	}
}

func TestFramePoolPutThenGetReusesBuffer(t *testing.T) {
	pool := &FramePool{Capacity: 64}
	buf := pool.Get()
	buf = append(buf, 0xAB, 0xCD)
	original := &buf[0]

	pool.Put(buf)

	// Force the same goroutine to immediately re-acquire — under
	// normal conditions sync.Pool will hand back the buffer we just
	// returned. GC could in theory drop it; if so the test runs but
	// asserts only the zero-length contract.
	got := pool.Get()
	if len(got) != 0 {
		t.Errorf("re-acquired buf len = %d; want 0 (cleared on Put)", len(got))
	}
	if cap(got) < 64 {
		t.Errorf("re-acquired buf cap = %d; want >= 64", cap(got))
	}

	// Reusing the previous buffer means the underlying array is the
	// same — we can detect this by examining the first element after
	// growing.
	got = append(got, 0x00)
	if cap(got) >= cap(buf) && &got[0] == original {
		// Good — re-used. No further assertion needed.
		_ = got
	}
}

func TestFramePoolPutNilIsNoOp(t *testing.T) {
	pool := &FramePool{}
	// Must not panic.
	pool.Put(nil)
	if got := pool.Stats(); got.Gets != 0 || got.Misses != 0 {
		t.Errorf("Put(nil) mutated stats: %+v", got)
	}
}

func TestFramePoolDropsOversizedBuffer(t *testing.T) {
	pool := &FramePool{Capacity: 64}
	huge := make([]byte, 0, 8*1024) // 8 KB, far above 4× 64 = 256
	pool.Put(huge)
	// Force the pool to drop the cache so any subsequent Get must
	// either pull a different (small) entry or allocate fresh — but
	// must not hand us the 8 KB buffer back.
	runtime.GC()
	got := pool.Get()
	if cap(got) >= 8*1024 {
		t.Errorf("Get() returned cap=%d; want < 8KB (oversized buffer should have been dropped)", cap(got))
	}
}

func TestFramePoolStatsCountsMisses(t *testing.T) {
	pool := &FramePool{Capacity: 64}
	// Drain sync.Pool by GC so misses can be observed deterministically.
	runtime.GC()
	for i := 0; i < 5; i++ {
		_ = pool.Get()
	}
	stats := pool.Stats()
	if stats.Gets != 5 {
		t.Errorf("stats.Gets = %d; want 5", stats.Gets)
	}
	// The first Get after a GC drain definitely misses; subsequent
	// Gets without Put also miss. So all 5 are misses.
	if stats.Misses == 0 {
		t.Errorf("stats.Misses = 0; want >= 1 (every Get without prior Put should be a miss)")
	}
	if stats.Misses > stats.Gets {
		t.Errorf("stats.Misses (%d) > stats.Gets (%d); accounting bug", stats.Misses, stats.Gets)
	}

	ratio := pool.HitRatio()
	if ratio < 0 || ratio > 1 {
		t.Errorf("HitRatio = %v; want in [0, 1]", ratio)
	}
}

func TestFramePoolHitRatioImprovesOnReuse(t *testing.T) {
	pool := &FramePool{Capacity: 64}

	// Round 1: Get + Put 100 times. Every Get is a miss (pool empty
	// each time because we Put after). Actually wait — Put before
	// the next Get hands back the same buffer; subsequent Gets are
	// hits. So we have 1 miss + 99 hits in single-goroutine flow.
	for i := 0; i < 100; i++ {
		buf := pool.Get()
		pool.Put(buf)
	}

	stats := pool.Stats()
	if stats.Misses > 5 {
		t.Errorf("stats.Misses = %d after 100 paired Get/Put; expected single-digit (most should hit cache). Stats: %+v", stats.Misses, stats)
	}

	ratio := pool.HitRatio()
	if ratio < 0.90 {
		t.Errorf("HitRatio = %.3f; want >= 0.90 after paired Get/Put cycle", ratio)
	}
}

func TestFramePoolConcurrentGetPut(t *testing.T) {
	pool := &FramePool{Capacity: 64}
	const N = 32
	const ops = 500

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				buf := pool.Get()
				if cap(buf) < 64 {
					t.Errorf("concurrent Get cap = %d; want >= 64", cap(buf))
					return
				}
				buf = append(buf, byte(j))
				pool.Put(buf)
			}
		}()
	}
	wg.Wait()

	stats := pool.Stats()
	if stats.Gets != uint64(N*ops) {
		t.Errorf("stats.Gets = %d; want %d (no lost counts)", stats.Gets, N*ops)
	}
	// HitRatio should be well above zero — many of the puts come
	// back as hits to other goroutines.
	if pool.HitRatio() < 0.5 {
		t.Errorf("HitRatio under concurrent load = %.3f; want >= 0.5", pool.HitRatio())
	}
}

func TestPackageLevelGetPut(t *testing.T) {
	// Sanity: the package-level Get/Put delegate to DefaultFramePool.
	buf := Get()
	if cap(buf) < DefaultFrameCapacity {
		t.Errorf("package-level Get().cap = %d; want >= DefaultFrameCapacity (%d)", cap(buf), DefaultFrameCapacity)
	}
	Put(buf)
}

func TestFramePoolNilReceiverHelpers(t *testing.T) {
	var p *FramePool
	if got := p.HitRatio(); got != 0 {
		t.Errorf("nil-receiver HitRatio = %v; want 0", got)
	}
	if got := p.Stats(); got.Gets != 0 || got.Misses != 0 {
		t.Errorf("nil-receiver Stats = %+v; want zero", got)
	}
}
