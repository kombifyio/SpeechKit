package audio

import (
	"testing"
)

// BenchmarkFramePoolGetPut measures the steady-state cost of the
// pool's hot path (matched Get + Put) — this is what the malgo
// callback would pay once B1b migrates the call site.
func BenchmarkFramePoolGetPut(b *testing.B) {
	pool := &FramePool{Capacity: 1024}
	// Warm up the pool so the first iteration doesn't dominate.
	for i := 0; i < 8; i++ {
		pool.Put(pool.Get())
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		buf = append(buf, 0x00) // simulate filling
		pool.Put(buf)
	}
}

// BenchmarkFreshAllocPerCallback measures the cost of the current
// malgo-callback strategy (allocate fresh per frame). Use this as the
// baseline; BenchmarkFramePoolGetPut should beat it on allocs/op.
func BenchmarkFreshAllocPerCallback(b *testing.B) {
	src := make([]byte, 640) // 20 ms at 16 kHz mono S16
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Mirrors the existing internal/audio/capturer_windows_cgo.go:107
		// `append([]byte(nil), inputSamples...)` copy-into-fresh pattern.
		_ = append([]byte(nil), src...)
	}
}

// BenchmarkFramePoolCopyFromSrc measures the apples-to-apples
// comparison: pool the destination buffer + copy from the malgo
// callback source. This is the exact pattern B1b will install.
func BenchmarkFramePoolCopyFromSrc(b *testing.B) {
	pool := &FramePool{Capacity: 1024}
	src := make([]byte, 640)
	// Warm up the pool.
	for i := 0; i < 8; i++ {
		pool.Put(pool.Get())
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		buf = append(buf, src...)
		pool.Put(buf)
	}
}
