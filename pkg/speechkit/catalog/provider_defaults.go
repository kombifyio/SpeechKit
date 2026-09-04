package catalog

import (
	"sort"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

type ProviderSupportKind string

const (
	ProviderSupportUnsupported ProviderSupportKind = "unsupported"
	ProviderSupportPlanned     ProviderSupportKind = "planned"
	ProviderSupportCascaded    ProviderSupportKind = "cascaded"
	ProviderSupportRouted      ProviderSupportKind = "routed"
	ProviderSupportNative      ProviderSupportKind = "native"
)

type ProviderFeature string

const (
	ProviderFeatureDictation             ProviderFeature = "dictation"
	ProviderFeatureDictationStreaming    ProviderFeature = "dictation_streaming"
	ProviderFeatureLongTranscription     ProviderFeature = "long_transcription"
	ProviderFeatureSpeakerDiarization    ProviderFeature = "speaker_diarization"
	ProviderFeatureSpeakerIdentification ProviderFeature = "speaker_identification"
	ProviderFeatureAssist                ProviderFeature = "assist"
	ProviderFeatureRealtimeVoice         ProviderFeature = "realtime_voice"
	ProviderFeatureTTS                   ProviderFeature = "tts"
)

const (
	ProviderAuthNone             = "none"
	ProviderAuthAPIKey           = "api_key"
	ProviderAuthToken            = "token"
	ProviderAuthHostDependencies = "host_dependencies"
	ProviderAuthOptionalAPIKey   = "optional_api_key"

	ProviderTransportLocal     = "local"
	ProviderTransportHTTP      = "http"
	ProviderTransportHTTPS     = "https"
	ProviderTransportWebSocket = "websocket"
	ProviderTransportPipeline  = "pipeline"
)

type ProviderDefault struct {
	Provider           string                   `json:"provider"`
	DisplayName        string                   `json:"displayName"`
	Mode               speechkit.Mode           `json:"mode"`
	ProfileID          string                   `json:"profileId"`
	ModelID            string                   `json:"modelId,omitempty"`
	ProviderKind       speechkit.ProviderKind   `json:"providerKind"`
	ExecutionMode      speechkit.ExecutionMode  `json:"executionMode,omitempty"`
	Support            ProviderSupportKind      `json:"support"`
	Capabilities       []speechkit.Capability   `json:"capabilities,omitempty"`
	NativeOptions      []string                 `json:"nativeOptions,omitempty"`
	AuthRequirement    string                   `json:"authRequirement,omitempty"`
	CredentialRequired bool                     `json:"credentialRequired"`
	CredentialTarget   string                   `json:"credentialTarget,omitempty"`
	Transport          string                   `json:"transport,omitempty"`
	EvidenceURL        string                   `json:"evidenceUrl,omitempty"`
	Default            bool                     `json:"default,omitempty"`
	Recommended        bool                     `json:"recommended,omitempty"`
	Experimental       bool                     `json:"experimental,omitempty"`
	Variants           []speechkit.ModelVariant `json:"variants,omitempty"`
}

type ProviderFeatureSupport struct {
	Feature       ProviderFeature     `json:"feature"`
	Support       ProviderSupportKind `json:"support"`
	Mode          speechkit.Mode      `json:"mode,omitempty"`
	ProfileID     string              `json:"profileId,omitempty"`
	ModelID       string              `json:"modelId,omitempty"`
	NativeOptions []string            `json:"nativeOptions,omitempty"`
	EvidenceURL   string              `json:"evidenceUrl,omitempty"`
}

type ProviderMatrixRow struct {
	Provider    string                   `json:"provider"`
	DisplayName string                   `json:"displayName"`
	Profiles    []ProviderDefault        `json:"profiles"`
	Features    []ProviderFeatureSupport `json:"features"`
}

// NormalizeProviderID maps a provider alias or a "<mode>.<provider>.<model>"
// profile id to its canonical provider id. Profile ids are reduced to their
// provider segment first, so every mode (stt, assist, utility, realtime, tts,
// speaker) shares one alias table; third-party providers pass through as-is.
func NormalizeProviderID(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	value = strings.ReplaceAll(value, "_", "-")
	if value == "" {
		return ""
	}
	if segment, ok := providerSegmentFromProfileID(value); ok {
		value = segment
	}
	switch value {
	case "builtin", "local-built-in", "local":
		return "local"
	case "hf", "hf-routed", "routed", "hugging-face", "huggingface":
		return "huggingface"
	case "open-router", "openrouter":
		return "openrouter"
	case "google-ai", "google-cloud", "gemini", "gemini-live", "google":
		return "google"
	case "assembly-ai", "assemblyai":
		return "assemblyai"
	case "openai-compatible", "openedai-speech", "openedai":
		return "openedai"
	default:
		return value
	}
}

var profileIDModePrefixes = []string{"stt.", "assist.", "utility.", "realtime.", "tts.", "speaker."}

func providerSegmentFromProfileID(value string) (string, bool) {
	for _, prefix := range profileIDModePrefixes {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		rest := value[len(prefix):]
		provider, _, found := strings.Cut(rest, ".")
		if !found || provider == "" {
			return "", false
		}
		return provider, true
	}
	return "", false
}

func ProviderIDForProfile(profile speechkit.ProviderProfile) string {
	if provider := NormalizeProviderID(profile.Provider); provider != "" {
		return provider
	}
	if provider := NormalizeProviderID(profile.ID); provider != "" && !strings.Contains(provider, ".") {
		return provider
	}
	return ProviderIDForExecutionMode(profile.ExecutionMode)
}

func ProviderIDForExecutionMode(mode speechkit.ExecutionMode) string {
	switch mode {
	case speechkit.ExecutionModeLocal:
		return "local"
	case speechkit.ExecutionModeSelfHostedHTTP:
		return "selfhosted"
	case speechkit.ExecutionModeHFRouted:
		return "huggingface"
	case speechkit.ExecutionModeOpenAI:
		return "openai"
	case speechkit.ExecutionModeGroq:
		return "groq"
	case speechkit.ExecutionModeGoogle:
		return "google"
	case speechkit.ExecutionModeDeepgram:
		return "deepgram"
	case speechkit.ExecutionModeAssemblyAI:
		return "assemblyai"
	case speechkit.ExecutionModeOllama:
		return "ollama"
	case speechkit.ExecutionModeOpenRouter:
		return "openrouter"
	case speechkit.ExecutionModeFoundry:
		return "foundry"
	default:
		return ""
	}
}

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

func ProviderProfileRequiresCredential(profile speechkit.ProviderProfile) bool {
	switch DefaultProviderAuthRequirement(profile) {
	case "", ProviderAuthNone, ProviderAuthHostDependencies, ProviderAuthOptionalAPIKey:
		return false
	default:
		return true
	}
}

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

func FindProviderMatrixRow(provider string) (ProviderMatrixRow, bool) {
	provider = NormalizeProviderID(provider)
	for _, row := range DefaultProviderMatrix() {
		if row.Provider == provider {
			return row, true
		}
	}
	return ProviderMatrixRow{}, false
}

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

func providerFeatureSupports(profiles []ProviderDefault) []ProviderFeatureSupport {
	out := make([]ProviderFeatureSupport, 0, len(providerFeatureOrder))
	for _, feature := range providerFeatureOrder {
		out = append(out, bestFeatureSupport(profiles, feature))
	}
	return out
}

func bestFeatureSupport(profiles []ProviderDefault, feature ProviderFeature) ProviderFeatureSupport {
	best := ProviderFeatureSupport{
		Feature: feature,
		Support: ProviderSupportUnsupported,
		Mode:    modeForProviderFeature(feature),
	}
	for _, profile := range profiles {
		candidate, ok := featureSupportForProfile(profile, feature)
		if !ok {
			continue
		}
		if supportRank(candidate.Support) > supportRank(best.Support) {
			best = candidate
		}
	}
	return best
}

func featureSupportForProfile(profile ProviderDefault, feature ProviderFeature) (ProviderFeatureSupport, bool) {
	support := ProviderFeatureSupport{
		Feature:       feature,
		Support:       profile.Support,
		Mode:          profile.Mode,
		ProfileID:     profile.ProfileID,
		ModelID:       profile.ModelID,
		NativeOptions: append([]string(nil), profile.NativeOptions...),
		EvidenceURL:   profile.EvidenceURL,
	}
	switch feature {
	case ProviderFeatureDictation:
		return support, profile.Mode == speechkit.ModeDictation && providerDefaultHasCapability(profile, speechkit.CapabilityTranscription)
	case ProviderFeatureDictationStreaming:
		if profile.Mode != speechkit.ModeDictation || !providerDefaultHasCapability(profile, speechkit.CapabilityTranscription) {
			return ProviderFeatureSupport{}, false
		}
		if providerDefaultHasCapability(profile, speechkit.CapabilityNativeDictationStream) {
			support.Support = ProviderSupportNative
		} else {
			support.Support = ProviderSupportCascaded
		}
		return support, true
	case ProviderFeatureLongTranscription:
		return support, profile.Mode == speechkit.ModeDictation && providerDefaultHasCapability(profile, speechkit.CapabilityTranscription)
	case ProviderFeatureSpeakerDiarization:
		return support, profile.Mode == speechkit.ModeDictation &&
			(providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerDiarization) ||
				providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerAttribution) ||
				providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerIdentification))
	case ProviderFeatureSpeakerIdentification:
		return support, profile.Mode == speechkit.ModeDictation &&
			(providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerIdentification) ||
				providerDefaultHasCapability(profile, speechkit.CapabilitySpeakerAttribution))
	case ProviderFeatureAssist:
		return support, profile.Mode == speechkit.ModeAssist && providerDefaultHasCapability(profile, speechkit.CapabilityLLM)
	case ProviderFeatureRealtimeVoice:
		if profile.Mode != speechkit.ModeVoiceAgent {
			return ProviderFeatureSupport{}, false
		}
		if providerDefaultHasCapability(profile, speechkit.CapabilityRealtimeAudio) {
			return support, true
		}
		if providerDefaultHasCapability(profile, speechkit.CapabilityPipelineFallback) {
			support.Support = ProviderSupportCascaded
			return support, true
		}
		return ProviderFeatureSupport{}, false
	case ProviderFeatureTTS:
		return support, profile.Mode == speechkit.ModeTTS && providerDefaultHasCapability(profile, speechkit.CapabilityTTS)
	default:
		return ProviderFeatureSupport{}, false
	}
}

func nativeOptionsForProfile(provider string, profile speechkit.ProviderProfile) []string {
	seen := map[string]bool{}
	var out []string
	for _, option := range profile.NativeOptions {
		option = strings.TrimSpace(option)
		if option != "" && !seen[option] {
			seen[option] = true
			out = append(out, option)
		}
	}
	modality := modalityForProviderMode(profile.Mode)
	if modality != "" {
		if manifest, ok := provideropts.FindManifest(provider, modality); ok {
			for _, option := range manifest.Options {
				if option.Status != provideropts.SupportNative {
					continue
				}
				id := strings.TrimSpace(string(option.ID))
				if id != "" && !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func modalityForProviderMode(mode speechkit.Mode) string {
	switch speechkit.NormalizeMode(mode) {
	case speechkit.ModeDictation:
		return provideropts.ModalitySTT
	case speechkit.ModeVoiceAgent:
		return provideropts.ModalityVoiceAgent
	case speechkit.ModeTTS:
		return provideropts.ModalityTTS
	default:
		return ""
	}
}

func modeForProviderFeature(feature ProviderFeature) speechkit.Mode {
	switch feature {
	case ProviderFeatureDictation, ProviderFeatureDictationStreaming, ProviderFeatureLongTranscription,
		ProviderFeatureSpeakerDiarization, ProviderFeatureSpeakerIdentification:
		return speechkit.ModeDictation
	case ProviderFeatureAssist:
		return speechkit.ModeAssist
	case ProviderFeatureRealtimeVoice:
		return speechkit.ModeVoiceAgent
	case ProviderFeatureTTS:
		return speechkit.ModeTTS
	default:
		return speechkit.ModeNone
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

func providerDisplayName(provider string) string {
	switch NormalizeProviderID(provider) {
	case "local":
		return "Local Built-in"
	case "ollama":
		return "Ollama"
	case "huggingface":
		return "Hugging Face"
	case "openrouter":
		return "OpenRouter"
	case "openai":
		return "OpenAI"
	case "google":
		return "Google"
	case "deepgram":
		return "Deepgram"
	case "assemblyai":
		return "AssemblyAI"
	case "groq":
		return "Groq"
	case "foundry":
		return "Microsoft Foundry"
	case "foundry-voicelive":
		return "Microsoft Foundry Voice Live"
	case "cloudflare":
		return "Cloudflare"
	case "piper":
		return "Piper"
	case "openedai":
		return "OpenAI-compatible local"
	case "selfhosted":
		return "Self-hosted HTTP"
	default:
		return strings.TrimSpace(provider)
	}
}

func providerOrderIndex(provider string) int {
	order := []string{
		"local",
		"ollama",
		"huggingface",
		"openrouter",
		"openai",
		"google",
		"deepgram",
		"assemblyai",
		"foundry",
		"foundry-voicelive",
		"groq",
		"cloudflare",
		"piper",
		"openedai",
		"selfhosted",
	}
	provider = NormalizeProviderID(provider)
	for i, candidate := range order {
		if candidate == provider {
			return i
		}
	}
	return len(order)
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

var providerFeatureOrder = []ProviderFeature{
	ProviderFeatureDictation,
	ProviderFeatureDictationStreaming,
	ProviderFeatureLongTranscription,
	ProviderFeatureSpeakerDiarization,
	ProviderFeatureSpeakerIdentification,
	ProviderFeatureAssist,
	ProviderFeatureRealtimeVoice,
	ProviderFeatureTTS,
}
