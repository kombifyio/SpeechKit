package voicebehavior

import (
	"errors"
	"strings"
	"testing"
)

// Audit 4.2: lift coverage from 0.24 -> ≥0.7. Original catalog_test.go
// covers Resolve happy-path only; this file adds the negative paths,
// the Profile/NormalizeID/ComposePrompt surface, and verifies the
// clone-isolation contract that the unexported clone* helpers exist
// to guarantee.

func TestCatalogResolveRejectsUnknownPersona(t *testing.T) {
	c := BuiltInCatalog()
	_, err := c.Resolve("nope_persona", BrainstormingCompanionRoleID, BrainstormingCompanionSequenceID, 0)
	if !errors.Is(err, ErrPersonaNotFound) {
		t.Fatalf("err = %v, want ErrPersonaNotFound", err)
	}
}

func TestCatalogResolveRejectsUnknownRole(t *testing.T) {
	c := BuiltInCatalog()
	_, err := c.Resolve(BrainstormingCompanionID, "nope_role", BrainstormingCompanionSequenceID, 0)
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestCatalogResolveRejectsUnknownSequence(t *testing.T) {
	c := BuiltInCatalog()
	_, err := c.Resolve(BrainstormingCompanionID, BrainstormingCompanionRoleID, "nope_seq", 0)
	if !errors.Is(err, ErrSequenceNotFound) {
		t.Fatalf("err = %v, want ErrSequenceNotFound", err)
	}
}

func TestCatalogResolveRejectsOutOfRangeStep(t *testing.T) {
	c := BuiltInCatalog()
	for _, idx := range []int{-1, 100} {
		_, err := c.Resolve(BrainstormingCompanionID, BrainstormingCompanionRoleID, BrainstormingCompanionSequenceID, idx)
		if !errors.Is(err, ErrStepNotFound) {
			t.Fatalf("step=%d err=%v, want ErrStepNotFound", idx, err)
		}
	}
}

func TestResolveProfileKnownAndUnknown(t *testing.T) {
	prof, ok := ResolveProfile(BrainstormingCompanionID)
	if !ok || prof.ID != BrainstormingCompanionID {
		t.Fatalf("ResolveProfile(known) = (%+v, %v)", prof, ok)
	}
	if _, ok := ResolveProfile("definitely-not-a-persona"); ok {
		t.Fatal("ResolveProfile(unknown) returned ok=true")
	}
	if _, ok := ResolveProfile(""); ok {
		t.Fatal("ResolveProfile('') returned ok=true")
	}
}

func TestNormalizeIDFallsBackToDefault(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{BrainstormingCompanionID, BrainstormingCompanionID},
		{"  " + HumorCompanionID + "  ", HumorCompanionID},
		{"unknown-id", DefaultID},
		{"", DefaultID},
		{"   ", DefaultID},
	}
	for _, c := range cases {
		if got := NormalizeID(c.in); got != c.want {
			t.Errorf("NormalizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestComposePromptCombinations(t *testing.T) {
	cases := []struct {
		role, stepID, stepInst, want string
	}{
		{"", "", "", ""},
		{"  role  ", "", "", "role"},
		{"role", "step1", "  do thing  ", "role\n\n[Current step: step1]\ndo thing"},
		{"", "step1", "do thing", "do thing"},
		{"role only", "step1", "  ", "role only"},
	}
	for _, c := range cases {
		got := ComposePrompt(c.role, c.stepID, c.stepInst)
		if got != c.want {
			t.Errorf("ComposePrompt(%q,%q,%q) = %q, want %q", c.role, c.stepID, c.stepInst, got, c.want)
		}
	}
}

func TestCatalogGettersAreCloneIsolated(t *testing.T) {
	c := BuiltInCatalog()
	p1, _ := c.Persona(BrainstormingCompanionID)
	p1.DisplayName = "mutated"
	p1.Tags = append(p1.Tags, "extra")
	p2, _ := c.Persona(BrainstormingCompanionID)
	if p2.DisplayName == "mutated" || strings.Contains(strings.Join(p2.Tags, ","), "extra") {
		t.Fatalf("Persona getter must return an isolated clone; second call saw mutation: %+v", p2)
	}

	r1, _ := c.Role(BrainstormingCompanionRoleID)
	r1.SystemPrompt = "mutated"
	r1.ToolAllowlist = append(r1.ToolAllowlist, "extra-tool")
	r2, _ := c.Role(BrainstormingCompanionRoleID)
	if r2.SystemPrompt == "mutated" || strings.Contains(strings.Join(r2.ToolAllowlist, ","), "extra-tool") {
		t.Fatalf("Role getter must return an isolated clone; second call saw mutation: %+v", r2)
	}

	s1, _ := c.Sequence(BrainstormingCompanionSequenceID)
	if len(s1.Steps) > 0 {
		s1.Steps[0].Instruction = "mutated"
	}
	s2, _ := c.Sequence(BrainstormingCompanionSequenceID)
	if len(s2.Steps) > 0 && s2.Steps[0].Instruction == "mutated" {
		t.Fatalf("Sequence getter must return an isolated clone; second call saw step mutation: %+v", s2.Steps[0])
	}
}
