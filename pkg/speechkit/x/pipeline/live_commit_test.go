package pipeline

import (
	"context"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestJoinTranscriptFragmentsInsertsSentenceGap(t *testing.T) {
	got := JoinTranscriptFragments("Das ist ein Satz.", "Und weiter geht's.")
	if !strings.Contains(got, "Satz. Und") {
		t.Fatalf("joined = %q, want a space between sentences", got)
	}
}

func TestLiveInjectFragmentPrefixesSpaceOnSameSession(t *testing.T) {
	first, tail, session := LiveInjectFragment(0, "", "Hallo Welt.", 9)
	if first != "Hallo Welt." {
		t.Fatalf("first fragment = %q", first)
	}
	second, _, _ := LiveInjectFragment(session, tail, "Weiter so.", 9)
	paste := first + second
	if !strings.Contains(paste, "Welt. Weiter") {
		t.Fatalf("consecutive inject paste = %q, want a space between sentences", paste)
	}
}

func TestLiveCommitPassageFlushesAfterTwoSentences(t *testing.T) {
	inner := &recordingDictationSink{}
	sink := wrapLiveCommitSink(inner, LiveCommitPassage)
	ctx := context.Background()
	opts := speechkit.DictationStreamSinkOptions{Language: "de"}

	if err := sink.HandleDictationStreamEvent(ctx, speechkit.DictationStreamEvent{Text: "Erster Satz.", IsFinal: true, SessionID: 1}, opts); err != nil {
		t.Fatalf("first final: %v", err)
	}
	if got := inner.count(); got != 0 {
		t.Fatalf("passage flushed after one sentence: %d events", got)
	}
	if err := sink.HandleDictationStreamEvent(ctx, speechkit.DictationStreamEvent{Text: "Zweiter Satz.", IsFinal: true, SessionID: 1}, opts); err != nil {
		t.Fatalf("second final: %v", err)
	}
	if got := inner.count(); got != 1 {
		t.Fatalf("passage events = %d, want 1 coalesced final", got)
	}
	inner.mu.Lock()
	got := inner.events[0].Text
	inner.mu.Unlock()
	if !strings.Contains(got, "Satz. Zweiter") {
		t.Fatalf("coalesced = %q, want both sentences with a space", got)
	}
}

func TestLiveCommitHoldFlushesIncompletePassage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := &recordingDictationSink{}
		sink := &liveCommitSink{
			inner:  inner,
			policy: LiveCommitPolicy{Mode: LiveCommitPassage, MinSentences: 2, Hold: 20 * time.Millisecond},
		}
		if err := sink.HandleDictationStreamEvent(context.Background(), speechkit.DictationStreamEvent{Text: "Nur ein Satz.", IsFinal: true}, speechkit.DictationStreamSinkOptions{}); err != nil {
			t.Fatalf("final: %v", err)
		}
		// Virtual time: advance past the hold window and let the timer fire.
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()
		if got := inner.count(); got != 1 {
			t.Fatalf("hold flush events = %d, want 1", got)
		}
	})
}

type recordingDictationSink struct {
	mu     sync.Mutex
	events []speechkit.DictationStreamEvent
}

func (s *recordingDictationSink) HandleDictationStreamEvent(_ context.Context, event speechkit.DictationStreamEvent, _ speechkit.DictationStreamSinkOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *recordingDictationSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// Hold-to-talk keeps the hotkey modifiers down for the whole capture, so its
// live finals wait for the release and reach the field as one insertion.
func TestLiveCommitSessionHoldsEveryFinalUntilStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := &recordingDictationSink{}
		sink := wrapLiveCommitSink(inner, LiveCommitSession)
		ctx := context.Background()
		opts := speechkit.DictationStreamSinkOptions{Language: "de"}
		for _, text := range []string{"Erster Satz.", "Zweiter Satz.", "Dritter Satz."} {
			if err := sink.HandleDictationStreamEvent(ctx, speechkit.DictationStreamEvent{Text: text, IsFinal: true, SessionID: 1}, opts); err != nil {
				t.Fatalf("final %q: %v", text, err)
			}
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := inner.count(); got != 0 {
			t.Fatalf("session mode committed %d events before stop", got)
		}

		if err := sink.(LiveCommitFlusher).FlushLiveCommit(ctx); err != nil {
			t.Fatalf("flush: %v", err)
		}
		inner.mu.Lock()
		defer inner.mu.Unlock()
		if len(inner.events) != 1 || inner.events[0].Text != "Erster Satz. Zweiter Satz. Dritter Satz." {
			t.Fatalf("events at stop = %#v, want one final with the whole dictation", inner.events)
		}
	})
}
