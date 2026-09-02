package speechkit_test

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func TestRuntimePolicyFiltersFixedDictationProfile(t *testing.T) {
	profiles := speechkit.FilterProviderProfiles(catalog.DefaultProviderProfiles(), speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
		FixedProfiles: map[speechkit.Mode]string{
			speechkit.ModeDictation: "stt.openai.whisper-1",
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
	profiles := speechkit.FilterProviderProfiles(catalog.DefaultProviderProfiles(), speechkit.RuntimePolicy{})
	seen := map[speechkit.Mode]bool{}
	for _, profile := range profiles {
		seen[speechkit.NormalizeMode(profile.Mode)] = true
	}
	for _, mode := range []speechkit.Mode{speechkit.ModeDictation, speechkit.ModeAssist, speechkit.ModeVoiceAgent, speechkit.ModeTTS} {
		if !seen[mode] {
			t.Fatalf("empty EnabledModes did not expose %q profiles", mode)
		}
	}
}

func TestRuntimePolicyHidesDisabledModeProfiles(t *testing.T) {
	profiles := speechkit.FilterProviderProfiles(catalog.DefaultProviderProfiles(), speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
	})

	for _, profile := range profiles {
		if speechkit.NormalizeMode(profile.Mode) != speechkit.ModeDictation {
			t.Fatalf("profile %q mode = %q, want dictation only", profile.ID, profile.Mode)
		}
	}
}

func TestRuntimePolicyNormalizesAllowedProfileAliases(t *testing.T) {
	profiles := speechkit.FilterProviderProfiles(catalog.DefaultProviderProfiles(), speechkit.RuntimePolicy{
		EnabledModes:    []speechkit.Mode{speechkit.ModeDictation},
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
	err := speechkit.ValidateModeSettingsForPolicy(catalog.DefaultProviderProfiles(), speechkit.ModeSettings{
		Dictation: speechkit.DictationSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           true,
				PrimaryProfileID:  "stt.local.whispercpp",
				FallbackProfileID: "stt.openai.whisper-1",
			},
		},
	}, speechkit.RuntimePolicy{
		EnabledModes:   []speechkit.Mode{speechkit.ModeDictation},
		AllowFallbacks: false,
	})

	if err == nil || !strings.Contains(err.Error(), "fallback profile") {
		t.Fatalf("ValidateModeSettingsForPolicy() error = %v, want fallback rejection", err)
	}
}

func TestRuntimePolicyAllowsFallbackWhenExplicitlyEnabledAndAllowed(t *testing.T) {
	err := speechkit.ValidateModeSettingsForPolicy(catalog.DefaultProviderProfiles(), speechkit.ModeSettings{
		Dictation: speechkit.DictationSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:           true,
				PrimaryProfileID:  "stt.google.chirp-3",
				FallbackProfileID: "stt.deepgram.nova-3",
			},
		},
	}, speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
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
	err := speechkit.ValidateModeSettingsForPolicy(catalog.DefaultProviderProfiles(), speechkit.ModeSettings{
		Assist: speechkit.AssistSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:          true,
				PrimaryProfileID: "assist.openai.gpt-5.4",
			},
		},
	}, speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
	})

	if err == nil || !strings.Contains(err.Error(), `mode "assist" is disabled`) {
		t.Fatalf("ValidateModeSettingsForPolicy() error = %v, want disabled mode rejection", err)
	}
}

func TestRuntimePolicyAcceptsLegacyGoogleSTTProfileID(t *testing.T) {
	err := speechkit.ValidateModeSettingsForPolicy(catalog.DefaultProviderProfiles(), speechkit.ModeSettings{
		Dictation: speechkit.DictationSetting{
			ModeSetting: speechkit.ModeSetting{
				Enabled:          true,
				PrimaryProfileID: "stt.google.chirp-3",
			},
		},
	}, speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
	})

	if err != nil {
		t.Fatalf("ValidateModeSettingsForPolicy() error = %v, want legacy Google profile accepted", err)
	}
}

func TestRuntimePolicyRejectsFixedProfileOutsideAllowedSet(t *testing.T) {
	err := speechkit.ValidateRuntimePolicy(catalog.DefaultProviderProfiles(), speechkit.RuntimePolicy{
		EnabledModes:    []speechkit.Mode{speechkit.ModeDictation},
		AllowedProfiles: []string{"stt.deepgram.nova-3"},
		FixedProfiles: map[speechkit.Mode]string{
			speechkit.ModeDictation: "stt.openai.whisper-1",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want fixed profile allowed-set rejection", err)
	}
}

func TestRuntimePolicyRejectsFixedProfileModeMismatch(t *testing.T) {
	err := speechkit.ValidateRuntimePolicy(catalog.DefaultProviderProfiles(), speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation, speechkit.ModeAssist},
		FixedProfiles: map[speechkit.Mode]string{
			speechkit.ModeDictation: "assist.openai.gpt-5.4",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "not \"dictation\"") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want mode mismatch rejection", err)
	}
}

func TestRuntimePolicyRejectsUnknownFixedProfile(t *testing.T) {
	err := speechkit.ValidateRuntimePolicy(catalog.DefaultProviderProfiles(), speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
		FixedProfiles: map[speechkit.Mode]string{
			speechkit.ModeDictation: "missing.profile",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want not found", err)
	}
}

func TestRuntimePolicyRejectsProfileThatViolatesModeContract(t *testing.T) {
	profiles := append(catalog.DefaultProviderProfiles(), speechkit.ProviderProfile{
		ID:           "stt.bad.llm",
		Mode:         speechkit.ModeDictation,
		Name:         "Bad Dictation LLM",
		ProviderKind: speechkit.ProviderKindDirectProvider,
		Capabilities: []speechkit.Capability{speechkit.CapabilityTranscription, speechkit.CapabilityLLM},
	})

	err := speechkit.ValidateRuntimePolicy(profiles, speechkit.RuntimePolicy{
		EnabledModes: []speechkit.Mode{speechkit.ModeDictation},
		FixedProfiles: map[speechkit.Mode]string{
			speechkit.ModeDictation: "stt.bad.llm",
		},
	})

	if err == nil || !strings.Contains(err.Error(), "cannot expose tools or LLM") {
		t.Fatalf("ValidateRuntimePolicy() error = %v, want mode contract error", err)
	}
}

func TestRuntimePolicyFilterDropsInvalidProfilesAndPreservesCatalogOrder(t *testing.T) {
	input := []speechkit.ProviderProfile{
		{
			ID:             "stt.first",
			Mode:           speechkit.ModeDictation,
			Name:           "First",
			ProviderKind:   speechkit.ProviderKindDirectProvider,
			ExecutionMode:  speechkit.ExecutionModeOpenAI,
			Capabilities:   []speechkit.Capability{speechkit.CapabilityTranscription, speechkit.CapabilitySTT},
			AllowInference: true,
		},
		{
			ID:             "stt.invalid.llm",
			Mode:           speechkit.ModeDictation,
			Name:           "Invalid",
			ProviderKind:   speechkit.ProviderKindDirectProvider,
			ExecutionMode:  speechkit.ExecutionModeOpenAI,
			Capabilities:   []speechkit.Capability{speechkit.CapabilityTranscription, speechkit.CapabilityLLM},
			AllowInference: true,
		},
		{
			ID:             "stt.second",
			Mode:           speechkit.ModeDictation,
			Name:           "Second",
			ProviderKind:   speechkit.ProviderKindDirectProvider,
			ExecutionMode:  speechkit.ExecutionModeDeepgram,
			Capabilities:   []speechkit.Capability{speechkit.CapabilityTranscription, speechkit.CapabilitySTT},
			AllowInference: true,
		},
	}

	profiles := speechkit.FilterProviderProfiles(input, speechkit.RuntimePolicy{EnabledModes: []speechkit.Mode{speechkit.ModeDictation}})

	if len(profiles) != 2 {
		t.Fatalf("profiles = %d, want 2: %#v", len(profiles), profiles)
	}
	if profiles[0].ID != "stt.first" || profiles[1].ID != "stt.second" {
		t.Fatalf("filtered profile order = %#v, want valid profiles in original order", profiles)
	}
}
