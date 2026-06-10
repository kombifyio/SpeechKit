package customize

import (
	"testing"

	speechcustomize "github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
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
