package lifecycle

import (
	"context"
	"testing"
)

// BenchmarkRegistryApplyStartAll measures the cost of bringing all three
// modes up from cold. Captures the lifecycle hot path that gets exercised
// every time the Wails-Desktop reads its config at startup.
func BenchmarkRegistryApplyStartAll(b *testing.B) {
	target := Target{ModeDictation: true, ModeAssist: true, ModeVoiceAgent: true}
	stop := Target{ModeDictation: false, ModeAssist: false, ModeVoiceAgent: false}
	ctx := context.Background()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		reg := newBenchRegistry()
		if err := reg.Apply(ctx, target); err != nil {
			b.Fatalf("Apply start: %v", err)
		}
		if err := reg.Apply(ctx, stop); err != nil {
			b.Fatalf("Apply stop: %v", err)
		}
	}
}

// BenchmarkRegistryApplyToggleOne measures the cost of a single-mode
// hot toggle on an otherwise-steady registry. Mirrors the UI scenario
// where the user flips one mode on or off.
func BenchmarkRegistryApplyToggleOne(b *testing.B) {
	ctx := context.Background()
	reg := newBenchRegistry()
	allOn := Target{ModeDictation: true, ModeAssist: true, ModeVoiceAgent: true}
	noAssist := Target{ModeDictation: true, ModeAssist: false, ModeVoiceAgent: true}
	if err := reg.Apply(ctx, allOn); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := reg.Apply(ctx, noAssist); err != nil {
			b.Fatalf("toggle off: %v", err)
		}
		if err := reg.Apply(ctx, allOn); err != nil {
			b.Fatalf("toggle on: %v", err)
		}
	}
}

// BenchmarkSharedDepRegistryAcquireHit measures the steady-state acquire
// path (entry already present, refcount increment). Dominant cost in a
// system that toggles modes frequently while holding the same shared
// audio capture.
func BenchmarkSharedDepRegistryAcquireHit(b *testing.B) {
	r := NewSharedDepRegistry()
	r.Register("hot", func(ctx context.Context) (any, func() error, error) {
		return "v", nil, nil
	})
	_, base, _ := r.Acquire(context.Background(), "hot")
	defer func() { _ = base() }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, rel, err := r.Acquire(context.Background(), "hot")
		if err != nil {
			b.Fatalf("acquire: %v", err)
		}
		_ = rel()
	}
}

func newBenchRegistry() *Registry {
	deps := NewSharedDepRegistry()
	deps.Register("audio.capture", func(ctx context.Context) (any, func() error, error) {
		return "audio", nil, nil
	})
	deps.Register("ai.genkit", func(ctx context.Context) (any, func() error, error) {
		return "genkit", nil, nil
	})
	deps.Register("tts.router", func(ctx context.Context) (any, func() error, error) {
		return "tts", nil, nil
	})
	reg := NewRegistry(deps)
	_ = reg.Register(newFakeRuntime(ModeDictation, "audio.capture"))
	_ = reg.Register(newFakeRuntime(ModeAssist, "audio.capture", "ai.genkit", "tts.router"))
	_ = reg.Register(newFakeRuntime(ModeVoiceAgent, "audio.capture", "ai.genkit", "tts.router"))
	return reg
}
