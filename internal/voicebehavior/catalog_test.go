package voicebehavior

import (
	"strings"
	"testing"
)

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
