package sdkparity

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

type WakeDetection struct {
	Phrase      string
	Mode        string
	Probability float32
}

type WakeDispatcherOptions struct {
	SyntheticRelease bool
	ReleaseAfter     time.Duration
}

type WakeHotkeyEvent struct {
	KeyDown bool
	Binding string
	Source  string
}

type WakeDispatcherHarness struct {
	Dispatch           func(ctx context.Context, opts WakeDispatcherOptions, events []WakeDetection) ([]WakeHotkeyEvent, error)
	DispatchAfterClose func(ctx context.Context, opts WakeDispatcherOptions, event WakeDetection) ([]WakeHotkeyEvent, error)
}

func RunWakeDispatcherParity(t *testing.T, h WakeDispatcherHarness) {
	t.Helper()
	if h.Dispatch == nil {
		t.Fatal("WakeDispatcherHarness.Dispatch is required")
	}
	if h.DispatchAfterClose == nil {
		t.Fatal("WakeDispatcherHarness.DispatchAfterClose is required")
	}

	t.Run("emits_key_down_only_by_default", func(t *testing.T) {
		got, err := h.Dispatch(testContext(t), WakeDispatcherOptions{}, []WakeDetection{
			{Phrase: "Hey Quby", Mode: "voice_agent", Probability: 0.9},
		})
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		assertWakeEvents(t, got, []WakeHotkeyEvent{
			{KeyDown: true, Binding: "voice_agent", Source: "wakeword"},
		})
	})

	t.Run("synthetic_release_emits_balanced_pair", func(t *testing.T) {
		got, err := h.Dispatch(testContext(t), WakeDispatcherOptions{
			SyntheticRelease: true,
			ReleaseAfter:     5 * time.Millisecond,
		}, []WakeDetection{{Mode: "dictate"}})
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		assertWakeEvents(t, got, []WakeHotkeyEvent{
			{KeyDown: true, Binding: "dictate", Source: "wakeword"},
			{KeyDown: false, Binding: "dictate", Source: "wakeword"},
		})
	})

	t.Run("ignores_events_without_mode", func(t *testing.T) {
		got, err := h.Dispatch(testContext(t), WakeDispatcherOptions{}, []WakeDetection{{Phrase: "Hey Quby"}})
		if err != nil {
			t.Fatalf("Dispatch: %v", err)
		}
		assertWakeEvents(t, got, nil)
	})

	t.Run("ignores_events_after_close", func(t *testing.T) {
		got, err := h.DispatchAfterClose(testContext(t), WakeDispatcherOptions{}, WakeDetection{Mode: "assist"})
		if err != nil {
			t.Fatalf("DispatchAfterClose: %v", err)
		}
		assertWakeEvents(t, got, nil)
	})
}

type WakeAutoEndConfig struct {
	SilenceCutoff time.Duration
	ExitPhrases   []string
}

type WakeAutoEndPolicy interface {
	Start()
	NotifyTranscript(string)
	EndSignal() <-chan string
	Close()
}

type WakeAutoEndHarness struct {
	DefaultConfig func() WakeAutoEndConfig
	NewPolicy     func(WakeAutoEndConfig) WakeAutoEndPolicy
}

func RunWakeAutoEndParity(t *testing.T, h WakeAutoEndHarness) {
	t.Helper()
	if h.DefaultConfig == nil {
		t.Fatal("WakeAutoEndHarness.DefaultConfig is required")
	}
	if h.NewPolicy == nil {
		t.Fatal("WakeAutoEndHarness.NewPolicy is required")
	}

	t.Run("default_config_is_shared_framework_baseline", func(t *testing.T) {
		cfg := h.DefaultConfig()
		if cfg.SilenceCutoff != 10*time.Second {
			t.Fatalf("SilenceCutoff = %v, want 10s", cfg.SilenceCutoff)
		}
		wantPhrases := []string{"danke", "ende", "thanks", "bye"}
		for _, want := range wantPhrases {
			if !containsFold(cfg.ExitPhrases, want) {
				t.Fatalf("ExitPhrases missing %q in %v", want, cfg.ExitPhrases)
			}
		}
	})

	t.Run("exit_phrase_match_is_case_insensitive", func(t *testing.T) {
		policy := h.NewPolicy(WakeAutoEndConfig{
			SilenceCutoff: time.Second,
			ExitPhrases:   []string{"stop", "bye", "danke"},
		})
		defer policy.Close()

		policy.Start()
		policy.NotifyTranscript("Danke! Das war hilfreich.")
		assertAutoEndReason(t, policy.EndSignal(), "exit_phrase")
	})

	t.Run("close_without_fire_closes_signal", func(t *testing.T) {
		policy := h.NewPolicy(WakeAutoEndConfig{SilenceCutoff: time.Second})
		policy.Start()
		policy.Close()

		select {
		case reason, ok := <-policy.EndSignal():
			if ok {
				t.Fatalf("EndSignal should be closed without reason, got %q", reason)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("EndSignal did not close after Close")
		}
	})
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func assertWakeEvents(t *testing.T, got, want []WakeHotkeyEvent) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %+v, want %+v", got, want)
	}
}

func assertAutoEndReason(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	select {
	case got, ok := <-ch:
		if !ok {
			t.Fatalf("EndSignal closed without reason, want %q", want)
		}
		if got != want {
			t.Fatalf("EndReason = %q, want %q", got, want)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("EndSignal did not fire reason %q", want)
	}
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
