package audio

import (
	"sync"
	"sync/atomic"
)

// DefaultFrameCapacity is the per-buffer capacity returned by the
// package-level frame pool. It is sized for the worst-case malgo
// callback chunk we have observed in production (16 kHz mono S16 at
// 32 ms FrameSizeMs → 1024 bytes; doubled for a generous safety
// margin).
const DefaultFrameCapacity = 4096

// FramePool returns recyclable byte slices for short-lived PCM frame
// buffers. The hot path it addresses is the malgo capture callback
// (internal/audio/capturer_windows_cgo.go) which used to allocate a
// fresh slice per callback (~33×/sec per active capture, ~3300×/sec
// at 100 concurrent server sessions).
//
// # Contract
//
//   - Get returns a []byte with len(buf) == 0 and cap(buf) >=
//     pool.Capacity (DefaultFrameCapacity if unset). Callers append to
//     it; reslicing past cap will reallocate the way the language
//     normally would.
//   - Put returns the buffer to the pool. After Put the caller MUST NOT
//     read from or write to the slice — another goroutine may pick it
//     up immediately.
//   - Put is idempotent on the nil slice (no-op).
//   - Buffers whose grown capacity exceeds 4× the configured Capacity
//     are dropped on Put rather than retained, so pathological growth
//     does not pin large allocations in the pool.
//
// # Concurrency
//
// FramePool wraps sync.Pool — safe for concurrent Get / Put from many
// goroutines. The pool may discard entries at any GC cycle; callers
// MUST treat Get as may-allocate-fresh.
//
// # Observability
//
// HitRatio() returns a coarse hit/miss ratio over the pool's lifetime.
// It is intended for occasional sanity checks and metric exports; the
// underlying counters use atomic adds so HitRatio is safe to call
// concurrently with Get and Put.
type FramePool struct {
	// Capacity is the cap() of fresh buffers returned by Get. Zero
	// falls back to DefaultFrameCapacity at first use.
	Capacity int

	once sync.Once
	pool sync.Pool

	// gets counts every Get call (denominator of HitRatio).
	gets atomic.Uint64
	// misses is incremented inside sync.Pool.New — every miss is a
	// fresh allocation. hits = gets - misses by construction.
	misses atomic.Uint64
}

// DefaultFramePool is the package-level pool. Most callers should use
// the package functions Get / Put rather than constructing their own
// pool. Tests that need isolation construct their own FramePool.
var DefaultFramePool FramePool

// Get returns a recyclable buffer from the default pool.
func Get() []byte { return DefaultFramePool.Get() }

// Put returns a buffer to the default pool.
func Put(buf []byte) { DefaultFramePool.Put(buf) }

// Get returns a recyclable buffer with len 0 and capacity at least
// p.Capacity (or DefaultFrameCapacity when unset).
func (p *FramePool) Get() []byte {
	p.lazyInit()
	p.gets.Add(1)
	v := p.pool.Get()
	buf, ok := v.(*[]byte)
	if !ok || buf == nil {
		// sync.Pool.New always returns a non-nil *[]byte, so this
		// branch only fires under a future contract change. Treat as
		// best-effort fresh allocation; do not double-count a miss
		// (New already did it on the empty path).
		return make([]byte, 0, p.capacity())
	}
	return (*buf)[:0]
}

// Put returns the buffer to the pool. nil buffers are silently
// dropped. Buffers whose capacity exceeds 4× the configured Capacity
// are dropped to prevent unbounded growth from pinning the pool.
func (p *FramePool) Put(buf []byte) {
	if buf == nil {
		return
	}
	p.lazyInit()
	maxCap := 4 * p.capacity()
	if cap(buf) > maxCap {
		// Pathological grower: don't return it to the pool. The
		// next Get will allocate a fresh, correctly-sized buffer.
		return
	}
	cleared := buf[:0]
	p.pool.Put(&cleared)
}

// HitRatio returns the fraction of Get calls served from the cache
// (1.0 = always hit, 0.0 = always allocated fresh). Returns 0 when no
// operations have happened yet. The return is a snapshot — the
// counters may advance immediately after the call.
//
// Concretely: HitRatio = (gets - misses) / gets, where misses is
// incremented inside sync.Pool.New (i.e. every fresh allocation
// counts as a miss).
func (p *FramePool) HitRatio() float64 {
	if p == nil {
		return 0
	}
	gets := p.gets.Load()
	misses := p.misses.Load()
	if gets == 0 {
		return 0
	}
	if misses > gets {
		// Should be impossible — New only fires inside Get. Defensive
		// clamp so a counter race never surfaces a HitRatio > 1.
		return 0
	}
	return float64(gets-misses) / float64(gets)
}

// Stats are the raw lifetime counters. Useful for OTel meter callbacks
// that prefer integer reports over a derived ratio.
type Stats struct {
	Gets   uint64
	Misses uint64
}

// Stats returns the lifetime counters. Hits can be derived as
// stats.Gets - stats.Misses.
func (p *FramePool) Stats() Stats {
	if p == nil {
		return Stats{}
	}
	return Stats{Gets: p.gets.Load(), Misses: p.misses.Load()}
}

func (p *FramePool) lazyInit() {
	p.once.Do(func() {
		capacity := p.capacity()
		p.pool.New = func() any {
			// sync.Pool.New fires when the pool has no cached entry
			// for this P (per-P sharding) — every call here is a
			// fresh allocation, which is exactly what "miss" tracks.
			p.misses.Add(1)
			buf := make([]byte, 0, capacity)
			return &buf
		}
	})
}

func (p *FramePool) capacity() int {
	if p.Capacity <= 0 {
		return DefaultFrameCapacity
	}
	return p.Capacity
}
