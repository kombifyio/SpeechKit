package wakeword

import (
	"sync"
	"testing"
	"time"
)

func TestDefaultAutoEndConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultAutoEndConfig()
	if cfg.SilenceCutoff != 10*time.Second {
		t.Errorf("SilenceCutoff = %v, want 10s", cfg.SilenceCutoff)
	}
	if len(cfg.ExitPhrases) == 0 {
		t.Fatal("ExitPhrases should not be empty")
	}
	// Spot-check DE+EN coverage so a typo regression on the default list
	// surfaces here instead of in the field.
	want := map[string]bool{"danke": false, "ende": false, "thanks": false, "bye": false}
	for _, phrase := range cfg.ExitPhrases {
		if _, ok := want[phrase]; ok {
			want[phrase] = true
		}
	}
	for phrase, found := range want {
		if !found {
			t.Errorf("DefaultAutoEndConfig.ExitPhrases missing %q", phrase)
		}
	}
}

func TestAutoEndPolicy_SilenceTimerFires(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 30 * time.Millisecond,
	}, nil)
	defer policy.Close()

	policy.Start()
	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonSilence {
			t.Errorf("EndReason = %q, want silence", reason)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("silence timer did not fire within 200ms")
	}
}

func TestAutoEndPolicy_NotifyActivityResetsTimer(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 50 * time.Millisecond,
	}, nil)
	defer policy.Close()

	policy.Start()

	// Three activity bursts at 20 ms intervals (well below cutoff) should
	// keep the timer alive throughout. Total elapsed = 60 ms > cutoff, but
	// no single silence window exceeds the cutoff.
	for i := 0; i < 3; i++ {
		time.Sleep(20 * time.Millisecond)
		policy.NotifyActivity()
	}

	// Now stay silent — the timer should fire ~50ms after the last activity.
	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonSilence {
			t.Errorf("EndReason = %q, want silence", reason)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("silence timer never fired after activity bursts stopped")
	}
}

func TestAutoEndPolicy_ExitPhraseMatch(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 1 * time.Second, // generous; should not fire in this test
		ExitPhrases:   []string{"danke", "stop"},
	}, nil)
	defer policy.Close()

	policy.Start()
	policy.NotifyTranscript("Okay danke, das war's.")

	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonExitPhrase {
			t.Errorf("EndReason = %q, want exit_phrase", reason)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("exit-phrase match did not fire EndSignal")
	}
}

func TestAutoEndPolicy_ExitPhraseCaseInsensitive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		transcript string
	}{
		{"all_caps", "STOP THE RECORDING"},
		{"mixed_case", "Bye for now"},
		{"with_punctuation", "Danke! Das war hilfreich."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			policy := NewAutoEndPolicy(AutoEndConfig{
				SilenceCutoff: 1 * time.Second,
				ExitPhrases:   []string{"stop", "bye", "danke"},
			}, nil)
			defer policy.Close()

			policy.Start()
			policy.NotifyTranscript(tc.transcript)

			select {
			case reason := <-policy.EndSignal():
				if reason != EndReasonExitPhrase {
					t.Errorf("EndReason = %q, want exit_phrase", reason)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("transcript %q did not match", tc.transcript)
			}
		})
	}
}

func TestAutoEndPolicy_NonMatchingTranscriptResetsTimerOnly(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 80 * time.Millisecond,
		ExitPhrases:   []string{"goodbye"},
	}, nil)
	defer policy.Close()

	policy.Start()

	// Non-matching transcript counts as activity — silence timer should
	// reset, NOT fire immediately.
	time.Sleep(50 * time.Millisecond)
	policy.NotifyTranscript("Hello, what time is it?")

	// We should NOT see EndSignal in the next 30ms (well within the
	// silence cutoff that just got reset).
	select {
	case reason, ok := <-policy.EndSignal():
		if ok {
			t.Fatalf("unexpected EndSignal fire after non-matching transcript: %v", reason)
		}
	case <-time.After(30 * time.Millisecond):
		// expected
	}

	// Then after the full cutoff elapses with silence, it should fire.
	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonSilence {
			t.Errorf("EndReason = %q, want silence", reason)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("silence timer never fired after non-matching transcript")
	}
}

func TestAutoEndPolicy_FiresOnlyOnce(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 1 * time.Second,
		ExitPhrases:   []string{"stop"},
	}, nil)
	defer policy.Close()

	policy.Start()
	policy.NotifyTranscript("stop") // first match -> fires exit_phrase

	first := <-policy.EndSignal()
	if first != EndReasonExitPhrase {
		t.Fatalf("first reason = %q, want exit_phrase", first)
	}

	// Second match on the same channel should be a no-op (channel closed
	// after first fire — receiver gets zero value + ok=false).
	policy.NotifyTranscript("stop")
	policy.NotifyActivity()

	select {
	case second, ok := <-policy.EndSignal():
		if ok && second != "" {
			t.Errorf("second receive should yield closed channel; got reason=%q ok=%v", second, ok)
		}
	default:
		t.Fatal("EndSignal channel should be closed after first fire")
	}
}

func TestAutoEndPolicy_CloseIdempotent(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 1 * time.Second,
	}, nil)

	policy.Start()
	policy.Close()
	policy.Close() // second close should not panic
	policy.Close() // third for good measure

	// Notify*-Calls after Close are no-ops, not panics.
	policy.NotifyActivity()
	policy.NotifyTranscript("danke")
}

func TestAutoEndPolicy_CloseWithoutFireClosesChannel(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 1 * time.Second,
	}, nil)

	policy.Start()
	policy.Close()

	select {
	case reason, ok := <-policy.EndSignal():
		if ok {
			t.Errorf("Close-without-fire should close channel; got reason=%q ok=%v", reason, ok)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("EndSignal channel not closed after Close-without-fire")
	}
}

func TestAutoEndPolicy_StartIdempotent(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 60 * time.Millisecond,
	}, nil)
	defer policy.Close()

	policy.Start()
	policy.Start() // second start should be a no-op (does not restart the timer)
	policy.Start()

	// Timer should still fire once after ~60ms from the first Start.
	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonSilence {
			t.Errorf("EndReason = %q, want silence", reason)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("silence timer did not fire after idempotent Start")
	}
}

func TestAutoEndPolicy_NotifyBeforeStartIsNoOp(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 50 * time.Millisecond,
		ExitPhrases:   []string{"stop"},
	}, nil)
	defer policy.Close()

	// Notify before Start should not arm anything.
	policy.NotifyActivity()
	policy.NotifyTranscript("stop")

	select {
	case reason := <-policy.EndSignal():
		t.Errorf("EndSignal fired without Start, reason=%q", reason)
	case <-time.After(80 * time.Millisecond):
		// expected
	}
}

func TestAutoEndPolicy_ZeroSilenceCutoffDisablesSilenceTrigger(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 0, // disabled
		ExitPhrases:   []string{"stop"},
	}, nil)
	defer policy.Close()

	policy.Start()

	// Wait longer than any reasonable default — no fire.
	select {
	case reason := <-policy.EndSignal():
		t.Errorf("silence trigger should be disabled, got reason=%q", reason)
	case <-time.After(150 * time.Millisecond):
		// expected
	}

	// Exit-phrase still works.
	policy.NotifyTranscript("please stop now")
	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonExitPhrase {
			t.Errorf("EndReason = %q, want exit_phrase", reason)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("exit-phrase still failed to fire after silence-disabled config")
	}
}

func TestAutoEndPolicy_EmptyExitPhrasesDisablesPhraseTrigger(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 80 * time.Millisecond,
		ExitPhrases:   nil,
	}, nil)
	defer policy.Close()

	policy.Start()
	policy.NotifyTranscript("danke das war's")

	// Should only fire via silence, not via phrase.
	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonSilence {
			t.Errorf("EndReason = %q, want silence", reason)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("silence trigger should still fire when ExitPhrases is empty")
	}
}

func TestAutoEndPolicy_ConcurrentNotifyActivityIsRaceFree(t *testing.T) {
	t.Parallel()
	policy := NewAutoEndPolicy(AutoEndConfig{
		SilenceCutoff: 100 * time.Millisecond,
	}, nil)
	defer policy.Close()

	policy.Start()

	// Pound NotifyActivity from many goroutines for ~50ms. The race
	// detector should flag any unprotected concurrent state access.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					policy.NotifyActivity()
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	// After the goroutines stop, silence cutoff should kick in (~100ms later).
	select {
	case reason := <-policy.EndSignal():
		if reason != EndReasonSilence {
			t.Errorf("EndReason = %q, want silence", reason)
		}
	case <-time.After(400 * time.Millisecond):
		t.Fatal("silence timer never fired after concurrent activity stopped")
	}
}

func TestAutoEndPolicy_ConfigReturnsCopy(t *testing.T) {
	t.Parallel()
	original := AutoEndConfig{
		SilenceCutoff: 5 * time.Second,
		ExitPhrases:   []string{"stop", "danke"},
	}
	policy := NewAutoEndPolicy(original, nil)
	defer policy.Close()

	got := policy.Config()
	if got.SilenceCutoff != 5*time.Second {
		t.Errorf("Config().SilenceCutoff = %v, want 5s", got.SilenceCutoff)
	}
	// Mutating the returned slice must not affect the policy's internal state.
	got.ExitPhrases[0] = "MUTATED"
	got2 := policy.Config()
	if got2.ExitPhrases[0] == "MUTATED" {
		t.Error("Config() must return a slice copy, not the internal slice")
	}
}
