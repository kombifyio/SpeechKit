package live

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func buildResolvedProviderPlan(intent ProviderIntent, chosen providerPlanCandidate, alternatives []providerPlanCandidate, rejections []ProviderRejection) ResolvedProviderPlan {
	descriptor := chosen.descriptor
	model := chosen.model
	requiredCaps := matchedCapabilities(descriptor, intent.RequiredCapabilities)
	preferredCaps := matchedCapabilities(descriptor, intent.PreferredCapabilities)
	requiredOptions := matchedNativeOptions(descriptor, intent.RequiredOptions)
	preferredOptions := matchedNativeOptions(descriptor, intent.PreferredOptions)
	fallbacks := providerFallbacks(intent, chosen, alternatives)
	return ResolvedProviderPlan{
		Provider:                         descriptor.Provider,
		ProfileID:                        descriptor.ProfileID,
		Model:                            model.ModelID,
		Descriptor:                       descriptor,
		ModelDescriptor:                  model,
		SelectionReason:                  selectionReason(intent, chosen),
		SelectedFallbackKind:             selectedFallbackKind(intent, chosen),
		Fallbacks:                        fallbacks,
		RejectedProviders:                rejections,
		MatchedRequiredCapabilities:      requiredCaps,
		MatchedPreferredCapabilities:     preferredCaps,
		UnsupportedPreferredCapabilities: missingCapabilities(descriptor, intent.PreferredCapabilities),
		MatchedRequiredOptions:           requiredOptions,
		MatchedPreferredOptions:          preferredOptions,
		UnsupportedPreferredOptions:      missingNativeOptions(descriptor, intent.PreferredOptions),
		AuthRequirement:                  descriptor.AuthRequirement,
		Transport:                        descriptor.Transport,
		LatencyProfile:                   intent.LatencyProfile,
	}
}

func providerFallbacks(intent ProviderIntent, chosen providerPlanCandidate, alternatives []providerPlanCandidate) []ProviderFallback {
	var out []ProviderFallback
	if chosen.descriptor.Provider != "" {
		if fallback, ok := sameProviderModelFallback(intent, chosen.descriptor, chosen.model); ok {
			out = append(out, fallback)
		}
	}
	for _, candidate := range alternatives {
		out = append(out, fallbackFromCandidate(candidate, fallbackKindForCandidate(chosen, candidate), "candidate satisfies intent if the selected plan cannot be used"))
	}
	return out
}

func sameProviderModelFallback(intent ProviderIntent, descriptor ProviderDescriptor, selected LiveModelDescriptor) (ProviderFallback, bool) {
	for _, model := range descriptor.Models {
		if model.ModelID == "" || strings.EqualFold(model.ModelID, selected.ModelID) {
			continue
		}
		if !modelLifecycleAllowed(model.Lifecycle, intent.SelectionPolicy) {
			continue
		}
		return ProviderFallback{
			Kind:            FallbackKindSameProviderModel,
			Provider:        descriptor.Provider,
			ProfileID:       descriptor.ProfileID,
			Model:           model.ModelID,
			ModelLifecycle:  model.Lifecycle,
			Reason:          "same provider advertises an alternate compatible model",
			AuthRequirement: descriptor.AuthRequirement,
			Transport:       descriptor.Transport,
			EvidenceURL:     firstNonEmptyString(model.SourceURL, descriptor.EvidenceURL),
		}, true
	}
	return ProviderFallback{}, false
}

func fallbackFromCandidate(candidate providerPlanCandidate, kind ProviderFallbackKind, reason string) ProviderFallback {
	return ProviderFallback{
		Kind:            kind,
		Provider:        candidate.descriptor.Provider,
		ProfileID:       candidate.descriptor.ProfileID,
		Model:           candidate.model.ModelID,
		ModelLifecycle:  candidate.model.Lifecycle,
		Reason:          reason,
		AuthRequirement: candidate.descriptor.AuthRequirement,
		Transport:       candidate.descriptor.Transport,
		EvidenceURL:     firstNonEmptyString(candidate.model.SourceURL, candidate.descriptor.EvidenceURL),
	}
}

func fallbackKindForCandidate(chosen, candidate providerPlanCandidate) ProviderFallbackKind {
	if isCascadedProvider(candidate.descriptor.Provider) {
		return FallbackKindCascaded
	}
	if chosen.descriptor.Provider != "" && candidate.descriptor.Provider == chosen.descriptor.Provider {
		return FallbackKindSameProviderModel
	}
	return FallbackKindCrossProvider
}

func selectedFallbackKind(intent ProviderIntent, chosen providerPlanCandidate) ProviderFallbackKind {
	if isCascadedProvider(chosen.descriptor.Provider) {
		return FallbackKindCascaded
	}
	preferred := normalizedProviderList(intent.SelectionPolicy.PreferredProviders)
	if len(preferred) > 0 && preferred[0] != chosen.descriptor.Provider {
		return FallbackKindCrossProvider
	}
	requestedModel := strings.TrimSpace(intent.Model)
	if requestedModel != "" && !strings.EqualFold(requestedModel, chosen.model.ModelID) {
		return FallbackKindSameProviderModel
	}
	return ""
}

func selectionReason(intent ProviderIntent, chosen providerPlanCandidate) string {
	switch selectedFallbackKind(intent, chosen) {
	case FallbackKindCascaded:
		return "selected cascaded pipeline fallback that satisfies the intent"
	case FallbackKindCrossProvider:
		return "selected a fallback provider because an earlier preferred provider did not satisfy the intent"
	case FallbackKindSameProviderModel:
		return "selected a same-provider fallback model that satisfies the intent"
	default:
		if strings.TrimSpace(intent.Provider) != "" || strings.TrimSpace(intent.ProfileID) != "" {
			return "selected the requested provider/profile because it satisfies the intent"
		}
		return "selected the highest-scoring provider/profile that satisfies the intent"
	}
}

func providerRejection(descriptor ProviderDescriptor, model LiveModelDescriptor, kind ProviderFallbackKind, reason string, missingCaps []LiveCapabilityFlag, missingOptions []provideropts.OptionID, unsupportedLocale string) ProviderRejection {
	return ProviderRejection{
		Provider:                    descriptor.Provider,
		ProfileID:                   descriptor.ProfileID,
		Model:                       model.ModelID,
		ModelLifecycle:              model.Lifecycle,
		FallbackKind:                kind,
		Reason:                      reason,
		MissingRequiredCapabilities: append([]LiveCapabilityFlag(nil), missingCaps...),
		MissingRequiredOptions:      append([]provideropts.OptionID(nil), missingOptions...),
		AuthRequirement:             descriptor.AuthRequirement,
		Transport:                   descriptor.Transport,
		EvidenceURL:                 firstNonEmptyString(model.SourceURL, descriptor.EvidenceURL),
		UnsupportedLocale:           unsupportedLocale,
	}
}
