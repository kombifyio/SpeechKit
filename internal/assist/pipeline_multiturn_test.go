package assist

import (
	"context"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// fakeToolExecutor records every call and returns a programmable
// result. Lets us drive the Pipeline through a multi-turn round-trip
// without mounting the real voice_companion skills (those have their
// own unit tests).
type fakeToolExecutor struct {
	calls   []ToolCall
	results []ToolResult
	idx     int
}

func (f *fakeToolExecutor) Execute(_ context.Context, c ToolCall) (ToolResult, error) {
	f.calls = append(f.calls, c)
	if f.idx >= len(f.results) {
		return ToolResult{}, nil
	}
	r := f.results[f.idx]
	f.idx++
	return r, nil
}

// fakeRouter overrides Decide to always return a fixed Intent.
// The pipeline still consults UtilityFor on follow-up turns, so we
// just need a route + a registered intent.
func newFakeMultiTurnPipeline(t *testing.T, execResults []ToolResult) (*Pipeline, *fakeToolExecutor, *InMemorySkillContextStore) {
	t.Helper()
	exec := &fakeToolExecutor{results: execResults}
	store := NewInMemorySkillContextStore(0, nil)

	// Use a router that resolves "set a timer" to IntentTimer via the
	// default resolver — the default shortcuts catalogue covers this.
	p := NewPipeline(nil, exec, nil, false, WithSkillContextStore(store))
	return p, exec, store
}

func TestPipeline_MultiTurn_FollowupRoutesBackToSameIntent(t *testing.T) {
	// Two-turn dialogue:
	//   Turn 1: user says "set a timer" → skill returns FollowupNeeded
	//   Turn 2: user says "5 minutes" → pipeline routes back to
	//           IntentTimer, skill returns done.
	results := []ToolResult{
		{
			Text:           "How long?",
			Action:         "respond",
			FollowupNeeded: true,
			FollowupState:  map[string]string{"awaiting_duration": "1"},
		},
		{
			Text:   "Timer set for 5 minutes.",
			Action: "execute",
		},
	}
	p, exec, store := newFakeMultiTurnPipeline(t, results)

	// Turn 1: a fresh transcript routes via the resolver to
	// IntentTimer (the default resolver covers "set a timer for...").
	turn1, err := p.Process(context.Background(), "set a timer", ProcessOpts{
		Locale:     "en",
		SessionKey: "user-1",
	})
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	if turn1.Text != "How long?" {
		t.Errorf("turn 1 Text = %q", turn1.Text)
	}
	if store.Len() != 1 {
		t.Errorf("store should hold 1 follow-up after turn 1; got %d", store.Len())
	}

	// Turn 2: a transcript that the resolver would NOT match to
	// IntentTimer ("5 minutes" alone has no codeword). The active
	// context must override the resolver and route back to the
	// timer skill.
	turn2, err := p.Process(context.Background(), "5 minutes", ProcessOpts{
		Locale:     "en",
		SessionKey: "user-1",
	})
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	if !strings.Contains(turn2.Text, "Timer set for") {
		t.Errorf("turn 2 Text = %q, want it to contain 'Timer set for'", turn2.Text)
	}
	if store.Len() != 0 {
		t.Errorf("store should clear after a non-follow-up result; got %d", store.Len())
	}

	// Turn 2's ToolCall should carry the previous state forward via
	// Context so the skill knows it's the second pass.
	if len(exec.calls) != 2 {
		t.Fatalf("expected 2 Execute calls; got %d", len(exec.calls))
	}
	if !strings.Contains(exec.calls[1].Context, "awaiting_duration=1") {
		t.Errorf("turn 2 Context should echo follow-up state; got %q", exec.calls[1].Context)
	}
	if exec.calls[1].Intent != shortcuts.IntentTimer {
		t.Errorf("turn 2 Intent = %v, want IntentTimer", exec.calls[1].Intent)
	}
}

func TestPipeline_MultiTurn_DifferentSessionsAreIsolated(t *testing.T) {
	results := []ToolResult{
		{Text: "How long?", Action: "respond", FollowupNeeded: true},
		{Text: "How long?", Action: "respond", FollowupNeeded: true},
		{Text: "Timer set for 5 minutes.", Action: "execute"},
	}
	p, _, store := newFakeMultiTurnPipeline(t, results)

	// Two different users open follow-up contexts.
	if _, err := p.Process(context.Background(), "set a timer", ProcessOpts{SessionKey: "alice"}); err != nil {
		t.Fatalf("alice turn 1: %v", err)
	}
	if _, err := p.Process(context.Background(), "set a timer", ProcessOpts{SessionKey: "bob"}); err != nil {
		t.Fatalf("bob turn 1: %v", err)
	}
	if store.Len() != 2 {
		t.Errorf("expected 2 separate contexts (alice + bob); got %d", store.Len())
	}

	// Alice completes — only her context clears.
	if _, err := p.Process(context.Background(), "5 minutes", ProcessOpts{SessionKey: "alice"}); err != nil {
		t.Fatalf("alice turn 2: %v", err)
	}
	if store.Len() != 1 {
		t.Errorf("alice's completion should leave bob untouched; got %d", store.Len())
	}
	if _, ok := store.Get("bob"); !ok {
		t.Error("bob's context must survive alice's completion")
	}
}

func TestPipeline_MultiTurn_DisabledWhenSessionKeyEmpty(t *testing.T) {
	results := []ToolResult{
		{Text: "How long?", Action: "respond", FollowupNeeded: true},
	}
	p, _, store := newFakeMultiTurnPipeline(t, results)

	if _, err := p.Process(context.Background(), "set a timer", ProcessOpts{}); err != nil {
		t.Fatalf("no-session turn: %v", err)
	}
	if store.Len() != 0 {
		t.Error("an empty SessionKey must not write to the store")
	}
}
