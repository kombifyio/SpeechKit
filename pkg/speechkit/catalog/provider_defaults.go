package catalog

import (
	"sort"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// DefaultProviderMatrix groups the built-in catalog by canonical provider id:
// one row per provider with its profiles (sorted by mode, then default,
// recommended, non-experimental, support grade and id) and the best support
// grade it reaches for each [ProviderFeature]. Rows follow the product's
// provider order; profiles without a resolvable provider id are skipped.
func DefaultProviderMatrix() []ProviderMatrixRow {
	return providerMatrixFor(DefaultProviderProfiles())
}

func providerMatrixFor(profiles []speechkit.ProviderProfile) []ProviderMatrixRow {
	grouped := map[string][]ProviderDefault{}
	for _, profile := range profiles {
		provider := ProviderIDForProfile(profile)
		if provider == "" {
			continue
		}
		grouped[provider] = append(grouped[provider], providerDefaultFromProfile(provider, profile))
	}

	rows := make([]ProviderMatrixRow, 0, len(grouped))
	for provider, profiles := range grouped {
		sortProviderDefaults(profiles)
		rows = append(rows, ProviderMatrixRow{
			Provider:    provider,
			DisplayName: providerDisplayName(provider),
			Profiles:    profiles,
			Features:    providerFeatureSupports(profiles),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := providerOrderIndex(rows[i].Provider), providerOrderIndex(rows[j].Provider)
		if left == right {
			return rows[i].Provider < rows[j].Provider
		}
		return left < right
	})
	return rows
}

// DefaultProviderDefaults returns the preferred profile of every built-in
// provider for each mode it supports (the first of its sorted matrix
// profiles), in matrix row order and then mode order: Dictation, Assist,
// Voice Agent, TTS.
func DefaultProviderDefaults() []ProviderDefault {
	return providerDefaultsFromMatrix(DefaultProviderMatrix())
}

func providerDefaultsFromMatrix(rows []ProviderMatrixRow) []ProviderDefault {
	var out []ProviderDefault
	for _, row := range rows {
		for _, mode := range []speechkit.Mode{speechkit.ModeDictation, speechkit.ModeAssist, speechkit.ModeVoiceAgent, speechkit.ModeTTS} {
			if profile, ok := preferredProviderDefault(row.Profiles, mode); ok {
				out = append(out, profile)
			}
		}
	}
	return out
}

// ProviderDefaultsFor returns the [DefaultProviderDefaults] entries whose
// provider matches provider after [NormalizeProviderID]; nil when unknown.
func ProviderDefaultsFor(provider string) []ProviderDefault {
	provider = NormalizeProviderID(provider)
	var out []ProviderDefault
	for _, profile := range DefaultProviderDefaults() {
		if profile.Provider == provider {
			out = append(out, profile)
		}
	}
	return out
}

// FindProviderDefault returns the preferred built-in profile of provider
// (normalised) for mode (normalised); ok is false when the provider has no
// profile for that mode.
func FindProviderDefault(provider string, mode speechkit.Mode) (ProviderDefault, bool) {
	provider = NormalizeProviderID(provider)
	mode = speechkit.NormalizeMode(mode)
	for _, profile := range DefaultProviderDefaults() {
		if profile.Provider == provider && profile.Mode == mode {
			return profile, true
		}
	}
	return ProviderDefault{}, false
}

// FindProviderMatrixRow returns the [DefaultProviderMatrix] row for provider
// after [NormalizeProviderID]; ok is false for an unknown provider.
func FindProviderMatrixRow(provider string) (ProviderMatrixRow, bool) {
	provider = NormalizeProviderID(provider)
	for _, row := range DefaultProviderMatrix() {
		if row.Provider == provider {
			return row, true
		}
	}
	return ProviderMatrixRow{}, false
}

// Feature returns the row's cell for feature; ok is false only when the row
// carries no such feature. A feature the provider lacks is still present,
// graded [ProviderSupportUnsupported].
func (r ProviderMatrixRow) Feature(feature ProviderFeature) (ProviderFeatureSupport, bool) {
	for _, support := range r.Features {
		if support.Feature == feature {
			return support, true
		}
	}
	return ProviderFeatureSupport{}, false
}

func providerDefaultFromProfile(provider string, profile speechkit.ProviderProfile) ProviderDefault {
	profile = ProviderProfileWithDefaults(profile)
	return ProviderDefault{
		Provider:           provider,
		DisplayName:        providerDisplayName(provider),
		Mode:               speechkit.NormalizeMode(profile.Mode),
		ProfileID:          profile.ID,
		ModelID:            profile.ModelID,
		ProviderKind:       profile.ProviderKind,
		ExecutionMode:      profile.ExecutionMode,
		Support:            supportKindForProfile(profile),
		Capabilities:       append([]speechkit.Capability(nil), profile.Capabilities...),
		NativeOptions:      nativeOptionsForProfile(provider, profile),
		AuthRequirement:    profile.AuthRequirement,
		CredentialRequired: ProviderProfileRequiresCredential(profile),
		CredentialTarget:   ProviderCredentialTarget(profile),
		Transport:          profile.Transport,
		EvidenceURL:        profile.EvidenceURL,
		Default:            profile.Default,
		Recommended:        profile.Recommended,
		Experimental:       profile.Experimental,
		Variants:           append([]speechkit.ModelVariant(nil), profile.Variants...),
	}
}

func supportKindForProfile(profile speechkit.ProviderProfile) ProviderSupportKind {
	if profile.Experimental && !profile.AllowInference {
		return ProviderSupportPlanned
	}
	if speechkit.NormalizeMode(profile.Mode) == speechkit.ModeVoiceAgent && profile.HasCapability(speechkit.CapabilityPipelineFallback) {
		return ProviderSupportCascaded
	}
	switch profile.ExecutionMode {
	case speechkit.ExecutionModeHFRouted, speechkit.ExecutionModeOpenRouter:
		return ProviderSupportRouted
	default:
		return ProviderSupportNative
	}
}

func preferredProviderDefault(profiles []ProviderDefault, mode speechkit.Mode) (ProviderDefault, bool) {
	mode = speechkit.NormalizeMode(mode)
	var matches []ProviderDefault
	for _, profile := range profiles {
		if profile.Mode == mode {
			matches = append(matches, profile)
		}
	}
	if len(matches) == 0 {
		return ProviderDefault{}, false
	}
	sortProviderDefaults(matches)
	return matches[0], true
}

func sortProviderDefaults(profiles []ProviderDefault) {
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Mode != profiles[j].Mode {
			return modeOrderIndex(profiles[i].Mode) < modeOrderIndex(profiles[j].Mode)
		}
		if profiles[i].Default != profiles[j].Default {
			return profiles[i].Default
		}
		if profiles[i].Recommended != profiles[j].Recommended {
			return profiles[i].Recommended
		}
		if profiles[i].Experimental != profiles[j].Experimental {
			return !profiles[i].Experimental
		}
		if profiles[i].Support != profiles[j].Support {
			return supportRank(profiles[i].Support) > supportRank(profiles[j].Support)
		}
		return profiles[i].ProfileID < profiles[j].ProfileID
	})
}

func providerDefaultHasCapability(profile ProviderDefault, capability speechkit.Capability) bool {
	for _, candidate := range profile.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

func modeOrderIndex(mode speechkit.Mode) int {
	switch speechkit.NormalizeMode(mode) {
	case speechkit.ModeDictation:
		return 0
	case speechkit.ModeAssist:
		return 1
	case speechkit.ModeVoiceAgent:
		return 2
	case speechkit.ModeTTS:
		return 3
	default:
		return 4
	}
}

func supportRank(kind ProviderSupportKind) int {
	switch kind {
	case ProviderSupportNative:
		return 4
	case ProviderSupportRouted:
		return 3
	case ProviderSupportCascaded:
		return 2
	case ProviderSupportPlanned:
		return 1
	default:
		return 0
	}
}
