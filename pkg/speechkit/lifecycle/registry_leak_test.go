package lifecycle

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// TestRegistryLeak is the canonical contract test for the
// "no goroutines survive a clean Stop" promise documented in
// ModeRuntime. Fake runtimes spawn one background goroutine on
// Start and shut it down on Stop; the leak detector at the bottom
// verifies that Apply(off) followed by Shutdown leaves no goroutines.
func TestRegistryLeak(t *testing.T) {
	defer goleak.VerifyNone(t,
		// goroutine started by go test for test orchestration is
		// allowed; it lives for the duration of the test binary.
		goleak.IgnoreTopFunction("testing.(*T).Parallel"),
	)

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
	if err := reg.Register(newFakeRuntime(ModeDictation, "audio.capture")); err != nil {
		t.Fatalf("register dictation: %v", err)
	}
	if err := reg.Register(newFakeRuntime(ModeAssist, "audio.capture", "ai.genkit", "tts.router")); err != nil {
		t.Fatalf("register assist: %v", err)
	}
	if err := reg.Register(newFakeRuntime(ModeVoiceAgent, "audio.capture", "ai.genkit", "tts.router")); err != nil {
		t.Fatalf("register voiceagent: %v", err)
	}

	ctx := context.Background()

	// Bring everything up.
	if err := reg.Apply(ctx, Target{
		ModeDictation:  true,
		ModeAssist:     true,
		ModeVoiceAgent: true,
	}); err != nil {
		t.Fatalf("Apply all-on: %v", err)
	}

	// Toggle a single mode off then on then off — exercises partial
	// transitions in the middle.
	if err := reg.Apply(ctx, Target{
		ModeDictation:  true,
		ModeAssist:     false,
		ModeVoiceAgent: true,
	}); err != nil {
		t.Fatalf("Apply assist=off: %v", err)
	}
	if err := reg.Apply(ctx, Target{
		ModeDictation:  true,
		ModeAssist:     true,
		ModeVoiceAgent: true,
	}); err != nil {
		t.Fatalf("Apply assist=back on: %v", err)
	}

	// Final shutdown.
	if err := reg.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if got := len(reg.Snapshot().SharedDeps); got != 0 {
		t.Fatalf("post-Shutdown SharedDeps snapshot len = %d; want 0 (no leaked refcounts)", got)
	}

	// Give any latent goroutines a moment to exit before goleak runs.
	// In practice everything is synchronous, but a short grace period
	// hides timing noise in CI.
	time.Sleep(10 * time.Millisecond)
}
