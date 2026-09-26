package catalog

import (
	"sort"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// DefaultLocalBuiltInLLMModel is the Hugging Face GGUF reference
// (repository:quantization) the Local Built-in Assist profile pins as its
// model id; hosts use it as the default model of their SpeechKit-managed
// llama.cpp runtime.
const DefaultLocalBuiltInLLMModel = "ggml-org/gemma-4-E2B-it-GGUF:Q8_0"

// DefaultProviderProfiles returns the built-in framework provider catalog for
// the three strict SpeechKit modes. The Windows desktop host adapts this
// public catalog into its internal runtime model; the catalog itself belongs to
// the reusable framework layer.
func DefaultProviderProfiles() []speechkit.ProviderProfile {
	profiles := make([]speechkit.ProviderProfile, 0)
	profiles = append(profiles, dictationProviderProfiles()...)
	profiles = append(profiles, assistProviderProfiles()...)
	profiles = append(profiles, voiceAgentProviderProfiles()...)
	profiles = append(profiles, ttsProviderProfiles()...)
	for i := range profiles {
		profiles[i] = ProviderProfileWithDefaults(profiles[i])
	}
	return profiles
}

// ProfilesForMode returns the built-in profiles for mode (normalised first),
// in catalog order; nil when no profile belongs to it.
func ProfilesForMode(mode speechkit.Mode) []speechkit.ProviderProfile {
	mode = speechkit.NormalizeMode(mode)
	var profiles []speechkit.ProviderProfile
	for _, profile := range DefaultProviderProfiles() {
		if speechkit.NormalizeMode(profile.Mode) == mode {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

// ProviderKindsForMode returns the distinct provider kinds the built-in
// profiles of mode cover, in the fixed product order local built-in, local
// provider, cloud provider, direct provider. [ValidateDefaultCatalog] expects
// all four for every strict mode.
func ProviderKindsForMode(mode speechkit.Mode) []speechkit.ProviderKind {
	seen := map[speechkit.ProviderKind]bool{}
	for _, profile := range ProfilesForMode(mode) {
		seen[profile.ProviderKind] = true
	}
	kinds := make([]speechkit.ProviderKind, 0, len(seen))
	for _, kind := range []speechkit.ProviderKind{
		speechkit.ProviderKindLocalBuiltIn,
		speechkit.ProviderKindLocalProvider,
		speechkit.ProviderKindCloudProvider,
		speechkit.ProviderKindDirectProvider,
	} {
		if seen[kind] {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// ValidateDefaultCatalog verifies the framework invariant that every strict
// mode exposes all four provider groups and every visible profile satisfies its
// mode contract. v0.37 added ModeTTS as a model-selection axis with the same
// four-provider-group invariant (Local Built-in via Piper, Local Provider
// via Kokoro/openedai-speech, Cloud Provider via Hugging Face Parler, Direct
// Provider via OpenAI + Google).
func ValidateDefaultCatalog() error {
	for _, mode := range []speechkit.Mode{speechkit.ModeDictation, speechkit.ModeAssist, speechkit.ModeVoiceAgent, speechkit.ModeTTS} {
		kinds := ProviderKindsForMode(mode)
		if len(kinds) != 4 {
			sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
			return catalogContractError{mode: mode, kinds: kinds}
		}
		for _, profile := range ProfilesForMode(mode) {
			if err := speechkit.ValidateProfileForMode(profile, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

type catalogContractError struct {
	mode  speechkit.Mode
	kinds []speechkit.ProviderKind
}

func (e catalogContractError) Error() string {
	return "speechkit: default catalog does not expose four provider groups for " + string(e.mode)
}
