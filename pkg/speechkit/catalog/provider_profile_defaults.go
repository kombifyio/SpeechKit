package catalog

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// ProviderProfileWithDefaults returns a copy with framework-standard provider
// metadata filled in. Explicit profile metadata wins; missing provider,
// credential, and transport fields are derived from the canonical provider id,
// execution mode, and mode capabilities.
func ProviderProfileWithDefaults(profile speechkit.ProviderProfile) speechkit.ProviderProfile {
	// Mode and Modality say the same thing from two angles. Callers set
	// whichever one they think in; this fills the other.
	if profile.Modality == "" {
		profile.Modality = speechkit.ModalityForMode(profile.Mode)
	}
	if speechkit.NormalizeMode(profile.Mode) == speechkit.ModeNone {
		profile.Mode = speechkit.ModeForModality(profile.Modality)
	}
	provider := ProviderIDForProfile(profile)
	if strings.TrimSpace(profile.Provider) == "" {
		profile.Provider = provider
	} else {
		profile.Provider = NormalizeProviderID(profile.Provider)
	}
	if strings.TrimSpace(profile.AuthRequirement) == "" {
		profile.AuthRequirement = DefaultProviderAuthRequirement(profile)
	}
	if strings.TrimSpace(profile.Transport) == "" {
		profile.Transport = DefaultProviderTransport(profile)
	}
	return profile
}

// DefaultProviderAuthRequirement describes the credential class a host must
// satisfy before a provider profile can run. It is intentionally semantic:
// hosts map the value to their own env vars or secret stores.
func DefaultProviderAuthRequirement(profile speechkit.ProviderProfile) string {
	if value := strings.TrimSpace(profile.AuthRequirement); value != "" {
		return value
	}
	switch profile.ExecutionMode {
	case speechkit.ExecutionModeLocal:
		if profile.ProviderKind == speechkit.ProviderKindLocalBuiltIn {
			return ProviderAuthHostDependencies
		}
		return ProviderAuthNone
	case speechkit.ExecutionModeOllama:
		return ProviderAuthNone
	case speechkit.ExecutionModeSelfHostedHTTP:
		return ProviderAuthOptionalAPIKey
	case speechkit.ExecutionModeHFRouted:
		return ProviderAuthToken
	case speechkit.ExecutionModeOpenAI, speechkit.ExecutionModeGroq, speechkit.ExecutionModeGoogle, speechkit.ExecutionModeDeepgram,
		speechkit.ExecutionModeAssemblyAI, speechkit.ExecutionModeOpenRouter, speechkit.ExecutionModeFoundry:
		return ProviderAuthAPIKey
	default:
		return ""
	}
}

// DefaultProviderTransport exposes the dominant runtime transport class for a
// profile. Native realtime providers use websocket; cascaded voice providers
// use pipeline; batch/provider APIs use HTTPS/HTTP/local.
func DefaultProviderTransport(profile speechkit.ProviderProfile) string {
	if value := strings.TrimSpace(profile.Transport); value != "" {
		return value
	}
	if speechkit.NormalizeMode(profile.Mode) == speechkit.ModeVoiceAgent {
		if profile.HasCapability(speechkit.CapabilityRealtimeAudio) {
			return ProviderTransportWebSocket
		}
		if profile.HasCapability(speechkit.CapabilityPipelineFallback) {
			return ProviderTransportPipeline
		}
	}
	switch profile.ExecutionMode {
	case speechkit.ExecutionModeLocal:
		return ProviderTransportLocal
	case speechkit.ExecutionModeOllama, speechkit.ExecutionModeSelfHostedHTTP:
		return ProviderTransportHTTP
	case speechkit.ExecutionModeHFRouted, speechkit.ExecutionModeOpenAI, speechkit.ExecutionModeGroq, speechkit.ExecutionModeGoogle,
		speechkit.ExecutionModeDeepgram, speechkit.ExecutionModeAssemblyAI, speechkit.ExecutionModeOpenRouter, speechkit.ExecutionModeFoundry:
		return ProviderTransportHTTPS
	default:
		return ""
	}
}

// ProviderProfileRequiresCredential reports whether a host must supply a
// secret before the profile can run: true for the api_key and token
// requirements (and any unrecognised value), false for none,
// host_dependencies, optional_api_key and an empty requirement.
func ProviderProfileRequiresCredential(profile speechkit.ProviderProfile) bool {
	switch DefaultProviderAuthRequirement(profile) {
	case "", ProviderAuthNone, ProviderAuthHostDependencies, ProviderAuthOptionalAPIKey:
		return false
	default:
		return true
	}
}

// ProviderCredentialTarget returns the canonical provider id under which a
// host stores the profile's credential, or "" when the profile needs none.
// Every profile of a provider shares the target, so a key entered once
// serves all of its modes. The one exception is Google dictation: Cloud
// Speech-to-Text needs its own key ("google_stt"), separate from the Gemini
// API key that serves Gemini Live and Cloud Text-to-Speech.
func ProviderCredentialTarget(profile speechkit.ProviderProfile) string {
	if !ProviderProfileRequiresCredential(profile) {
		return ""
	}
	provider := ProviderIDForProfile(profile)
	if provider == "google" && speechkit.NormalizeMode(profile.Mode) == speechkit.ModeDictation {
		return "google_stt"
	}
	return provider
}
