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
