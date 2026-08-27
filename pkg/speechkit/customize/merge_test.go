package customize

import "testing"

func TestMergeWordsCollapsesDuplicateTermsAndUnionsAliases(t *testing.T) {
	merged := MergeWords([]Word{
		{ID: "legacy_dictionary_word_1", Term: "Kombify", Language: "de", SoundsLike: []string{"kombi fire"}, Enabled: true, UsageCount: 2},
		{ID: "legacy_dictionary_word_2", Term: "kombify", Language: "de-DE", SoundsLike: []string{"combi fy", "Kombify"}, Enabled: false, UsageCount: 1},
		{Term: "AcmeOS", Language: "de", Enabled: true},
		{Term: "  ", Language: "de"},
	})
	if len(merged) != 2 {
		t.Fatalf("merged len = %d, want 2 unique terms", len(merged))
	}
	kombify := merged[0]
	if kombify.Term != "Kombify" {
		t.Fatalf("canonical term = %q, want Kombify", kombify.Term)
	}
	if !kombify.Enabled {
		t.Fatal("merged word should stay enabled")
	}
	if kombify.UsageCount != 3 {
		t.Fatalf("usage_count = %d, want 3", kombify.UsageCount)
	}
	if got := stringsJoin(kombify.SoundsLike); got != "kombi fire,combi fy" {
		t.Fatalf("sounds_like = %q, want kombi fire,combi fy", got)
	}
	if kombify.ID == "legacy_dictionary_word_1" || kombify.ID == "legacy_dictionary_word_2" {
		t.Fatalf("merged word kept a legacy id: %q", kombify.ID)
	}
}

func TestMergeReplacementsKeepsSharedOutputAndCollapsesExactDuplicates(t *testing.T) {
	merged := MergeReplacements([]Replacement{
		{
			Kind:     KindSynonym,
			Language: "de",
			Stage:    StagePostSTT,
			Match:    Match{Type: MatchSpokenAlias, Pattern: "kombi fire", WordBoundary: true},
			Output:   ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
		},
		{
			Kind:     KindSynonym,
			Language: "de",
			Stage:    StagePostSTT,
			Match:    Match{Type: MatchSpokenAlias, Pattern: "combi fy", WordBoundary: true},
			Output:   ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
		},
		{
			Kind:     KindSynonym,
			Language: "de",
			Stage:    StagePostSTT,
			Match:    Match{Type: MatchSpokenAlias, Pattern: "kombi fire", WordBoundary: true},
			Output:   ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
		},
	})
	if len(merged) != 2 {
		t.Fatalf("merged len = %d, want 2 distinct spoken forms", len(merged))
	}
	seen := map[string]string{}
	for _, replacement := range merged {
		seen[replacement.Match.Pattern] = replacement.Output.Text
		if replacement.Output.Text != "Kombify" {
			t.Fatalf("output = %q, want Kombify", replacement.Output.Text)
		}
	}
	if seen["kombi fire"] != "Kombify" || seen["combi fy"] != "Kombify" {
		t.Fatalf("shared-output aliases = %+v", seen)
	}
}

func TestSynonymReplacementUsesSpokenAliasIdentity(t *testing.T) {
	replacement := SynonymReplacement("Kombify", "kombi fire", "de-DE", "")
	if replacement.Kind != KindSynonym {
		t.Fatalf("kind = %q", replacement.Kind)
	}
	if replacement.Match.Type != MatchSpokenAlias {
		t.Fatalf("match type = %q", replacement.Match.Type)
	}
	if replacement.Language != "de" {
		t.Fatalf("language = %q", replacement.Language)
	}
	if replacement.ID == "" {
		t.Fatal("expected stable id")
	}
	if !hasMode(replacement.Modes, ModeVoiceAgent) {
		t.Fatalf("modes = %v, want voice_agent included", replacement.Modes)
	}
}

func TestFoldAndMaterializeKeepPunctuationAsExtra(t *testing.T) {
	words := []Word{{Term: "Kombify", Language: "de", Enabled: true}}
	replacements := []Replacement{
		{
			Kind:     KindSynonym,
			Language: "de",
			Stage:    StagePostSTT,
			Match:    Match{Type: MatchSpokenAlias, Pattern: "kombi fire", WordBoundary: true},
			Output:   ReplacementOutput{Text: "Kombify"},
			Enabled:  true,
		},
		{
			Kind:     KindSubstitution,
			Language: "de",
			Stage:    StagePostSTT,
			Match:    Match{Type: MatchPhrase, Pattern: "punkt", WordBoundary: true},
			Output:   ReplacementOutput{Text: "."},
			Enabled:  true,
		},
	}
	folded, extras := FoldVocabulary(words, replacements)
	if len(folded) != 1 || stringsJoin(folded[0].SoundsLike) != "kombi fire" {
		t.Fatalf("folded = %+v", folded)
	}
	if len(extras) != 1 || extras[0].Output.Text != "." {
		t.Fatalf("extras = %+v", extras)
	}
	merged, materialized := MaterializeVocabulary(folded, extras)
	if len(merged) != 1 {
		t.Fatalf("merged words = %+v", merged)
	}
	seenAlias, seenDot := false, false
	for _, replacement := range materialized {
		if replacement.Match.Pattern == "kombi fire" && replacement.Output.Text == "Kombify" {
			seenAlias = true
		}
		if replacement.Match.Pattern == "punkt" && replacement.Output.Text == "." {
			seenDot = true
		}
	}
	if !seenAlias || !seenDot {
		t.Fatalf("materialized = %+v", materialized)
	}
}

func hasMode(modes []Mode, want Mode) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}

func stringsJoin(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ","
		}
		out += value
	}
	return out
}
