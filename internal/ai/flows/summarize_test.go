package flows

import (
	"context"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/genkit"
)

func TestBuildSummarizeSystemPrompt_German(t *testing.T) {
	for _, locale := range []string{"de", "de-DE"} {
		p := buildSummarizeSystemPrompt(locale)
		if !strings.Contains(p, "German") {
			t.Errorf("locale %q: expected 'German' in prompt, got %q", locale, p)
		}
	}
}

func TestBuildSummarizeSystemPrompt_English(t *testing.T) {
	for _, locale := range []string{"en", "en-US", "", "fr"} {
		p := buildSummarizeSystemPrompt(locale)
		if !strings.Contains(p, "English") {
			t.Errorf("locale %q: expected 'English' in prompt, got %q", locale, p)
		}
	}
}

func TestBuildSummarizeUserPrompt_WithInstruction(t *testing.T) {
	p := buildSummarizeUserPrompt(SummarizeInput{
		Text:        "Some text",
		Instruction: "Make it shorter",
	})
	if !strings.Contains(p, "Make it shorter") {
		t.Errorf("expected instruction in prompt: %q", p)
	}
	if !strings.Contains(p, "Some text") {
		t.Errorf("expected text in prompt: %q", p)
	}
}

func TestBuildSummarizeUserPrompt_NoInstruction(t *testing.T) {
	p := buildSummarizeUserPrompt(SummarizeInput{Text: "Some text"})
	if !strings.Contains(p, "Summarize") {
		t.Errorf("expected 'Summarize' in prompt: %q", p)
	}
	if !strings.Contains(p, "Some text") {
		t.Errorf("expected text in prompt: %q", p)
	}
}

func TestSummarizeFlow_EmptyText(t *testing.T) {
	g := genkit.Init(context.Background())
	flow := DefineSummarizeFlow(g, nil)

	_, err := flow.Run(context.Background(), SummarizeInput{Text: ""})
	if err == nil {
		t.Fatal("expected error for empty text")
	}
	if !strings.Contains(err.Error(), "empty text") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSummarizeFlow_NoModels(t *testing.T) {
	g := genkit.Init(context.Background())
	flow := DefineSummarizeFlow(g, nil)

	_, err := flow.Run(context.Background(), SummarizeInput{Text: "hello"})
	if err == nil {
		t.Fatal("expected error when no models configured")
	}
	if !strings.Contains(err.Error(), "no models") {
		t.Errorf("unexpected error: %v", err)
	}
}
