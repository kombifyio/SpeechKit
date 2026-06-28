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

func TestRuntimePolicyEmptyEnabledModesMeansAllFrameworkModes(t *testing.T) {
	profiles := FilterProviderProfiles(DefaultProviderProfiles(), RuntimePolicy{})
	seen := map[Mode]bool{}
	for _, profile := range profiles {
		seen[NormalizeMode(profile.Mode)] = true
	}
	for _, mode := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent, ModeTTS} {
		if !seen[mode] {
			t.Fatalf("empty EnabledModes did not expose %q profiles", mode)
		}
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

func TestRuntimePolicyNormalizesAllowedProfileAliases(t *testing.T) {
	profiles := FilterProviderProfiles(DefaultProviderProfiles(), RuntimePolicy{
		EnabledModes:    []Mode{ModeDictation},
		AllowedProfiles: []string{"stt.google.chirp-3"},
	})

	if len(profiles) != 1 {
		t.Fatalf("profiles = %d, want 1: %#v", len(profiles), profiles)
	}
	if got := profiles[0].ID; got != "stt.google.latest-long" {
		t.Fatalf("profile ID = %q, want normalized Google STT profile", got)
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

func TestRuntimePolicyAllowsFallbackWhenExplicitlyEnabledAndAllowed(t *testing.T) {
	err := ValidateModeSettingsForPolicy(DefaultProviderProfiles(), ModeSettings{
		Dictation: DictationSetting{
			ModeSetting: ModeSetting{
				Enabled:           true,
				PrimaryProfileID:  "stt.google.chirp-3",
				FallbackProfileID: "stt.deepgram.nova-3",
			},
		},
	}, RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
		AllowedProfiles: []string{
			"stt.google.latest-long",
			"stt.deepgram.nova-3",
		},
		AllowFallbacks: true,
	})

	if err != nil {
		t.Fatalf("ValidateModeSettingsForPolicy() error = %v, want fallback allowed", err)
	}
}

func TestRuntimePolicyRejectsEnabledDisabledModeSettings(t *testing.T) {
	err := ValidateModeSettingsForPolicy(DefaultProviderProfiles(), ModeSettings{
		Assist: AssistSetting{
			ModeSetting: ModeSetting{
				Enabled:          true,
				PrimaryProfileID: "assist.openai.gpt-5.4",
			},
		},
	}, RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
	})

	if err == nil || !strings.Contains(err.Error(), `mode "assist" is disabled`) {
		t.Fatalf("ValidateModeSettingsForPolicy() error = %v, want disabled mode rejection", err)
	}
}

func TestRuntimePolicyAcceptsLegacyGoogleSTTProfileID(t *testing.T) {
	err := ValidateModeSettingsForPolicy(DefaultProviderProfiles(), ModeSettings{
		Dictation: DictationSetting{
			ModeSetting: ModeSetting{
				Enabled:          true,
				PrimaryProfileID: "stt.google.chirp-3",
			},
		},
	}, RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
	})

	if err != nil {
		t.Fatalf("ValidateModeSettingsForPolicy() error = %v, want legacy Google profile accepted", err)
	}
}

func TestRuntimePolicyRejectsFixedProfileOutsideAllowedSet(t *testing.T) {
	err := ValidateRuntimePolicy(DefaultProviderProfiles(), RuntimePolicy{
		EnabledModes:    []Mode{ModeDictation},
		AllowedProfiles: []string{"stt.deepgram.nova-3"},
		FixedProfiles: map[Mode]string{
			ModeDictation: "stt.openai.whisper-1",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want fixed profile allowed-set rejection", err)
	}
}

func TestRuntimePolicyRejectsFixedProfileModeMismatch(t *testing.T) {
	err := ValidateRuntimePolicy(DefaultProviderProfiles(), RuntimePolicy{
		EnabledModes: []Mode{ModeDictation, ModeAssist},
		FixedProfiles: map[Mode]string{
			ModeDictation: "assist.openai.gpt-5.4",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "not \"dictation\"") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want mode mismatch rejection", err)
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

func TestRuntimePolicyFilterDropsInvalidProfilesAndPreservesCatalogOrder(t *testing.T) {
	input := []ProviderProfile{
		{
			ID:             "stt.first",
			Mode:           ModeDictation,
			Name:           "First",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeOpenAI,
			Capabilities:   []Capability{CapabilityTranscription, CapabilitySTT},
			AllowInference: true,
		},
		{
			ID:             "stt.invalid.llm",
			Mode:           ModeDictation,
			Name:           "Invalid",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeOpenAI,
			Capabilities:   []Capability{CapabilityTranscription, CapabilityLLM},
			AllowInference: true,
		},
		{
			ID:             "stt.second",
			Mode:           ModeDictation,
			Name:           "Second",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeDeepgram,
			Capabilities:   []Capability{CapabilityTranscription, CapabilitySTT},
			AllowInference: true,
		},
	}

	profiles := FilterProviderProfiles(input, RuntimePolicy{EnabledModes: []Mode{ModeDictation}})

	if len(profiles) != 2 {
		t.Fatalf("profiles = %d, want 2: %#v", len(profiles), profiles)
	}
	if profiles[0].ID != "stt.first" || profiles[1].ID != "stt.second" {
		t.Fatalf("filtered profile order = %#v, want valid profiles in original order", profiles)
	}
}
