package live

import (
	"errors"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestResolveProviderIntentSelectsProviderWithRequiredNativeWords(t *testing.T) {
	t.Parallel()
	plan, err := ResolveProviderIntent(ProviderIntent{
		RequiredCapabilities: []LiveCapabilityFlag{
			LiveCapabilityRealtimeAudio,
			LiveCapabilityToolCalling,
			LiveCapabilityNativeKeyterms,
		},
		PreferredCapabilities: []LiveCapabilityFlag{LiveCapabilitySessionResume},
		RequiredOptions:       []provideropts.OptionID{provideropts.OptionKeyterms},
		Locale:                "de-DE",
		SelectionPolicy: ProviderSelectionPolicy{
			PreferredProviders: []string{"assemblyai", "deepgram"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveProviderIntent: %v", err)
	}
	if plan.Provider != "assemblyai" || plan.ProfileID != "realtime.assemblyai.voice-agent" || plan.Model != "assemblyai-voice-agent" {
		t.Fatalf("plan = %+v", plan)
	}
	if got := plan.LiveConfig(); got.Provider != "assemblyai" || got.Model == "" {
		t.Fatalf("LiveConfig = %+v", got)
	}
	if !strings.Contains(plan.String(), "assemblyai") {
		t.Fatalf("String = %q", plan.String())
	}
}

func TestResolveProviderIntentHonorsPreferredProviderOrder(t *testing.T) {
	t.Parallel()
	plan, err := ResolveProviderIntent(ProviderIntent{
		RequiredCapabilities: []LiveCapabilityFlag{LiveCapabilityRealtimeAudio, LiveCapabilityToolCalling},
		SelectionPolicy: ProviderSelectionPolicy{
			PreferredProviders: []string{"openai", "google"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveProviderIntent: %v", err)
	}
	if plan.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", plan.Provider)
	}
}

func TestResolveProviderIntentUsesCustomDescriptorWithoutBuiltInProvider(t *testing.T) {
	t.Parallel()
	descriptors := []ProviderDescriptor{
		{
			Provider:    "acme",
			DisplayName: "Acme Realtime",
			ProfileID:   "realtime.acme.voice",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityRealtimeAudio,
				LiveCapabilityToolCalling,
				LiveCapabilityLanguageHints,
			},
			Models: []LiveModelDescriptor{
				{
					Provider:  "acme",
					ModelID:   "acme-live-1",
					Name:      "Acme Live 1",
					Lifecycle: "ga",
					Default:   true,
					SourceURL: "https://example.test/acme-live",
				},
			},
			SupportedLocales: []string{"de", "en"},
			NativeOptions:    []provideropts.OptionID{provideropts.OptionLanguageHints},
			AuthRequirement:  "api_key",
			Transport:        "websocket",
			EvidenceURL:      "https://example.test/acme-live",
		},
	}

	plan, err := ResolveProviderIntent(ProviderIntent{
		Provider:             "acme",
		RequiredCapabilities: []LiveCapabilityFlag{LiveCapabilityRealtimeAudio},
		RequiredOptions:      []provideropts.OptionID{provideropts.OptionLanguageHints},
		Locale:               "de-DE",
		SelectionPolicy: ProviderSelectionPolicy{
			ModelLifecycle: ModelLifecycleRequireGA,
		},
	}, descriptors)
	if err != nil {
		t.Fatalf("ResolveProviderIntent: %v", err)
	}
	if plan.Provider != "acme" || plan.ProfileID != "realtime.acme.voice" || plan.Model != "acme-live-1" {
		t.Fatalf("plan = %+v, want custom provider descriptor selected", plan)
	}
	if !capabilityFlagsContain(plan.MatchedRequiredCapabilities, LiveCapabilityRealtimeAudio) {
		t.Fatalf("matched required capabilities = %v, want realtime audio", plan.MatchedRequiredCapabilities)
	}
	if !optionIDsContain(plan.MatchedRequiredOptions, provideropts.OptionLanguageHints) {
		t.Fatalf("matched required options = %v, want language hints", plan.MatchedRequiredOptions)
	}
	if plan.AuthRequirement != "api_key" || plan.Transport != "websocket" {
		t.Fatalf("plan diagnostics = auth %q transport %q", plan.AuthRequirement, plan.Transport)
	}
}

func TestResolveProviderIntentReportsMissingRequiredCapabilities(t *testing.T) {
	t.Parallel()
	_, err := ResolveProviderIntent(ProviderIntent{
		Provider:             "openai",
		PrivacyRedaction:     true,
		RequiredCapabilities: []LiveCapabilityFlag{LiveCapabilityVoiceFocus},
	}, nil)
	var intentErr *ProviderIntentError
	if !errors.As(err, &intentErr) {
		t.Fatalf("error = %T %[1]v, want ProviderIntentError", err)
	}
	if !errors.Is(err, ErrNoMatchingProvider) {
		t.Fatalf("error should unwrap ErrNoMatchingProvider: %v", err)
	}
	got := strings.Join([]string{
		joinCapabilityFlags(intentErr.MissingRequiredCapabilities),
	}, ",")
	if !strings.Contains(got, string(LiveCapabilityPrivacyRedaction)) ||
		!strings.Contains(got, string(LiveCapabilityVoiceFocus)) {
		t.Fatalf("missing capabilities = %v", intentErr.MissingRequiredCapabilities)
	}
	if len(intentErr.RejectedProviders) == 0 || intentErr.RejectedProviders[0].FallbackKind != FallbackKindCapabilityMissing {
		t.Fatalf("rejections = %+v, want capability_missing", intentErr.RejectedProviders)
	}
	if intentErr.RejectedProviders[0].AuthRequirement == "" || intentErr.RejectedProviders[0].EvidenceURL == "" {
		t.Fatalf("rejection missing provider diagnostics: %+v", intentErr.RejectedProviders[0])
	}
}

func TestResolveProviderIntentRequiresGAWhenRequested(t *testing.T) {
	t.Parallel()
	plan, err := ResolveProviderIntent(ProviderIntent{
		Provider:             "openai",
		RequiredCapabilities: []LiveCapabilityFlag{LiveCapabilityRealtimeAudio},
		SelectionPolicy:      ProviderSelectionPolicy{ModelLifecycle: ModelLifecycleRequireGA},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveProviderIntent: %v", err)
	}
	if plan.ModelDescriptor.Lifecycle != "ga" {
		t.Fatalf("lifecycle = %q", plan.ModelDescriptor.Lifecycle)
	}
}

func TestResolveProviderIntentReportsCrossProviderFallback(t *testing.T) {
	t.Parallel()
	plan, err := ResolveProviderIntent(ProviderIntent{
		RequiredCapabilities: []LiveCapabilityFlag{
			LiveCapabilityRealtimeAudio,
			LiveCapabilityNativeKeyterms,
		},
		RequiredOptions: []provideropts.OptionID{provideropts.OptionKeyterms},
		SelectionPolicy: ProviderSelectionPolicy{
			PreferredProviders: []string{"openai", "assemblyai"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveProviderIntent: %v", err)
	}
	if plan.Provider != "assemblyai" {
		t.Fatalf("provider = %q, want assemblyai", plan.Provider)
	}
	if plan.SelectedFallbackKind != FallbackKindCrossProvider {
		t.Fatalf("SelectedFallbackKind = %q, want cross_provider", plan.SelectedFallbackKind)
	}
	if len(plan.RejectedProviders) == 0 {
		t.Fatalf("expected rejected preferred provider diagnostics")
	}
	var sawOpenAI bool
	for _, rejection := range plan.RejectedProviders {
		if rejection.Provider == "openai" {
			sawOpenAI = true
			if rejection.FallbackKind != FallbackKindCapabilityMissing ||
				!capabilityFlagsContain(rejection.MissingRequiredCapabilities, LiveCapabilityNativeKeyterms) ||
				!optionIDsContain(rejection.MissingRequiredOptions, provideropts.OptionKeyterms) {
				t.Fatalf("openai rejection = %+v", rejection)
			}
			if rejection.AuthRequirement == "" || rejection.Transport == "" || rejection.ModelLifecycle == "" {
				t.Fatalf("openai rejection missing diagnostics: %+v", rejection)
			}
		}
	}
	if !sawOpenAI {
		t.Fatalf("rejections = %+v, want openai", plan.RejectedProviders)
	}
}

func TestResolveProviderIntentAddsSameProviderModelFallback(t *testing.T) {
	t.Parallel()
	plan, err := ResolveProviderIntent(ProviderIntent{
		Provider:             "deepgram",
		RequiredCapabilities: []LiveCapabilityFlag{LiveCapabilityRealtimeAudio},
	}, nil)
	if err != nil {
		t.Fatalf("ResolveProviderIntent: %v", err)
	}
	if !fallbacksContain(plan.Fallbacks, FallbackKindSameProviderModel, "deepgram", "flux-general-en") {
		t.Fatalf("fallbacks = %+v, want deepgram flux-general-en model fallback", plan.Fallbacks)
	}
	if cfg := plan.LiveConfig(); cfg.FallbackModel != "flux-general-en" {
		t.Fatalf("LiveConfig.FallbackModel = %q, want flux-general-en", cfg.FallbackModel)
	}
}

func TestResolveProviderIntentReportsCascadedFallbackCandidate(t *testing.T) {
	t.Parallel()
	descriptors := []ProviderDescriptor{
		{
			Provider:    "openai",
			DisplayName: "OpenAI Realtime",
			ProfileID:   "realtime.openai.gpt-realtime-2",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityTranscript,
			},
			Models: []LiveModelDescriptor{{
				Provider:  "openai",
				ModelID:   "gpt-realtime-2",
				Name:      "GPT Realtime 2",
				Lifecycle: "ga",
				Default:   true,
				SourceURL: "https://platform.openai.com/docs/guides/realtime",
			}},
			AuthRequirement: "api_key",
			Transport:       "websocket",
			EvidenceURL:     "https://platform.openai.com/docs/guides/realtime",
		},
		{
			Provider:    "cascaded",
			DisplayName: "Cascaded Voice Agent",
			ProfileID:   "voice_agent.cascaded.pipeline",
			Capabilities: []LiveCapabilityFlag{
				LiveCapabilityTranscript,
			},
			Models: []LiveModelDescriptor{{
				Provider:  "cascaded",
				ModelID:   "local-cascaded",
				Name:      "Local cascaded pipeline",
				Lifecycle: "ga",
				Default:   true,
				SourceURL: "https://github.com/kombifyio/SpeechKit/tree/main/pkg/speechkit/voiceagent/cascaded",
			}},
			AuthRequirement: "host_dependencies",
			Transport:       "pipeline",
			EvidenceURL:     "https://github.com/kombifyio/SpeechKit/tree/main/pkg/speechkit/voiceagent/cascaded",
		},
	}
	plan, err := ResolveProviderIntent(ProviderIntent{
		RequiredCapabilities: []LiveCapabilityFlag{LiveCapabilityTranscript},
		SelectionPolicy: ProviderSelectionPolicy{
			PreferredProviders: []string{"openai"},
		},
	}, descriptors)
	if err != nil {
		t.Fatalf("ResolveProviderIntent: %v", err)
	}
	if plan.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", plan.Provider)
	}
	if !fallbacksContain(plan.Fallbacks, FallbackKindCascaded, "cascaded", "local-cascaded") {
		t.Fatalf("fallbacks = %+v, want cascaded fallback", plan.Fallbacks)
	}
}

func capabilityFlagsContain(values []LiveCapabilityFlag, want LiveCapabilityFlag) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func optionIDsContain(values []provideropts.OptionID, want provideropts.OptionID) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func fallbacksContain(values []ProviderFallback, kind ProviderFallbackKind, provider, model string) bool {
	for _, value := range values {
		if value.Kind == kind && value.Provider == provider && value.Model == model {
			return true
		}
	}
	return false
}
