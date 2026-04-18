package flows

import (
	"context"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/genkit"
)

func TestLangName_KnownLocales(t *testing.T) {
	cases := map[string]string{
		"de":    "German",
		"de-DE": "German",
		"fr":    "French",
		"fr-FR": "French",
		"es":    "Spanish",
		"es-ES": "Spanish",
		"it":    "Italian",
		"it-IT": "Italian",
		"pt":    "Portuguese",
		"pt-BR": "Portuguese",
		"ja":    "Japanese",
		"ja-JP": "Japanese",
		"ko":    "Korean",
		"ko-KR": "Korean",
		"zh":    "Chinese",
		"zh-CN": "Chinese",
	}
	for locale, want := range cases {
		if got := langName(locale); got != want {
			t.Errorf("langName(%q) = %q, want %q", locale, got, want)
		}
	}
}

func TestLangName_DefaultsToEnglish(t *testing.T) {
	for _, locale := range []string{"", "en", "en-US", "nl", "ru", "tr"} {
		if got := langName(locale); got != "English" {
			t.Errorf("langName(%q) = %q, want English", locale, got)
		}
	}
}

func TestBuildAssistSystemPrompt_LocaleAndFormatting(t *testing.T) {
	p := buildAssistSystemPrompt("de", AssistInput{Utterance: "hi"})
	if !strings.Contains(p, "German") {
		t.Errorf("prompt missing German: %q", p)
	}
	if !strings.Contains(p, "1-3 sentences") {
		t.Errorf("prompt missing concise-length instruction: %q", p)
	}
	if !strings.Contains(p, "Avoid markdown") {
		t.Errorf("prompt missing TTS formatting guidance: %q", p)
	}
}

func TestBuildAssistSystemPrompt_SelectionAppended(t *testing.T) {
	p := buildAssistSystemPrompt("en", AssistInput{
		Utterance: "make this better",
		Selection: "The quick brown fox",
	})
	if !strings.Contains(p, "The quick brown fox") {
		t.Errorf("prompt missing selection text: %q", p)
	}
	if !strings.Contains(p, "selected") {
		t.Errorf("prompt missing 'selected' marker: %q", p)
	}
}

func TestBuildAssistSystemPrompt_ContextAppended(t *testing.T) {
	p := buildAssistSystemPrompt("en", AssistInput{
		Utterance: "summarize",
		Context:   "User is in VS Code, editing a Go file",
	})
	if !strings.Contains(p, "VS Code") {
		t.Errorf("prompt missing context: %q", p)
	}
	if !strings.Contains(p, "Additional context") {
		t.Errorf("prompt missing context marker: %q", p)
	}
}

func TestBuildAssistSystemPrompt_OmitsEmptySelectionAndContext(t *testing.T) {
	p := buildAssistSystemPrompt("en", AssistInput{Utterance: "hi"})
	if strings.Contains(p, "selected") {
		t.Errorf("prompt should not mention selection when empty: %q", p)
	}
	if strings.Contains(p, "Additional context") {
		t.Errorf("prompt should not mention context when empty: %q", p)
	}
}

func TestAssistFlow_EmptyUtterance(t *testing.T) {
	g := genkit.Init(context.Background())
	flow := DefineAssistFlow(g, nil)

	_, err := flow.Run(context.Background(), AssistInput{Utterance: ""})
	if err == nil {
		t.Fatal("expected error for empty utterance")
	}
	if !strings.Contains(err.Error(), "empty utterance") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAssistFlow_NoModels(t *testing.T) {
	g := genkit.Init(context.Background())
	flow := DefineAssistFlow(g, nil)

	_, err := flow.Run(context.Background(), AssistInput{Utterance: "hello"})
	if err == nil {
		t.Fatal("expected error when no models configured")
	}
	if !strings.Contains(err.Error(), "no models") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAssistFlow_NoModelsWithLocaleDefault(t *testing.T) {
	// Even when Locale is empty, the flow should still proceed past the locale
	// default assignment and reach the "no models" branch. Guards the default
	// locale="en" assignment from accidentally short-circuiting.
	g := genkit.Init(context.Background())
	flow := DefineAssistFlow(g, nil)

	_, err := flow.Run(context.Background(), AssistInput{Utterance: "hi", Locale: ""})
	if err == nil || !strings.Contains(err.Error(), "no models") {
		t.Errorf("expected 'no models' error with empty locale, got %v", err)
	}
}
