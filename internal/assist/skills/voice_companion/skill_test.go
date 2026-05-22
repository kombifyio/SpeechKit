package voice_companion

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

type stubSkill struct {
	intent shortcuts.Intent
	result assist.ToolResult
	err    error
	called bool
}

func (s *stubSkill) Intent() shortcuts.Intent { return s.intent }

func (s *stubSkill) Execute(_ context.Context, _ assist.ToolCall) (assist.ToolResult, error) {
	s.called = true
	return s.result, s.err
}

type stubFallback struct {
	called bool
	result assist.ToolResult
	err    error
}

func (s *stubFallback) Execute(_ context.Context, _ assist.ToolCall) (assist.ToolResult, error) {
	s.called = true
	return s.result, s.err
}

func TestCompositeExecutor_DispatchesByIntent(t *testing.T) {
	time := &stubSkill{intent: shortcuts.IntentTime, result: assist.ToolResult{Text: "time-skill"}}
	math := &stubSkill{intent: shortcuts.IntentMath, result: assist.ToolResult{Text: "math-skill"}}
	exec := NewCompositeExecutor([]Skill{time, math}, nil)

	got, err := exec.Execute(context.Background(), assist.ToolCall{Intent: shortcuts.IntentMath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !math.called {
		t.Fatal("expected math skill to be called")
	}
	if time.called {
		t.Fatal("expected time skill NOT to be called")
	}
	if got.Text != "math-skill" {
		t.Fatalf("unexpected result text: %q", got.Text)
	}
}

func TestCompositeExecutor_FallbackOnUnknownIntent(t *testing.T) {
	time := &stubSkill{intent: shortcuts.IntentTime, result: assist.ToolResult{Text: "time"}}
	fallback := &stubFallback{result: assist.ToolResult{Text: "fallback"}}
	exec := NewCompositeExecutor([]Skill{time}, fallback)

	got, err := exec.Execute(context.Background(), assist.ToolCall{Intent: shortcuts.IntentCopyLast})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallback.called {
		t.Fatal("expected fallback to be called for unknown intent")
	}
	if got.Text != "fallback" {
		t.Fatalf("unexpected result: %q", got.Text)
	}
}

func TestCompositeExecutor_ErrorWhenNoSkillNoFallback(t *testing.T) {
	exec := NewCompositeExecutor([]Skill{}, nil)
	_, err := exec.Execute(context.Background(), assist.ToolCall{Intent: shortcuts.IntentTime})
	if err == nil {
		t.Fatal("expected error when no skill matches and no fallback configured")
	}
}

func TestCompositeExecutor_PropagatesSkillError(t *testing.T) {
	want := errors.New("skill failed")
	time := &stubSkill{intent: shortcuts.IntentTime, err: want}
	exec := NewCompositeExecutor([]Skill{time}, nil)

	_, err := exec.Execute(context.Background(), assist.ToolCall{Intent: shortcuts.IntentTime})
	if !errors.Is(err, want) {
		t.Fatalf("expected error %v, got %v", want, err)
	}
}

func TestCompositeExecutor_LastRegisteredWinsForSameIntent(t *testing.T) {
	first := &stubSkill{intent: shortcuts.IntentTime, result: assist.ToolResult{Text: "first"}}
	second := &stubSkill{intent: shortcuts.IntentTime, result: assist.ToolResult{Text: "second"}}
	exec := NewCompositeExecutor([]Skill{first, second}, nil)

	got, err := exec.Execute(context.Background(), assist.ToolCall{Intent: shortcuts.IntentTime})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "second" {
		t.Fatalf("expected second skill to win, got %q", got.Text)
	}
	if first.called {
		t.Fatal("expected first skill NOT to be called when second overrides")
	}
}

func TestCompositeExecutor_NilSkillsAndNoneIntentSkipped(t *testing.T) {
	bogus := &stubSkill{intent: shortcuts.IntentNone, result: assist.ToolResult{Text: "bogus"}}
	exec := NewCompositeExecutor([]Skill{nil, bogus}, nil)

	intents := exec.Intents()
	if len(intents) != 0 {
		t.Fatalf("expected zero intents, got %v", intents)
	}
}

func TestDefaultSkills_CoverPhase1aIntents(t *testing.T) {
	exec := NewCompositeExecutor(DefaultSkills(), nil)
	want := []shortcuts.Intent{shortcuts.IntentTime, shortcuts.IntentDate, shortcuts.IntentMath}
	for _, intent := range want {
		_, err := exec.Execute(context.Background(), assist.ToolCall{Intent: intent, Payload: "42"})
		if err != nil {
			t.Errorf("expected DefaultSkills to handle intent %q without error, got %v", intent, err)
		}
	}
}

func TestNormalizeLocale_Buckets(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", "en"},
		{"  ", "en"},
		{"de", "de"},
		{"DE", "de"},
		{"de-DE", "de"},
		{"de_AT", "de"},
		{"en", "en"},
		{"en-US", "en"},
		{"fr", "en"},
		{"es-ES", "en"},
	}
	for _, c := range cases {
		got := normalizeLocale(c.in)
		if got != c.want {
			t.Errorf("normalizeLocale(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
