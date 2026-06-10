package customize

import "testing"

func TestValidateWord(t *testing.T) {
	if err := ValidateWord(Word{Term: "Kombify"}); err != nil {
		t.Fatalf("ValidateWord valid = %v", err)
	}
	if err := ValidateWord(Word{}); err == nil {
		t.Fatal("ValidateWord empty term error = nil")
	}
}

func TestValidateReplacement(t *testing.T) {
	valid := Replacement{
		Kind:  KindSubstitution,
		Stage: StagePostSTT,
		Match: Match{Type: MatchPhrase, Pattern: "kombi fire"},
		Output: ReplacementOutput{
			Text: "Kombify",
		},
		Modes: []Mode{ModeDictation, ModeAssist},
	}
	if err := ValidateReplacement(valid); err != nil {
		t.Fatalf("ValidateReplacement valid = %v", err)
	}
	for name, replacement := range map[string]Replacement{
		"kind":  {Kind: "bad", Stage: StagePostSTT, Match: valid.Match, Output: valid.Output},
		"stage": {Kind: KindSubstitution, Stage: "bad", Match: valid.Match, Output: valid.Output},
		"match": {Kind: KindSubstitution, Stage: StagePostSTT, Match: Match{Type: "bad", Pattern: "x"}, Output: valid.Output},
		"mode":  {Kind: KindSubstitution, Stage: StagePostSTT, Match: valid.Match, Output: valid.Output, Modes: []Mode{"bad"}},
		"empty": {Kind: KindSubstitution, Stage: StagePostSTT, Match: Match{Type: MatchPhrase}, Output: valid.Output},
	} {
		if err := ValidateReplacement(replacement); err == nil {
			t.Fatalf("%s: ValidateReplacement error = nil", name)
		}
	}
}

func TestPackRoundTripValidation(t *testing.T) {
	pack := Pack{
		SchemaVersion: PackSchemaVersion,
		Words:         []Word{{Term: "Kombify"}},
		Replacements: []Replacement{{
			Kind:   KindSynonym,
			Stage:  StagePostSTT,
			Match:  Match{Type: MatchPhrase, Pattern: "quby"},
			Output: ReplacementOutput{Text: "Quby"},
		}},
	}
	if err := ValidatePack(pack); err != nil {
		t.Fatalf("ValidatePack = %v", err)
	}
	pack.SchemaVersion = "future"
	if err := ValidatePack(pack); err == nil {
		t.Fatal("ValidatePack future schema error = nil")
	}
}

func TestWithDefaultsStableID(t *testing.T) {
	a := WithDefaultsWord(Word{Term: "Kombify", Language: "de-DE"})
	b := WithDefaultsWord(Word{Term: "Kombify", Language: "de"})
	if a.ID == "" || a.ID != b.ID {
		t.Fatalf("stable word IDs = %q / %q", a.ID, b.ID)
	}
	if a.Language != "de" {
		t.Fatalf("Language = %q, want de", a.Language)
	}
}
