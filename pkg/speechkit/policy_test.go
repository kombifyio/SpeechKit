package speechkit

import (
	"strings"
	"testing"
)

func TestRuntimePolicyFiltersFixedDictationProfile(t *testing.T) {
	profiles := FilterProviderProfiles(DefaultProviderProfiles(), RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
		FixedProfiles: map[Mode]string{
			ModeDictation: "stt.openai.whisper-1",
		},
	})

	if len(profiles) != 1 {
		t.Fatalf("profiles = %d, want 1: %#v", len(profiles), profiles)
	}
	if got := profiles[0].ID; got != "stt.openai.whisper-1" {
		t.Fatalf("profile ID = %q, want fixed profile", got)
	}
}

func TestRuntimePolicyHidesDisabledModeProfiles(t *testing.T) {
	profiles := FilterProviderProfiles(DefaultProviderProfiles(), RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
	})

	for _, profile := range profiles {
		if NormalizeMode(profile.Mode) != ModeDictation {
			t.Fatalf("profile %q mode = %q, want dictation only", profile.ID, profile.Mode)
		}
	}
}

func TestRuntimePolicyRejectsFallbackWhenDisabled(t *testing.T) {
	err := ValidateModeSettingsForPolicy(DefaultProviderProfiles(), ModeSettings{
		Dictation: DictationSetting{
			ModeSetting: ModeSetting{
				Enabled:           true,
				PrimaryProfileID:  "stt.local.whispercpp",
				FallbackProfileID: "stt.openai.whisper-1",
			},
		},
	}, RuntimePolicy{
		EnabledModes:   []Mode{ModeDictation},
		AllowFallbacks: false,
	})

	if err == nil || !strings.Contains(err.Error(), "fallback profile") {
		t.Fatalf("ValidateModeSettingsForPolicy() error = %v, want fallback rejection", err)
	}
}

func TestRuntimePolicyRejectsUnknownFixedProfile(t *testing.T) {
	err := ValidateRuntimePolicy(DefaultProviderProfiles(), RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
		FixedProfiles: map[Mode]string{
			ModeDictation: "missing.profile",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want not found", err)
	}
}

func TestRuntimePolicyRejectsProfileThatViolatesModeContract(t *testing.T) {
	profiles := append(DefaultProviderProfiles(), ProviderProfile{
		ID:           "stt.bad.llm",
		Mode:         ModeDictation,
		Name:         "Bad Dictation LLM",
		ProviderKind: ProviderKindDirectProvider,
		Capabilities: []Capability{CapabilityTranscription, CapabilityLLM},
	})

	err := ValidateRuntimePolicy(profiles, RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
		FixedProfiles: map[Mode]string{
			ModeDictation: "stt.bad.llm",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "cannot expose tools or LLM") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want mode contract error", err)
	}
}
