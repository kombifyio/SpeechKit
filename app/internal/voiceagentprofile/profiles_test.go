package voiceagentprofile

import (
	"strings"
	"testing"
)

func TestBuiltInProfilesExposeDefaultAndThreeTemplates(t *testing.T) {
	profiles := BuiltInProfiles()
	wantIDs := []string{
		DefaultID,
		BrainstormingCompanionID,
		HumorCompanionID,
		SupportCompanionID,
	}

	byID := make(map[string]Profile, len(profiles))
	for _, profile := range profiles {
		byID[profile.ID] = profile
		if strings.TrimSpace(profile.DisplayName) == "" {
			t.Fatalf("profile %q has empty display name", profile.ID)
		}
		if strings.TrimSpace(profile.Description) == "" {
			t.Fatalf("profile %q has empty description", profile.ID)
		}
	}

	for _, id := range wantIDs {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing built-in profile %q in %#v", id, profiles)
		}
	}
	if profile := byID[DefaultID]; strings.TrimSpace(profile.FrameworkPrompt) != "" {
		t.Fatalf("default profile framework prompt = %q, want empty to preserve current Voice Mode behavior", profile.FrameworkPrompt)
	}
	for _, id := range []string{BrainstormingCompanionID, HumorCompanionID, SupportCompanionID} {
		profile := byID[id]
		if strings.TrimSpace(profile.FrameworkPrompt) == "" {
			t.Fatalf("profile %q should define a framework prompt", id)
		}
		if strings.TrimSpace(profile.Voice) == "" {
			t.Fatalf("profile %q should define a voice", id)
		}
		if strings.TrimSpace(profile.DefaultSequenceID) == "" {
			t.Fatalf("profile %q should define a default sequence", id)
		}
	}
	if got := strings.ToLower(byID[BrainstormingCompanionID].FrameworkPrompt); !strings.Contains(got, "blind spot") {
		t.Fatalf("brainstorming prompt should ask for blind spots, got %q", byID[BrainstormingCompanionID].FrameworkPrompt)
	}
	if got := strings.ToLower(byID[HumorCompanionID].FrameworkPrompt); !strings.Contains(got, "humor") {
		t.Fatalf("humor prompt should be explicitly humorous, got %q", byID[HumorCompanionID].FrameworkPrompt)
	}
	if got := strings.ToLower(byID[SupportCompanionID].FrameworkPrompt); !strings.Contains(got, "feeling") {
		t.Fatalf("support prompt should include an emotional check-in, got %q", byID[SupportCompanionID].FrameworkPrompt)
	}
}

func TestResolveProfileNormalizesUnknownAndEmptyToDefault(t *testing.T) {
	if got := NormalizeID(""); got != DefaultID {
		t.Fatalf("NormalizeID(empty) = %q, want %q", got, DefaultID)
	}
	if got := NormalizeID("missing-profile"); got != DefaultID {
		t.Fatalf("NormalizeID(unknown) = %q, want %q", got, DefaultID)
	}
	if got := NormalizeID("  " + BrainstormingCompanionID + "  "); got != BrainstormingCompanionID {
		t.Fatalf("NormalizeID(brainstorming) = %q, want %q", got, BrainstormingCompanionID)
	}

	profile, ok := Resolve(BrainstormingCompanionID)
	if !ok {
		t.Fatal("Resolve(brainstorming) should find built-in profile")
	}
	if profile.ID != BrainstormingCompanionID {
		t.Fatalf("resolved profile ID = %q", profile.ID)
	}
}
