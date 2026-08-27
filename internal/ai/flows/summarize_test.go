package flows

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/firebase/genkit/go/ai"
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

// A hung first model (e.g. a dead local llama-server) must not consume the
// whole summary budget: the per-model timeout has to cut it off so a later
// model still gets real time. Fails against the pre-fix chain, where every
// attempt shared the caller deadline.
func TestSummarizeFlow_PerModelTimeoutSkipsHungModel(t *testing.T) {
	prev := summarizePerModelTimeout
	summarizePerModelTimeout = 100 * time.Millisecond
	defer func() { summarizePerModelTimeout = prev }()

	g := genkit.Init(context.Background())
	supports := &ai.ModelOptions{Supports: &ai.ModelSupports{Multiturn: true, SystemRole: true}}
	hung := genkit.DefineModel(g, "test/hung", supports,
		func(ctx context.Context, _ *ai.ModelRequest, _ ai.ModelStreamCallback) (*ai.ModelResponse, error) {
			<-ctx.Done() // never answers; only the per-attempt deadline releases it
			return nil, ctx.Err()
		})
	healthy := genkit.DefineModel(g, "test/healthy", supports,
		func(_ context.Context, _ *ai.ModelRequest, _ ai.ModelStreamCallback) (*ai.ModelResponse, error) {
			return &ai.ModelResponse{Message: &ai.Message{
				Role:    ai.RoleModel,
				Content: []*ai.Part{ai.NewTextPart("healthy summary")},
			}}, nil
		})
	flow := DefineSummarizeFlow(g, []ai.Model{hung, healthy})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := flow.Run(ctx, SummarizeInput{Text: "some transcript"})
	if err != nil {
		t.Fatalf("summarize failed despite a healthy fallback model: %v", err)
	}
	if !strings.Contains(out, "healthy summary") {
		t.Fatalf("summary = %q, want the healthy model's output", out)
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
