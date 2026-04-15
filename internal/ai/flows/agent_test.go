package flows

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/genkit"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
)

func TestBuildAgentSystemPrompt_German(t *testing.T) {
	p := buildAgentSystemPrompt(AgentInput{Locale: "de"})
	if !strings.Contains(p, "German") {
		t.Errorf("expected 'German' in prompt: %q", p)
	}
}

func TestBuildAgentSystemPrompt_English(t *testing.T) {
	p := buildAgentSystemPrompt(AgentInput{Locale: "en"})
	if !strings.Contains(p, "English") {
		t.Errorf("expected 'English' in prompt: %q", p)
	}
}

func TestBuildAgentSystemPrompt_WithSelection(t *testing.T) {
	p := buildAgentSystemPrompt(AgentInput{Selection: "selected text"})
	if !strings.Contains(p, "selected text") {
		t.Errorf("expected selection in prompt: %q", p)
	}
}

func TestBuildAgentSystemPrompt_WithLastTranscription(t *testing.T) {
	p := buildAgentSystemPrompt(AgentInput{LastTranscription: "previous"})
	if !strings.Contains(p, "previous") {
		t.Errorf("expected last transcription in prompt: %q", p)
	}
}

func TestBuildAgentUserPrompt(t *testing.T) {
	p := buildAgentUserPrompt(AgentInput{Utterance: "What time is it?"})
	if p != "What time is it?" {
		t.Errorf("user prompt = %q", p)
	}
}

func TestAgentFlow_EmptyUtterance(t *testing.T) {
	g := genkit.Init(context.Background())
	flow := DefineAgentFlow(g, nil)

	_, err := flow.Run(context.Background(), AgentInput{})
	if err == nil {
		t.Fatal("expected error for empty utterance")
	}
	if !strings.Contains(err.Error(), "empty utterance") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentFlow_NoModels(t *testing.T) {
	g := genkit.Init(context.Background())
	flow := DefineAgentFlow(g, nil)

	_, err := flow.Run(context.Background(), AgentInput{Utterance: "hello"})
	if err == nil {
		t.Fatal("expected error when no models configured")
	}
	if !strings.Contains(err.Error(), "no models") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentFlow_Integration(t *testing.T) {
	if os.Getenv("SPEECHKIT_AI_SMOKE") != "1" {
		t.Skip("set SPEECHKIT_AI_SMOKE=1 to run live AI smoke test")
	}

	groqKey := os.Getenv("GROQ_API_KEY")
	if groqKey == "" {
		t.Skip("GROQ_API_KEY not set")
	}

	rt, err := appai.Init(context.Background(), appai.Config{
		GroqAPIKey:     groqKey,
		GroqAgentModel: "llama-3.1-8b-instant",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	flow := DefineAgentFlow(rt.G, rt.AgentModels())
	result, err := flow.Run(context.Background(), AgentInput{
		Utterance: "Was ist 2 plus 2?",
		Locale:    "de",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Text == "" {
		t.Fatal("expected non-empty response")
	}
	if result.Action != "paste" {
		t.Errorf("action = %q, want paste", result.Action)
	}
	t.Logf("Agent: %s (action=%s)", result.Text, result.Action)
}
