package customize

import (
	"testing"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestBuildHintsFromWords(t *testing.T) {
	words := []speechcustomize.Word{
		{Term: "Kombify", Enabled: true},
		{Term: "kombify", Enabled: true},
		{Term: "AcmeOS", Enabled: true},
		{Term: "Disabled", Enabled: false},
	}
	if got, want := BuildPrompt(words), "Prefer these terms when transcribing: Kombify, AcmeOS."; got != want {
		t.Fatalf("BuildPrompt = %q, want %q", got, want)
	}
	if got := BuildKeyterms(words); len(got) != 2 || got[0] != "Kombify" || got[1] != "AcmeOS" {
		t.Fatalf("BuildKeyterms = %v", got)
	}
}

func TestBuildKeytermsIncludesWordAliases(t *testing.T) {
	got := BuildKeyterms([]speechcustomize.Word{
		{Term: "Kombify", SoundsLike: []string{"kombi fire", "Kombify"}, Enabled: true},
	})
	if len(got) != 2 || got[0] != "Kombify" || got[1] != "kombi fire" {
		t.Fatalf("BuildKeyterms aliases = %v", got)
	}
}

func TestBuildProviderBiasRoutesNativeKeyterms(t *testing.T) {
	bias := BuildProviderBias([]speechcustomize.Word{
		{Term: "Kombify", Enabled: true},
		{Term: "SpeechKit", Enabled: true},
	})
	for _, provider := range []string{"deepgram", "assemblyai", "google"} {
		values := bias.ByProvider[provider]
		if values == nil {
			t.Fatalf("missing provider bias for %s: %+v", provider, bias.ByProvider)
		}
		if got := values.StringList("keyterms"); len(got) != 2 || got[0] != "Kombify" || got[1] != "SpeechKit" {
			t.Fatalf("%s keyterms = %v", provider, got)
		}
		if !values.Bool("vocabulary_bias") {
			t.Fatalf("%s vocabulary_bias disabled", provider)
		}
	}
	if bias.Prompt == "" {
		t.Fatal("prompt fallback is empty")
	}
}

func TestBuildProviderBiasForVoiceAgentRoutesDeepgramKeyterms(t *testing.T) {
	bias := BuildProviderBiasForModality([]speechcustomize.Word{
		{Term: "Kombify", Enabled: true},
		{Term: "SpeechKit", Enabled: true},
	}, provideropts.ModalityVoiceAgent)

	values := bias.ByProvider["deepgram"]
	if values == nil {
		t.Fatalf("missing deepgram voice agent bias: %+v", bias.ByProvider)
	}
	if got := values.StringList(provideropts.OptionKeyterms); len(got) != 2 || got[0] != "Kombify" || got[1] != "SpeechKit" {
		t.Fatalf("deepgram voice agent keyterms = %v", got)
	}
	if _, ok := bias.ByProvider["google"]; ok {
		t.Fatalf("google voice agent bias = %+v, want no structured keyterms", bias.ByProvider["google"])
	}
}

func TestApplySnippetTemplateAndCommandActions(t *testing.T) {
	replacements := []speechcustomize.Replacement{
		{
			ID:      "snippet",
			Kind:    speechcustomize.KindSnippet,
			Stage:   speechcustomize.StagePostSTT,
			Match:   speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "sig", WordBoundary: true},
			Output:  speechcustomize.ReplacementOutput{Template: "Best,\n{{.Payload.name}}", Payload: map[string]any{"name": "Marcel"}},
			Enabled: true,
		},
		{
			ID:      "command",
			Kind:    speechcustomize.KindCommand,
			Stage:   speechcustomize.StagePostSTT,
			Match:   speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "send note", WordBoundary: true},
			Output:  speechcustomize.ReplacementOutput{Intent: "note.send", Payload: map[string]any{"surface": "quick_note"}},
			Enabled: true,
		},
	}
	got, err := Apply("sig and send note", replacements, speechcustomize.StagePostSTT)
	if err != nil {
		t.Fatalf("Apply = %v", err)
	}
	if got.Text != "Best,\nMarcel and send note" {
		t.Fatalf("Apply.Text = %q", got.Text)
	}
	if len(got.Actions) != 1 {
		t.Fatalf("actions = %+v", got.Actions)
	}
	action := got.Actions[0]
	if action.Intent != "note.send" || action.MatchedText != "send note" || action.Payload["surface"] != "quick_note" {
		t.Fatalf("action = %+v", action)
	}
	publicActions := PublicActions(got.Actions)
	if len(publicActions) != 1 || publicActions[0].Intent != "note.send" || publicActions[0].MatchedText != "send note" {
		t.Fatalf("public actions = %+v", publicActions)
	}
}

func TestApplyReplacementPriorityAndBoundaries(t *testing.T) {
	replacements := []speechcustomize.Replacement{
		{
			ID:       "low",
			Kind:     speechcustomize.KindSubstitution,
			Stage:    speechcustomize.StagePostSTT,
			Priority: 1,
			Match:    speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "kombi", WordBoundary: true},
			Output:   speechcustomize.ReplacementOutput{Text: "wrong"},
			Enabled:  true,
		},
		{
			ID:       "high",
			Kind:     speechcustomize.KindSubstitution,
			Stage:    speechcustomize.StagePostSTT,
			Priority: 10,
			Match:    speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "kombi fire", WordBoundary: true},
			Output:   speechcustomize.ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
		},
		{
			ID:      "disabled",
			Kind:    speechcustomize.KindSubstitution,
			Stage:   speechcustomize.StagePostSTT,
			Match:   speechcustomize.Match{Type: speechcustomize.MatchPhrase, Pattern: "tomorrow"},
			Output:  speechcustomize.ReplacementOutput{Text: "today"},
			Enabled: false,
		},
	}
	got, err := Apply("call kombi fire tomorrow, not kombinator", replacements, speechcustomize.StagePostSTT)
	if err != nil {
		t.Fatalf("Apply = %v", err)
	}
	if got.Text != "call Kombify tomorrow, not kombinator" {
		t.Fatalf("Apply.Text = %q", got.Text)
	}
	if len(got.Matches) != 1 || got.Matches[0].ReplacementID != "high" || got.Matches[0].Count != 1 {
		t.Fatalf("matches = %+v", got.Matches)
	}
}

func TestDictionaryProjection(t *testing.T) {
	entries := []DictionaryEntry{
		{Spoken: "kombi fire", Canonical: "Kombify", Language: "de-DE", Source: "settings", Enabled: true},
		{Spoken: "AcmeOS", Canonical: "AcmeOS", Language: "de", Source: "settings", Enabled: true},
	}
	words := WordsFromDictionary(entries)
	replacements := ReplacementsFromDictionary(entries)
	if len(words) != 2 {
		t.Fatalf("words = %+v", words)
	}
	if len(replacements) != 1 {
		t.Fatalf("replacements = %+v", replacements)
	}
	projected := DictionaryFromSet(Set{Words: words, Replacements: replacements})
	if len(projected) != 2 {
		t.Fatalf("projected dictionary = %+v", projected)
	}
}
