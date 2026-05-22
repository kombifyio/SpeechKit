package voice_companion

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestMathSkill_IntentIsMath(t *testing.T) {
	if NewMathSkill().Intent() != shortcuts.IntentMath {
		t.Errorf("expected IntentMath")
	}
}

func TestMathSkill_DigitForm(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"15 * 8", "The result is 120."},
		{"100 + 25", "The result is 125."},
		{"100 - 75", "The result is 25."},
		{"10 / 4", "The result is 2.50."},
	}
	skill := NewMathSkill()
	for _, c := range cases {
		got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: c.expr, Locale: "en"})
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.expr, err)
			continue
		}
		if got.Text != c.want {
			t.Errorf("%q: got %q, want %q", c.expr, got.Text, c.want)
		}
	}
}

func TestMathSkill_GermanWordOperators(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"15 mal 8", "Das Ergebnis ist 120."},
		{"100 plus 25", "Das Ergebnis ist 125."},
		{"100 minus 75", "Das Ergebnis ist 25."},
		{"100 durch 4", "Das Ergebnis ist 25."},
		{"100 geteilt durch 4", "Das Ergebnis ist 25."},
		{"3 multipliziert mit 7", "Das Ergebnis ist 21."},
	}
	skill := NewMathSkill()
	for _, c := range cases {
		got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: c.expr, Locale: "de"})
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.expr, err)
			continue
		}
		if got.Text != c.want {
			t.Errorf("%q: got %q, want %q", c.expr, got.Text, c.want)
		}
	}
}

func TestMathSkill_EnglishWordOperators(t *testing.T) {
	skill := NewMathSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "15 times 8", Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "The result is 120." {
		t.Errorf("Text = %q", got.Text)
	}

	got, _ = skill.Execute(context.Background(), assist.ToolCall{Payload: "100 divided by 4", Locale: "en"})
	if got.Text != "The result is 25." {
		t.Errorf("divided by: Text = %q", got.Text)
	}
}

func TestMathSkill_DecimalCommaNormalised(t *testing.T) {
	skill := NewMathSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "3,5 mal 2", Locale: "de"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Text != "Das Ergebnis ist 7." {
		t.Errorf("Text = %q, want 'Das Ergebnis ist 7.'", got.Text)
	}
}

func TestMathSkill_DecimalResultDE_UsesComma(t *testing.T) {
	skill := NewMathSkill()
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Payload: "10 / 4", Locale: "de"})
	if got.Text != "Das Ergebnis ist 2,50." {
		t.Errorf("DE decimal: Text = %q, want 'Das Ergebnis ist 2,50.'", got.Text)
	}
}

func TestMathSkill_UnaryNumberPasses(t *testing.T) {
	skill := NewMathSkill()
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Payload: "42", Locale: "en"})
	if got.Text != "The result is 42." {
		t.Errorf("unary: Text = %q", got.Text)
	}
}

func TestMathSkill_DivisionByZeroIsSilent(t *testing.T) {
	skill := NewMathSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "10 / 0", Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "silent" {
		t.Errorf("div-by-zero should be silent, got Action=%q", got.Action)
	}
}

func TestMathSkill_UnparseableExpressionIsSilent(t *testing.T) {
	skill := NewMathSkill()
	got, err := skill.Execute(context.Background(), assist.ToolCall{Payload: "what is the meaning of life", Locale: "en"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Action != "silent" {
		t.Errorf("non-math should be silent, got Action=%q", got.Action)
	}
}

func TestMathSkill_EmptyPayloadFallsBackToTranscript(t *testing.T) {
	skill := NewMathSkill()
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Transcript: "15 mal 8", Locale: "de"})
	if got.Text != "Das Ergebnis ist 120." {
		t.Errorf("transcript fallback: Text = %q", got.Text)
	}
}

func TestMathSkill_EmptyPayloadAndTranscriptIsSilent(t *testing.T) {
	skill := NewMathSkill()
	got, _ := skill.Execute(context.Background(), assist.ToolCall{Locale: "en"})
	if got.Action != "silent" {
		t.Errorf("empty: Action = %q, want silent", got.Action)
	}
}

func TestEvaluateExpression_Exports(t *testing.T) {
	// Public surface smoke test — make sure the exported parser stays
	// callable from future skills (timer-duration parsing, etc.).
	v, ok := EvaluateExpression("15 mal 8")
	if !ok || v != 120 {
		t.Errorf("EvaluateExpression(15 mal 8) = (%v, %v), want (120, true)", v, ok)
	}
	v, ok = EvaluateExpression("bogus")
	if ok {
		t.Errorf("EvaluateExpression(bogus) = (%v, true), want false", v)
	}
}
