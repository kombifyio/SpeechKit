package live

import (
	"sort"
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// ResolveProviderIntent picks the provider and model that best satisfy
// intent from descriptors; nil or empty uses [DefaultProviderDescriptors].
// A candidate must match the requested provider or profile, advertise the
// requested (or its default) model within the lifecycle policy, support the
// locale, and provide every required capability and option. Candidates are
// scored by an explicit provider or profile match, the default, recommended
// and GA flags of the model, position in PreferredProviders, and each
// matched preferred capability and option; ties keep descriptor order.
// [ProviderDescriptor.OptIn] providers are skipped unless the intent names
// them. When nothing qualifies it returns a *[ProviderIntentError].
func ResolveProviderIntent(intent ProviderIntent, descriptors []ProviderDescriptor) (ResolvedProviderPlan, error) {
	if len(descriptors) == 0 {
		descriptors = DefaultProviderDescriptors()
	}
	intent = normalizeProviderIntent(intent)
	preferredProviders := normalizedProviderList(intent.SelectionPolicy.PreferredProviders)
	var candidates []providerPlanCandidate
	var rejections []ProviderRejection
	var aggregateMissingCaps []LiveCapabilityFlag
	var aggregateMissingOptions []provideropts.OptionID

	for index, descriptor := range descriptors {
		if !providerSelectorMatches(intent, descriptor) {
			continue
		}
		if descriptor.OptIn && !intentNamesProvider(intent, descriptor, preferredProviders) {
			continue
		}
		model, ok := selectIntentModel(intent, descriptor)
		if !ok {
			rejections = append(rejections, providerRejection(descriptor, LiveModelDescriptor{}, "", "requested model is not advertised by provider", nil, nil, ""))
			continue
		}
		if !modelLifecycleAllowed(model.Lifecycle, intent.SelectionPolicy) {
			rejections = append(rejections, providerRejection(descriptor, model, "", "model lifecycle is not allowed by intent policy", nil, nil, ""))
			continue
		}
		if locale := strings.TrimSpace(intent.Locale); locale != "" && !descriptorSupportsLocale(descriptor, locale) {
			rejections = append(rejections, providerRejection(descriptor, model, "", "locale is not advertised by provider descriptor", nil, nil, locale))
			continue
		}
		missingCaps := missingCapabilities(descriptor, intent.RequiredCapabilities)
		missingOptions := missingNativeOptions(descriptor, intent.RequiredOptions)
		if len(missingCaps) > 0 || len(missingOptions) > 0 {
			aggregateMissingCaps = appendCapabilitySet(aggregateMissingCaps, missingCaps...)
			aggregateMissingOptions = appendOptionSet(aggregateMissingOptions, missingOptions...)
			rejections = append(rejections, providerRejection(descriptor, model, FallbackKindCapabilityMissing, "provider is missing required intent support", missingCaps, missingOptions, ""))
			continue
		}
		score := providerIntentScore(intent, descriptor, model, preferredProviders)
		candidates = append(candidates, providerPlanCandidate{
			descriptor: descriptor,
			model:      model,
			score:      score,
			index:      index,
		})
	}
	if len(candidates) == 0 {
		sortCapabilityFlags(aggregateMissingCaps)
		sortOptionIDs(aggregateMissingOptions)
		return ResolvedProviderPlan{}, &ProviderIntentError{
			Intent:                      intent,
			MissingRequiredCapabilities: aggregateMissingCaps,
			MissingRequiredOptions:      aggregateMissingOptions,
			Fallbacks:                   providerFallbacks(intent, providerPlanCandidate{}, nil),
			RejectedProviders:           rejections,
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].index < candidates[j].index
		}
		return candidates[i].score > candidates[j].score
	})
	chosen := candidates[0]
	return buildResolvedProviderPlan(intent, chosen, candidates[1:], rejections), nil
}

type providerPlanCandidate struct {
	descriptor ProviderDescriptor
	model      LiveModelDescriptor
	score      int
	index      int
}

func normalizeProviderIntent(intent ProviderIntent) ProviderIntent {
	if strings.TrimSpace(intent.Mode) == "" {
		intent.Mode = string(framework.ModeVoiceAgent)
	}
	if intent.PrivacyRedaction {
		intent.RequiredCapabilities = appendCapabilitySet(intent.RequiredCapabilities, LiveCapabilityPrivacyRedaction)
	}
	if intent.ResumePreferred {
		intent.PreferredCapabilities = appendCapabilitySet(intent.PreferredCapabilities, LiveCapabilitySessionResume)
	}
	for _, requirement := range intent.Requirements {
		if requirement.Required {
			intent.RequiredCapabilities = appendCapabilitySet(intent.RequiredCapabilities, requirement.Capability)
		} else {
			intent.PreferredCapabilities = appendCapabilitySet(intent.PreferredCapabilities, requirement.Capability)
		}
	}
	if len(intent.LanguageHints) > 0 {
		intent.PreferredCapabilities = appendCapabilitySet(intent.PreferredCapabilities, LiveCapabilityLanguageHints)
		intent.PreferredOptions = appendOptionSet(intent.PreferredOptions, provideropts.OptionLanguageHints)
	}
	sortCapabilityFlags(intent.RequiredCapabilities)
	sortCapabilityFlags(intent.PreferredCapabilities)
	sortOptionIDs(intent.RequiredOptions)
	sortOptionIDs(intent.PreferredOptions)
	return intent
}

func providerSelectorMatches(intent ProviderIntent, descriptor ProviderDescriptor) bool {
	if selector := strings.TrimSpace(intent.Provider); selector != "" && NormalizeProviderID(selector) != descriptor.Provider {
		return false
	}
	if profileID := strings.TrimSpace(intent.ProfileID); profileID != "" && !strings.EqualFold(profileID, descriptor.ProfileID) {
		return false
	}
	mode := strings.TrimSpace(intent.Mode)
	return mode == "" || mode == string(framework.ModeVoiceAgent) || mode == provideropts.ModalityVoiceAgent
}

// intentNamesProvider reports whether the intent explicitly asks for the
// descriptor's provider: by provider id, profile id, model id, or as a
// preferred provider. Opt-in providers are only resolved on such a request.
func intentNamesProvider(intent ProviderIntent, descriptor ProviderDescriptor, preferredProviders []string) bool {
	if strings.TrimSpace(intent.Provider) != "" || strings.TrimSpace(intent.ProfileID) != "" || strings.TrimSpace(intent.Model) != "" {
		return true
	}
	for _, provider := range preferredProviders {
		if provider == descriptor.Provider {
			return true
		}
	}
	return false
}

func selectIntentModel(intent ProviderIntent, descriptor ProviderDescriptor) (LiveModelDescriptor, bool) {
	if modelID := strings.TrimSpace(intent.Model); modelID != "" {
		for _, model := range descriptor.Models {
			if strings.EqualFold(model.ModelID, modelID) {
				return model, true
			}
		}
		return LiveModelDescriptor{}, false
	}
	return descriptor.DefaultModel()
}

func modelLifecycleAllowed(lifecycle framework.ModelLifecycle, policy ProviderSelectionPolicy) bool {
	switch policy.ModelLifecycle {
	case ModelLifecycleRequireGA:
		return lifecycle == framework.ModelLifecycleGA
	case ModelLifecyclePreferGA, ModelLifecycleAny, "":
		if lifecycle == framework.ModelLifecyclePreview {
			return policy.AllowPreview || policy.ModelLifecycle == ModelLifecycleAny || policy.ModelLifecycle == ModelLifecyclePreferGA
		}
		if lifecycle == framework.ModelLifecycleLegacy || lifecycle == framework.ModelLifecycleDeprecated {
			return policy.AllowLegacy || policy.ModelLifecycle == ModelLifecycleAny
		}
		return true
	default:
		return true
	}
}

func providerIntentScore(intent ProviderIntent, descriptor ProviderDescriptor, model LiveModelDescriptor, preferredProviders []string) int {
	score := 0
	if descriptor.Provider == NormalizeProviderID(intent.Provider) || strings.EqualFold(descriptor.ProfileID, strings.TrimSpace(intent.ProfileID)) {
		score += 100
	}
	if model.Default {
		score += 10
	}
	if model.Recommended {
		score += 8
	}
	if model.Lifecycle == framework.ModelLifecycleGA {
		score += 6
	}
	for i, provider := range preferredProviders {
		if descriptor.Provider == provider {
			score += 50 - i
			break
		}
	}
	score += len(matchedCapabilities(descriptor, intent.PreferredCapabilities)) * 3
	score += len(matchedNativeOptions(descriptor, intent.PreferredOptions)) * 2
	return score
}
