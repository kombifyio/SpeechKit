package live

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

var ErrNoMatchingProvider = errors.New("speechkit live: no provider satisfies intent")

type LatencyProfile string

const (
	LatencyProfileInteractive LatencyProfile = "interactive"
	LatencyProfileBalanced    LatencyProfile = "balanced"
	LatencyProfileAccuracy    LatencyProfile = "accuracy"
)

type ModelLifecyclePolicy string

const (
	ModelLifecycleAny       ModelLifecyclePolicy = "any"
	ModelLifecyclePreferGA  ModelLifecyclePolicy = "prefer_ga"
	ModelLifecycleRequireGA ModelLifecyclePolicy = "require_ga"
)

type CapabilityRequirement struct {
	Capability LiveCapabilityFlag `json:"capability"`
	Required   bool               `json:"required,omitempty"`
}

type ProviderSelectionPolicy struct {
	PreferredProviders []string             `json:"preferredProviders,omitempty"`
	AllowPreview       bool                 `json:"allowPreview,omitempty"`
	AllowLegacy        bool                 `json:"allowLegacy,omitempty"`
	ModelLifecycle     ModelLifecyclePolicy `json:"modelLifecycle,omitempty"`
}

type ProviderFallbackKind string

const (
	FallbackKindSameProviderModel ProviderFallbackKind = "same_provider_model"
	FallbackKindCrossProvider     ProviderFallbackKind = "cross_provider"
	FallbackKindCascaded          ProviderFallbackKind = "cascaded"
	FallbackKindCapabilityMissing ProviderFallbackKind = "capability_missing"
)

type ProviderIntent struct {
	Mode                  string                  `json:"mode,omitempty"`
	Provider              string                  `json:"provider,omitempty"`
	ProfileID             string                  `json:"profileId,omitempty"`
	Model                 string                  `json:"model,omitempty"`
	RequiredCapabilities  []LiveCapabilityFlag    `json:"requiredCapabilities,omitempty"`
	PreferredCapabilities []LiveCapabilityFlag    `json:"preferredCapabilities,omitempty"`
	Requirements          []CapabilityRequirement `json:"requirements,omitempty"`
	RequiredOptions       []provideropts.OptionID `json:"requiredOptions,omitempty"`
	PreferredOptions      []provideropts.OptionID `json:"preferredOptions,omitempty"`
	Locale                string                  `json:"locale,omitempty"`
	LanguageHints         []string                `json:"languageHints,omitempty"`
	PrivacyRedaction      bool                    `json:"privacyRedaction,omitempty"`
	ResumePreferred       bool                    `json:"resumePreferred,omitempty"`
	LatencyProfile        LatencyProfile          `json:"latencyProfile,omitempty"`
	SelectionPolicy       ProviderSelectionPolicy `json:"selectionPolicy,omitempty"`
}

type ResolvedProviderPlan struct {
	Provider                         string                  `json:"provider"`
	ProfileID                        string                  `json:"profileId"`
	Model                            string                  `json:"model"`
	Descriptor                       ProviderDescriptor      `json:"descriptor"`
	ModelDescriptor                  LiveModelDescriptor     `json:"modelDescriptor"`
	SelectionReason                  string                  `json:"selectionReason,omitempty"`
	SelectedFallbackKind             ProviderFallbackKind    `json:"selectedFallbackKind,omitempty"`
	Fallbacks                        []ProviderFallback      `json:"fallbacks,omitempty"`
	RejectedProviders                []ProviderRejection     `json:"rejectedProviders,omitempty"`
	MatchedRequiredCapabilities      []LiveCapabilityFlag    `json:"matchedRequiredCapabilities,omitempty"`
	MatchedPreferredCapabilities     []LiveCapabilityFlag    `json:"matchedPreferredCapabilities,omitempty"`
	UnsupportedPreferredCapabilities []LiveCapabilityFlag    `json:"unsupportedPreferredCapabilities,omitempty"`
	MatchedRequiredOptions           []provideropts.OptionID `json:"matchedRequiredOptions,omitempty"`
	MatchedPreferredOptions          []provideropts.OptionID `json:"matchedPreferredOptions,omitempty"`
	UnsupportedPreferredOptions      []provideropts.OptionID `json:"unsupportedPreferredOptions,omitempty"`
	AuthRequirement                  string                  `json:"authRequirement,omitempty"`
	Transport                        string                  `json:"transport,omitempty"`
	LatencyProfile                   LatencyProfile          `json:"latencyProfile,omitempty"`
}

type ProviderIntentError struct {
	Intent                      ProviderIntent          `json:"intent"`
	MissingRequiredCapabilities []LiveCapabilityFlag    `json:"missingRequiredCapabilities,omitempty"`
	MissingRequiredOptions      []provideropts.OptionID `json:"missingRequiredOptions,omitempty"`
	Fallbacks                   []ProviderFallback      `json:"fallbacks,omitempty"`
	RejectedProviders           []ProviderRejection     `json:"rejectedProviders,omitempty"`
}

func (e *ProviderIntentError) Error() string {
	if e == nil {
		return ErrNoMatchingProvider.Error()
	}
	var reasons []string
	if len(e.MissingRequiredCapabilities) > 0 {
		reasons = append(reasons, "missing capabilities: "+joinCapabilityFlags(e.MissingRequiredCapabilities))
	}
	if len(e.MissingRequiredOptions) > 0 {
		reasons = append(reasons, "missing options: "+joinOptionIDs(e.MissingRequiredOptions))
	}
	if len(reasons) == 0 && len(e.RejectedProviders) > 0 {
		reasons = append(reasons, e.RejectedProviders[0].Reason)
	}
	if len(reasons) == 0 {
		return ErrNoMatchingProvider.Error()
	}
	return ErrNoMatchingProvider.Error() + ": " + strings.Join(reasons, "; ")
}

func (e *ProviderIntentError) Unwrap() error { return ErrNoMatchingProvider }

type ProviderRejection struct {
	Provider                    string                   `json:"provider"`
	ProfileID                   string                   `json:"profileId,omitempty"`
	Model                       string                   `json:"model,omitempty"`
	ModelLifecycle              framework.ModelLifecycle `json:"modelLifecycle,omitempty"`
	FallbackKind                ProviderFallbackKind     `json:"fallbackKind,omitempty"`
	Reason                      string                   `json:"reason"`
	MissingRequiredCapabilities []LiveCapabilityFlag     `json:"missingRequiredCapabilities,omitempty"`
	MissingRequiredOptions      []provideropts.OptionID  `json:"missingRequiredOptions,omitempty"`
	AuthRequirement             string                   `json:"authRequirement,omitempty"`
	Transport                   string                   `json:"transport,omitempty"`
	EvidenceURL                 string                   `json:"evidenceUrl,omitempty"`
	UnsupportedLocale           string                   `json:"unsupportedLocale,omitempty"`
}

type ProviderFallback struct {
	Kind                        ProviderFallbackKind     `json:"kind"`
	Provider                    string                   `json:"provider"`
	ProfileID                   string                   `json:"profileId,omitempty"`
	Model                       string                   `json:"model,omitempty"`
	ModelLifecycle              framework.ModelLifecycle `json:"modelLifecycle,omitempty"`
	Reason                      string                   `json:"reason,omitempty"`
	MissingRequiredCapabilities []LiveCapabilityFlag     `json:"missingRequiredCapabilities,omitempty"`
	MissingRequiredOptions      []provideropts.OptionID  `json:"missingRequiredOptions,omitempty"`
	AuthRequirement             string                   `json:"authRequirement,omitempty"`
	Transport                   string                   `json:"transport,omitempty"`
	EvidenceURL                 string                   `json:"evidenceUrl,omitempty"`
}

type LiveSessionCapabilities interface {
	SessionCapabilities() SessionCapabilities
}

type SessionCapabilities struct {
	Provider         string               `json:"provider,omitempty"`
	ProfileID        string               `json:"profileId,omitempty"`
	Model            string               `json:"model,omitempty"`
	Capabilities     []LiveCapabilityFlag `json:"capabilities,omitempty"`
	ProviderMetadata map[string]any       `json:"providerMetadata,omitempty"`
}

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

func isCascadedProvider(provider string) bool {
	provider = NormalizeProviderID(provider)
	return provider == "cascaded" || provider == "local-cascaded"
}

func descriptorSupportsLocale(descriptor ProviderDescriptor, locale string) bool {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		return true
	}
	lang := strings.Split(locale, "-")[0]
	for _, candidate := range descriptor.SupportedLocales {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "*" || candidate == locale || candidate == lang {
			return true
		}
	}
	return len(descriptor.SupportedLocales) == 0
}

func matchedCapabilities(descriptor ProviderDescriptor, capabilities []LiveCapabilityFlag) []LiveCapabilityFlag {
	var out []LiveCapabilityFlag
	for _, capability := range capabilities {
		if descriptor.HasCapability(capability) {
			out = appendCapabilitySet(out, capability)
		}
	}
	sortCapabilityFlags(out)
	return out
}

func missingCapabilities(descriptor ProviderDescriptor, capabilities []LiveCapabilityFlag) []LiveCapabilityFlag {
	var out []LiveCapabilityFlag
	for _, capability := range capabilities {
		if !descriptor.HasCapability(capability) {
			out = appendCapabilitySet(out, capability)
		}
	}
	sortCapabilityFlags(out)
	return out
}

func matchedNativeOptions(descriptor ProviderDescriptor, options []provideropts.OptionID) []provideropts.OptionID {
	var out []provideropts.OptionID
	for _, option := range options {
		if descriptorHasNativeOption(descriptor, option) {
			out = appendOptionSet(out, option)
		}
	}
	sortOptionIDs(out)
	return out
}

func missingNativeOptions(descriptor ProviderDescriptor, options []provideropts.OptionID) []provideropts.OptionID {
	var out []provideropts.OptionID
	for _, option := range options {
		if !descriptorHasNativeOption(descriptor, option) {
			out = appendOptionSet(out, option)
		}
	}
	sortOptionIDs(out)
	return out
}

func descriptorHasNativeOption(descriptor ProviderDescriptor, option provideropts.OptionID) bool {
	for _, candidate := range descriptor.NativeOptions {
		if candidate == option {
			return true
		}
	}
	return false
}

func normalizedProviderList(providers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, provider := range providers {
		normalized := NormalizeProviderID(provider)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out
}

func appendCapabilitySet(base []LiveCapabilityFlag, values ...LiveCapabilityFlag) []LiveCapabilityFlag {
	seen := map[LiveCapabilityFlag]bool{}
	for _, value := range base {
		if strings.TrimSpace(string(value)) != "" {
			seen[value] = true
		}
	}
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" || seen[value] {
			continue
		}
		seen[value] = true
		base = append(base, value)
	}
	return base
}

func appendOptionSet(base []provideropts.OptionID, values ...provideropts.OptionID) []provideropts.OptionID {
	seen := map[provideropts.OptionID]bool{}
	for _, value := range base {
		if strings.TrimSpace(string(value)) != "" {
			seen[value] = true
		}
	}
	for _, value := range values {
		if strings.TrimSpace(string(value)) == "" || seen[value] {
			continue
		}
		seen[value] = true
		base = append(base, value)
	}
	return base
}

func sortCapabilityFlags(values []LiveCapabilityFlag) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}

func sortOptionIDs(values []provideropts.OptionID) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}

func joinCapabilityFlags(values []LiveCapabilityFlag) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, ",")
}

func joinOptionIDs(values []provideropts.OptionID) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, string(value))
	}
	return strings.Join(parts, ",")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (p ResolvedProviderPlan) LiveConfig() LiveConfig {
	cfg := LiveConfig{Provider: p.Provider, ProfileID: p.ProfileID, Model: p.Model}
	for _, fallback := range p.Fallbacks {
		if fallback.Kind == FallbackKindSameProviderModel && fallback.Provider == p.Provider && strings.TrimSpace(fallback.Model) != "" {
			cfg.FallbackModel = fallback.Model
			break
		}
	}
	return cfg
}

func (p ResolvedProviderPlan) String() string {
	if p.Provider == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/%s", p.Provider, p.ProfileID, p.Model)
}
