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

func TestRuntimePolicyRejectsDisabledAndDisallowedSelections(t *testing.T) {
	profiles := DefaultProviderProfiles()

	err := ValidateRuntimePolicy(profiles, RuntimePolicy{
		EnabledModes:    []Mode{ModeDictation},
		AllowedProfiles: []string{"assist.openai.gpt-5.4"},
	})
	if err == nil || !strings.Contains(err.Error(), "disabled mode") {
		t.Fatalf("ValidateRuntimePolicy disabled allowed profile error = %v", err)
	}

	err = ValidateModeSettingsForPolicy(profiles, ModeSettings{
		Assist: AssistSetting{ModeSetting: ModeSetting{Enabled: true}},
	}, RuntimePolicy{EnabledModes: []Mode{ModeDictation}})
	if err == nil || !strings.Contains(err.Error(), "mode \"assist\" is disabled") {
		t.Fatalf("ValidateModeSettingsForPolicy disabled mode error = %v", err)
	}

	err = ValidateModeSettingsForPolicy(profiles, ModeSettings{
		Dictation: DictationSetting{ModeSetting: ModeSetting{Enabled: true, PrimaryProfileID: "stt.openai.whisper-1"}},
	}, RuntimePolicy{
		EnabledModes:    []Mode{ModeDictation},
		AllowedProfiles: []string{"stt.local.whispercpp"},
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("ValidateModeSettingsForPolicy disallowed profile error = %v", err)
	}
}

func TestRuntimePolicyRejectsFixedProfileModeAndAllowListMismatches(t *testing.T) {
	profiles := DefaultProviderProfiles()

	err := ValidateRuntimePolicy(profiles, RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
		FixedProfiles: map[Mode]string{
			Mode("bogus"): "stt.local.whispercpp",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("fixed unsupported mode error = %v", err)
	}

	err = ValidateRuntimePolicy(profiles, RuntimePolicy{
		EnabledModes: []Mode{ModeDictation},
		FixedProfiles: map[Mode]string{
			ModeDictation: "assist.openai.gpt-5.4",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not \"dictation\"") {
		t.Fatalf("fixed mode mismatch error = %v", err)
	}

	err = ValidateRuntimePolicy(profiles, RuntimePolicy{
		EnabledModes:    []Mode{ModeDictation},
		AllowedProfiles: []string{"stt.openai.whisper-1"},
		FixedProfiles: map[Mode]string{
			ModeDictation: "stt.local.whispercpp",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("fixed not allowed error = %v", err)
	}
}

func TestModeContractsAndRequiredCapabilitiesAreStable(t *testing.T) {
	contracts := DefaultModeContracts()
	if len(contracts) != 3 {
		t.Fatalf("contracts = %d, want 3", len(contracts))
	}
	for _, contract := range contracts {
		if contract.Mode == ModeNone || contract.Intelligence == "" || contract.Input == "" || contract.Output == "" {
			t.Fatalf("incomplete contract: %+v", contract)
		}
	}

	cases := map[Mode][]Capability{
		ModeDictation:  {CapabilityTranscription},
		ModeAssist:     {CapabilityLLM},
		ModeVoiceAgent: {CapabilityPipelineFallback, CapabilitySessionSummary},
		ModeNone:       nil,
	}
	for mode, want := range cases {
		got := RequiredCapabilities(mode, false)
		if len(got) != len(want) {
			t.Fatalf("RequiredCapabilities(%s) = %#v, want %#v", mode, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("RequiredCapabilities(%s)[%d] = %q, want %q", mode, i, got[i], want[i])
			}
		}
	}
	if got := RequiredCapabilities(ModeVoiceAgent, true); len(got) != 1 || got[0] != CapabilityRealtimeAudio {
		t.Fatalf("native realtime requirements = %#v", got)
	}
}
