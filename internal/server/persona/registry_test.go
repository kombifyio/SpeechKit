//go:build linux

package persona

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRegistry_UpsertPersona(t *testing.T) {
	r := NewRegistry()
	p, err := r.UpsertPersona(Persona{ID: "default", DisplayName: "Default"})
	if err != nil {
		t.Fatalf("UpsertPersona: %v", err)
	}
	if p.ID != "default" || p.Source != "store" || p.CreatedAt.IsZero() {
		t.Fatalf("unexpected persona: %+v", p)
	}
}

func TestRegistry_UpsertRejectsMissingID(t *testing.T) {
	r := NewRegistry()
	if _, err := r.UpsertPersona(Persona{DisplayName: "X"}); err == nil {
		t.Fatalf("expected validation error for missing ID")
	}
}

func TestRegistry_UpsertPreservesCreatedAt(t *testing.T) {
	r := NewRegistry()
	now := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	later := now.Add(1 * time.Hour)
	clock := &mutClock{t: now}
	r.WithClock(clock.Now)

	first, _ := r.UpsertPersona(Persona{ID: "p", DisplayName: "P"})

	clock.Set(later)
	second, _ := r.UpsertPersona(Persona{ID: "p", DisplayName: "P v2"})

	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("CreatedAt should not change on update; got %v → %v", first.CreatedAt, second.CreatedAt)
	}
	if !second.UpdatedAt.Equal(later) {
		t.Fatalf("UpdatedAt should advance; got %v want %v", second.UpdatedAt, later)
	}
}

func TestRegistry_ListPersonasSorted(t *testing.T) {
	r := NewRegistry()
	_, _ = r.UpsertPersona(Persona{ID: "b", DisplayName: "B"})
	_, _ = r.UpsertPersona(Persona{ID: "a", DisplayName: "A"})
	list := r.ListPersonas()
	if len(list) != 2 || list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("unsorted list: %+v", list)
	}
}

func TestRegistry_DeletePersonaMissing(t *testing.T) {
	r := NewRegistry()
	if err := r.DeletePersona("nope"); !errors.Is(err, ErrPersonaNotFound) {
		t.Fatalf("expected ErrPersonaNotFound, got %v", err)
	}
}

func TestRegistry_UpsertRole(t *testing.T) {
	r := NewRegistry()
	role, err := r.UpsertRole(Role{ID: "helpful", DisplayName: "Helpful", SystemPrompt: "be nice"})
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if role.Source != "store" {
		t.Fatalf("expected Source=store; got %q", role.Source)
	}
}

func TestRegistry_RoleRequiresSystemPrompt(t *testing.T) {
	r := NewRegistry()
	_, err := r.UpsertRole(Role{ID: "x", DisplayName: "X"})
	if err == nil || !strings.Contains(err.Error(), "system_prompt") {
		t.Fatalf("expected system_prompt validation error; got %v", err)
	}
}

func TestRegistry_UpsertSequence(t *testing.T) {
	r := NewRegistry()
	seq, err := r.UpsertSequence(Sequence{
		ID:          "flow",
		DisplayName: "Flow",
		Steps: []SequenceStep{
			{ID: "greet", Instruction: "say hi"},
			{ID: "ask", Instruction: "ask the thing"},
		},
	})
	if err != nil {
		t.Fatalf("UpsertSequence: %v", err)
	}
	if len(seq.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(seq.Steps))
	}
}

func TestRegistry_SequenceRejectsEmptySteps(t *testing.T) {
	r := NewRegistry()
	_, err := r.UpsertSequence(Sequence{ID: "flow", DisplayName: "Flow"})
	if err == nil {
		t.Fatalf("expected error for sequence with zero steps")
	}
}

func TestRegistry_SequenceRejectsStepMissingInstruction(t *testing.T) {
	r := NewRegistry()
	_, err := r.UpsertSequence(Sequence{
		ID: "flow", DisplayName: "Flow",
		Steps: []SequenceStep{{ID: "greet" /* no instruction */}},
	})
	if err == nil {
		t.Fatalf("expected error for step missing instruction")
	}
}

func TestRegistry_ResolveFullChain(t *testing.T) {
	r := NewRegistry()
	_, _ = r.UpsertPersona(Persona{
		ID: "host", DisplayName: "Host", Voice: "Kore", Locale: "de",
		DefaultRole: "discovery",
	})
	_, _ = r.UpsertRole(Role{
		ID: "discovery", DisplayName: "Discovery",
		SystemPrompt:               "Run a structured discovery conversation.",
		AutomaticActivityDetection: true,
		VADStartSensitivity:        "medium", VADEndSensitivity: "medium",
	})
	_, _ = r.UpsertSequence(Sequence{
		ID: "call", DisplayName: "Call",
		Steps: []SequenceStep{
			{ID: "greet", Instruction: "Greet the caller."},
			{ID: "ask", Instruction: "Ask needs."},
		},
	})

	resolved, err := r.Resolve("host", "", "call", 1)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.PersonaID != "host" || resolved.RoleID != "discovery" || resolved.SequenceID != "call" {
		t.Fatalf("unexpected resolved IDs: %+v", resolved)
	}
	if resolved.Voice != "Kore" || resolved.Locale != "de" {
		t.Fatalf("persona defaults lost: voice=%q locale=%q", resolved.Voice, resolved.Locale)
	}
	if !strings.Contains(resolved.SystemPrompt, "Discovery conversation") && !strings.Contains(resolved.SystemPrompt, "Ask needs") {
		t.Fatalf("system prompt missing step instruction; got %q", resolved.SystemPrompt)
	}
	if resolved.CurrentStep == nil || resolved.CurrentStep.ID != "ask" {
		t.Fatalf("expected step index 1 = 'ask'; got %+v", resolved.CurrentStep)
	}
	if !resolved.AutomaticVAD || resolved.StartSensitivity != "medium" {
		t.Fatalf("role fields not propagated: %+v", resolved)
	}
}

func TestRegistry_ResolveMissingPersona(t *testing.T) {
	r := NewRegistry()
	_, err := r.Resolve("missing", "", "", 0)
	if !errors.Is(err, ErrPersonaNotFound) {
		t.Fatalf("expected ErrPersonaNotFound, got %v", err)
	}
}

func TestRegistry_ResolveMissingRole(t *testing.T) {
	r := NewRegistry()
	_, _ = r.UpsertPersona(Persona{ID: "host", DisplayName: "Host", DefaultRole: "ghost"})
	_, err := r.Resolve("host", "", "", 0)
	if !errors.Is(err, ErrRoleNotFound) {
		t.Fatalf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestRegistry_ResolveMissingSequence(t *testing.T) {
	r := NewRegistry()
	_, _ = r.UpsertPersona(Persona{ID: "host", DisplayName: "Host", DefaultRole: "x"})
	_, _ = r.UpsertRole(Role{ID: "x", DisplayName: "X", SystemPrompt: "..."})
	_, err := r.Resolve("host", "", "ghost-seq", 0)
	if !errors.Is(err, ErrSequenceNotFound) {
		t.Fatalf("expected ErrSequenceNotFound, got %v", err)
	}
}

func TestRegistry_ResolveStepOutOfRange(t *testing.T) {
	r := NewRegistry()
	_, _ = r.UpsertPersona(Persona{ID: "host", DisplayName: "Host", DefaultRole: "x"})
	_, _ = r.UpsertRole(Role{ID: "x", DisplayName: "X", SystemPrompt: "..."})
	_, _ = r.UpsertSequence(Sequence{
		ID: "s", DisplayName: "S",
		Steps: []SequenceStep{{ID: "a", Instruction: "a"}},
	})
	_, err := r.Resolve("host", "", "s", 9)
	if !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("expected ErrStepNotFound, got %v", err)
	}
}

func TestRegistry_CloneIsolation(t *testing.T) {
	r := NewRegistry()
	original, _ := r.UpsertPersona(Persona{
		ID: "x", DisplayName: "X",
		Tags:     []string{"a"},
		Metadata: map[string]string{"k": "v"},
	})
	// Mutate caller's copy and verify the registry is untouched.
	original.Tags[0] = "TAMPERED"
	original.Metadata["k"] = "TAMPERED"
	fresh, _ := r.GetPersona("x")
	if fresh.Tags[0] == "TAMPERED" {
		t.Fatalf("registry leaked Tags slice")
	}
	if fresh.Metadata["k"] == "TAMPERED" {
		t.Fatalf("registry leaked Metadata map")
	}
}

func TestRegistry_CombinePrompts(t *testing.T) {
	prompt := combinePrompts("You are helpful.", &SequenceStep{ID: "greet", Instruction: "Say hi."})
	if !strings.Contains(prompt, "You are helpful.") || !strings.Contains(prompt, "Say hi.") || !strings.Contains(prompt, "[Current step: greet]") {
		t.Fatalf("combined prompt missing expected parts: %q", prompt)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

type mutClock struct {
	t time.Time
}

func (c *mutClock) Now() time.Time  { return c.t }
func (c *mutClock) Set(t time.Time) { c.t = t }
