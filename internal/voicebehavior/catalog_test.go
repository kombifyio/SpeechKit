package voicebehavior

import (
	"strings"
	"testing"
)

func TestBuiltInCatalogMigratesExistingVoiceAgentPersonas(t *testing.T) {
	catalog := BuiltInCatalog()
	for _, id := range []string{
		DefaultID,
		BrainstormingCompanionID,
		HumorCompanionID,
		SupportCompanionID,
	} {
		persona, ok := catalog.Persona(id)
		if !ok {
			t.Fatalf("missing built-in persona %q", id)
		}
		if strings.TrimSpace(persona.DisplayName) == "" {
			t.Fatalf("persona %q has empty display name", id)
		}
	}

	brainstorming, _ := catalog.Persona(BrainstormingCompanionID)
	if brainstorming.DefaultRole != BrainstormingCompanionRoleID {
		t.Fatalf("brainstorming default role = %q, want %q", brainstorming.DefaultRole, BrainstormingCompanionRoleID)
	}
	if brainstorming.DefaultSequence != BrainstormingCompanionSequenceID {
		t.Fatalf("brainstorming default sequence = %q, want %q", brainstorming.DefaultSequence, BrainstormingCompanionSequenceID)
	}
}

func TestBuiltInCatalogIncludesWorkflowSequencesForNonDefaultPersonas(t *testing.T) {
	catalog := BuiltInCatalog()
	for _, id := range []string{
		BrainstormingCompanionSequenceID,
		HumorCompanionSequenceID,
		SupportCompanionSequenceID,
	} {
		sequence, ok := catalog.Sequence(id)
		if !ok {
			t.Fatalf("missing built-in sequence %q", id)
		}
		if len(sequence.Steps) < 3 {
			t.Fatalf("sequence %q has %d steps, want at least 3", id, len(sequence.Steps))
		}
		for _, step := range sequence.Steps {
			if strings.TrimSpace(step.Instruction) == "" {
				t.Fatalf("sequence %q step %q has empty instruction", id, step.ID)
			}
			if strings.TrimSpace(step.ExitCriteria) == "" {
				t.Fatalf("sequence %q step %q has empty exit criteria", id, step.ID)
			}
		}
	}
}

func TestResolveComposesRoleAndSequenceStep(t *testing.T) {
	catalog := BuiltInCatalog()

	resolved, err := catalog.Resolve(BrainstormingCompanionID, "", "", 1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PersonaID != BrainstormingCompanionID || resolved.RoleID != BrainstormingCompanionRoleID {
		t.Fatalf("unexpected resolved IDs: %+v", resolved)
	}
	if resolved.SequenceID != BrainstormingCompanionSequenceID || resolved.StepIndex != 1 || resolved.StepCount < 3 {
		t.Fatalf("unexpected sequence metadata: %+v", resolved)
	}
	if !strings.Contains(strings.ToLower(resolved.SystemPrompt), "blind spot") {
		t.Fatalf("system prompt missing role guidance: %q", resolved.SystemPrompt)
	}
	if !strings.Contains(resolved.SystemPrompt, resolved.StepInstruction) {
		t.Fatalf("system prompt missing step instruction: %q", resolved.SystemPrompt)
	}
}

func TestProfilesExposeDefaultSequences(t *testing.T) {
	profiles := BuiltInProfiles()
	byID := make(map[string]Profile, len(profiles))
	for _, profile := range profiles {
		byID[profile.ID] = profile
	}
	for _, id := range []string{BrainstormingCompanionID, HumorCompanionID, SupportCompanionID} {
		if strings.TrimSpace(byID[id].DefaultSequenceID) == "" {
			t.Fatalf("profile %q missing default sequence", id)
		}
	}
	if byID[DefaultID].DefaultSequenceID != "" {
		t.Fatalf("default profile sequence = %q, want empty", byID[DefaultID].DefaultSequenceID)
	}
}
