package live

import (
	"errors"
	"fmt"
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// ErrNoMatchingProvider is the sentinel every [ProviderIntentError] unwraps
// to; test for it with errors.Is.
var ErrNoMatchingProvider = errors.New("speechkit live: no provider satisfies intent")

// LatencyProfile expresses how a host trades response latency against
// accuracy. [ResolveProviderIntent] copies it onto the plan for hosts and
// providers to interpret; it does not influence provider selection.
type LatencyProfile string

// Latency profiles, from fastest response to highest accuracy.
const (
	LatencyProfileInteractive LatencyProfile = "interactive"
	LatencyProfileBalanced    LatencyProfile = "balanced"
	LatencyProfileAccuracy    LatencyProfile = "accuracy"
)

// ModelLifecyclePolicy limits which model lifecycle stages
// [ResolveProviderIntent] may select; see [ProviderSelectionPolicy].
type ModelLifecyclePolicy string

// Lifecycle policies. The empty value behaves like ModelLifecyclePreferGA
// except that preview models also need [ProviderSelectionPolicy.AllowPreview].
const (
	// ModelLifecycleAny accepts GA, preview, legacy and deprecated models.
	ModelLifecycleAny ModelLifecyclePolicy = "any"
	// ModelLifecyclePreferGA accepts GA and preview models; legacy and
	// deprecated ones also need [ProviderSelectionPolicy.AllowLegacy].
	ModelLifecyclePreferGA ModelLifecyclePolicy = "prefer_ga"
	// ModelLifecycleRequireGA accepts GA models only.
	ModelLifecycleRequireGA ModelLifecyclePolicy = "require_ga"
)

// CapabilityRequirement is one capability of a [ProviderIntent]: Required
// makes it mandatory, otherwise it is only preferred.
type CapabilityRequirement struct {
	Capability LiveCapabilityFlag `json:"capability"`
	Required   bool               `json:"required,omitempty"`
}

// ProviderSelectionPolicy tunes how [ResolveProviderIntent] filters and
// ranks candidates. PreferredProviders (ids or aliases, best first) add a
// decreasing score bonus; ModelLifecycle, AllowPreview and AllowLegacy gate
// preview, legacy and deprecated models as described on
// [ModelLifecyclePolicy].
type ProviderSelectionPolicy struct {
	PreferredProviders []string             `json:"preferredProviders,omitempty"`
	AllowPreview       bool                 `json:"allowPreview,omitempty"`
	AllowLegacy        bool                 `json:"allowLegacy,omitempty"`
	ModelLifecycle     ModelLifecyclePolicy `json:"modelLifecycle,omitempty"`
}

// ProviderFallbackKind classifies a [ProviderFallback] or
// [ProviderRejection] relative to the selected plan.
type ProviderFallbackKind string

// Fallback kinds.
const (
	// FallbackKindSameProviderModel is another model of the same provider.
	FallbackKindSameProviderModel ProviderFallbackKind = "same_provider_model"
	// FallbackKindCrossProvider is a different realtime provider.
	FallbackKindCrossProvider ProviderFallbackKind = "cross_provider"
	// FallbackKindCascaded is the local STT -> LLM -> TTS pipeline.
	FallbackKindCascaded ProviderFallbackKind = "cascaded"
	// FallbackKindCapabilityMissing marks a rejection caused by missing
	// required capabilities or options.
	FallbackKindCapabilityMissing ProviderFallbackKind = "capability_missing"
)

// ProviderIntent describes what a host needs from a realtime session so
// [ResolveProviderIntent] can choose a provider and model. Provider,
// ProfileID and Model pin candidates when set; Required* fields must be
// satisfied while Preferred* fields only raise the score, and Requirements
// is folded into those lists. PrivacyRedaction requires
// [LiveCapabilityPrivacyRedaction], ResumePreferred prefers
// [LiveCapabilitySessionResume], and LanguageHints prefers native language
// hints. Locale excludes descriptors that list locales without it. Mode
// defaults to voice_agent, the only mode the catalog serves.
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

// ResolvedProviderPlan is the result of [ResolveProviderIntent]: the chosen
// provider, profile and model with their descriptors, why they were chosen
// (SelectionReason, SelectedFallbackKind), which requested capabilities and
// options matched or are unsupported, ranked alternatives in Fallbacks, and
// the candidates that were rejected.
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

// ProviderIntentError is returned by [ResolveProviderIntent] when no
// descriptor satisfies the intent. It lists the required capabilities and
// options that no candidate provided and every rejected candidate with its
// reason; Fallbacks is currently never populated. It unwraps to
// [ErrNoMatchingProvider].
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

// ProviderRejection explains why one descriptor was excluded from a plan:
// the model it would have used and its lifecycle, the reason, and the
// missing capabilities, options or locale when those caused it.
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

// ProviderFallback is an alternative to the selected plan. Kind says how it
// relates to the selection; the other fields identify the provider and
// model, its authentication and transport, and the evidence URL.
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

// LiveSessionCapabilities is implemented by providers that can report the
// profile, model and capabilities of their sessions. The built-in providers
// derive it from [SessionCapabilitiesForProvider].
type LiveSessionCapabilities interface {
	SessionCapabilities() SessionCapabilities
}

// SessionCapabilities is a provider's self-description for hosts: canonical
// provider id, profile, model and capability flags, plus optional
// provider-native metadata.
type SessionCapabilities struct {
	Provider         string               `json:"provider,omitempty"`
	ProfileID        string               `json:"profileId,omitempty"`
	Model            string               `json:"model,omitempty"`
	Capabilities     []LiveCapabilityFlag `json:"capabilities,omitempty"`
	ProviderMetadata map[string]any       `json:"providerMetadata,omitempty"`
}

// LiveConfig converts the plan into a [LiveConfig] with Provider, ProfileID
// and Model set and FallbackModel taken from the first
// [FallbackKindSameProviderModel] fallback. Credentials, prompts, tools and
// policies are left to the caller.
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
