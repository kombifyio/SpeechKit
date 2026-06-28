//go:build linux

package persona

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func TestLoadSeedsIncludesBuiltInVoiceAgentProfiles(t *testing.T) {
	reg := NewRegistry()

	LoadSeeds(reg, &config.Config{})

	for _, id := range []string{
		voiceagentprofile.DefaultID,
		voiceagentprofile.BrainstormingCompanionID,
		voiceagentprofile.HumorCompanionID,
		voiceagentprofile.SupportCompanionID,
	} {
		if _, err := reg.GetPersona(id); err != nil {
			t.Fatalf("built-in persona %q not loaded: %v", id, err)
		}
	}

	defaultResolved, err := reg.Resolve(voiceagentprofile.DefaultID, "", "", 0)
	if err != nil {
		t.Fatalf("resolve default persona: %v", err)
	}
	if defaultResolved.SystemPrompt != "" {
		t.Fatalf("default persona system prompt = %q, want empty current behavior", defaultResolved.SystemPrompt)
	}

	brainstormingResolved, err := reg.Resolve(voiceagentprofile.BrainstormingCompanionID, "", "", 0)
	if err != nil {
		t.Fatalf("resolve brainstorming persona: %v", err)
	}
	if strings.TrimSpace(brainstormingResolved.Voice) == "" {
		t.Fatal("brainstorming persona should resolve a voice")
	}
	if strings.TrimSpace(brainstormingResolved.SequenceID) == "" {
		t.Fatal("brainstorming persona should resolve a default sequence")
	}
	if strings.TrimSpace(brainstormingResolved.StepID) == "" {
		t.Fatal("brainstorming persona should resolve an initial sequence step")
	}
	if !strings.Contains(strings.ToLower(brainstormingResolved.SystemPrompt), "blind spot") {
		t.Fatalf("brainstorming system prompt = %q, want blind spot guidance", brainstormingResolved.SystemPrompt)
	}
}

func TestLoadSeedsSkipsInvalidEntriesAndKeepsValidSeeds(t *testing.T) {
	reg := NewRegistry()

	notes := LoadSeeds(reg, &config.Config{
		Personas: []config.PersonaConfig{
			{ID: "", DisplayName: "invalid persona"},
			{ID: "host", DisplayName: "Host", DefaultRole: "moderator", DefaultSequence: "meeting"},
		},
		Roles: []config.RoleConfig{
			{ID: "invalid-role", DisplayName: "Invalid"},
			{ID: "moderator", DisplayName: "Moderator", SystemPrompt: "Run the meeting."},
		},
		Sequences: []config.SequenceConfig{
			{ID: "invalid-sequence", DisplayName: "Invalid"},
			{
				ID:          "meeting",
				DisplayName: "Meeting",
				Steps: []config.SequenceStepConfig{
					{ID: "open", Instruction: "Open the meeting."},
				},
			},
		},
	})

	if _, err := reg.GetPersona("host"); err != nil {
		t.Fatalf("valid persona missing: %v", err)
	}
	if _, err := reg.GetRole("moderator"); err != nil {
		t.Fatalf("valid role missing: %v", err)
	}
	if _, err := reg.GetSequence("meeting"); err != nil {
		t.Fatalf("valid sequence missing: %v", err)
	}

	resolved, err := reg.Resolve("host", "", "", 0)
	if err != nil {
		t.Fatalf("resolve valid seed chain: %v", err)
	}
	if resolved.RoleID != "moderator" || resolved.SequenceID != "meeting" || resolved.StepID != "open" {
		t.Fatalf("resolved valid seed chain = %+v", resolved)
	}

	joined := strings.Join(notes, "\n")
	for _, want := range []string{
		"persona seed skipped:",
		"role seed skipped: invalid-role",
		"sequence seed skipped: invalid-sequence",
		"persona seeded: host",
		"role seeded: moderator",
		"sequence seeded: meeting",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("notes missing %q:\n%s", want, joined)
		}
	}
}
