package speechkit

import (
	"sort"
	"strings"

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
	Provider           string              `json:"provider"`
	DisplayName        string              `json:"displayName"`
	Mode               Mode                `json:"mode"`
	ProfileID          string              `json:"profileId"`
	ModelID            string              `json:"modelId,omitempty"`
	ProviderKind       ProviderKind        `json:"providerKind"`
	ExecutionMode      ExecutionMode       `json:"executionMode,omitempty"`
	Support            ProviderSupportKind `json:"support"`
	Capabilities       []Capability        `json:"capabilities,omitempty"`
	NativeOptions      []string            `json:"nativeOptions,omitempty"`
	AuthRequirement    string              `json:"authRequirement,omitempty"`
	CredentialRequired bool                `json:"credentialRequired"`
	CredentialTarget   string              `json:"credentialTarget,omitempty"`
	Transport          string              `json:"transport,omitempty"`
	EvidenceURL        string              `json:"evidenceUrl,omitempty"`
	Default            bool                `json:"default,omitempty"`
	Recommended        bool                `json:"recommended,omitempty"`
	Experimental       bool                `json:"experimental,omitempty"`
	Variants           []ModelVariant      `json:"variants,omitempty"`
}

type ProviderFeatureSupport struct {
	Feature       ProviderFeature     `json:"feature"`
	Support       ProviderSupportKind `json:"support"`
	Mode          Mode                `json:"mode,omitempty"`
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

func NormalizeProviderID(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	value = strings.ReplaceAll(value, "_", "-")
	switch {
	case value == "":
		return ""
	case value == "builtin", value == "local-built-in", value == "local":
		return "local"
	case value == "hf", value == "hf-routed", value == "hugging-face", value == "huggingface":
		return "huggingface"
	case value == "open-router", value == "openrouter":
		return "openrouter"
	case value == "google-ai", value == "google-cloud", value == "gemini", value == "gemini-live", value == "google":
		return "google"
	case value == "assembly-ai", value == "assemblyai":
		return "assemblyai"
	case value == "openai-compatible", value == "openedai-speech", value == "openedai":
		return "openedai"
	case strings.HasPrefix(value, "stt.local."), strings.HasPrefix(value, "assist.builtin."),
		strings.HasPrefix(value, "realtime.builtin."), strings.HasPrefix(value, "tts.local."):
		return "local"
	case strings.HasPrefix(value, "stt.ollama."), strings.HasPrefix(value, "assist.ollama."),
		strings.HasPrefix(value, "realtime.ollama."):
		return "ollama"
	case strings.HasPrefix(value, "stt.routed."), strings.HasPrefix(value, "assist.routed."),
		strings.HasPrefix(value, "realtime.hf."), strings.HasPrefix(value, "tts.huggingface."):
		return "huggingface"
	case strings.HasPrefix(value, "stt.openrouter."), strings.HasPrefix(value, "assist.openrouter."),
		strings.HasPrefix(value, "realtime.openrouter."):
		return "openrouter"
	case strings.HasPrefix(value, "stt.openai."), strings.HasPrefix(value, "assist.openai."),
		strings.HasPrefix(value, "realtime.openai."), strings.HasPrefix(value, "tts.openai."):
		return "openai"
	case strings.HasPrefix(value, "stt.google."), strings.HasPrefix(value, "assist.google."),
		strings.HasPrefix(value, "realtime.google."), strings.HasPrefix(value, "tts.google."):
		return "google"
	case strings.HasPrefix(value, "stt.deepgram."), strings.HasPrefix(value, "realtime.deepgram."),
		strings.HasPrefix(value, "tts.deepgram."), strings.HasPrefix(value, "speaker.deepgram."):
		return "deepgram"
	case strings.HasPrefix(value, "stt.assemblyai."), strings.HasPrefix(value, "realtime.assemblyai."),
		strings.HasPrefix(value, "speaker.assemblyai."):
		return "assemblyai"
	case strings.HasPrefix(value, "stt.groq."), strings.HasPrefix(value, "assist.groq."):
		return "groq"
	case strings.HasPrefix(value, "tts.openedai."):
		return "openedai"
	default:
		return value
	}
}

func ProviderIDForProfile(profile ProviderProfile) string {
	if provider := NormalizeProviderID(profile.Provider); provider != "" {
		return provider
	}
	if provider := NormalizeProviderID(profile.ID); provider != "" && !strings.Contains(provider, ".") {
		return provider
	}
	return ProviderIDForExecutionMode(profile.ExecutionMode)
}

func ProviderIDForExecutionMode(mode ExecutionMode) string {
	switch mode {
	case ExecutionModeLocal:
		return "local"
	case ExecutionModeSelfHostedHTTP:
		return "selfhosted"
	case ExecutionModeHFRouted:
		return "huggingface"
	case ExecutionModeOpenAI:
		return "openai"
	case ExecutionModeGroq:
		return "groq"
	case ExecutionModeGoogle:
		return "google"
	case ExecutionModeDeepgram:
		return "deepgram"
	case ExecutionModeAssemblyAI:
		return "assemblyai"
	case ExecutionModeOllama:
		return "ollama"
	case ExecutionModeOpenRouter:
		return "openrouter"
	default:
		return ""
	}
}

// ProviderProfileWithDefaults returns a copy with framework-standard provider
// metadata filled in. Explicit profile metadata wins; missing provider,
// credential, and transport fields are derived from the canonical provider id,
// execution mode, and mode capabilities.
func ProviderProfileWithDefaults(profile ProviderProfile) ProviderProfile {
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
func DefaultProviderAuthRequirement(profile ProviderProfile) string {
	if value := strings.TrimSpace(profile.AuthRequirement); value != "" {
		return value
	}
	switch profile.ExecutionMode {
	case ExecutionModeLocal:
		if profile.ProviderKind == ProviderKindLocalBuiltIn {
			return ProviderAuthHostDependencies
		}
		return ProviderAuthNone
	case ExecutionModeOllama:
		return ProviderAuthNone
	case ExecutionModeSelfHostedHTTP:
		return ProviderAuthOptionalAPIKey
	case ExecutionModeHFRouted:
		return ProviderAuthToken
	case ExecutionModeOpenAI, ExecutionModeGroq, ExecutionModeGoogle, ExecutionModeDeepgram,
		ExecutionModeAssemblyAI, ExecutionModeOpenRouter:
		return ProviderAuthAPIKey
	default:
		return ""
	}
}

// DefaultProviderTransport exposes the dominant runtime transport class for a
// profile. Native realtime providers use websocket; cascaded voice providers
// use pipeline; batch/provider APIs use HTTPS/HTTP/local.
func DefaultProviderTransport(profile ProviderProfile) string {
	if value := strings.TrimSpace(profile.Transport); value != "" {
		return value
	}
	if NormalizeMode(profile.Mode) == ModeVoiceAgent {
		if profile.HasCapability(CapabilityRealtimeAudio) {
			return ProviderTransportWebSocket
		}
		if profile.HasCapability(CapabilityPipelineFallback) {
			return ProviderTransportPipeline
		}
	}
	switch profile.ExecutionMode {
	case ExecutionModeLocal:
		return ProviderTransportLocal
	case ExecutionModeOllama, ExecutionModeSelfHostedHTTP:
		return ProviderTransportHTTP
	case ExecutionModeHFRouted, ExecutionModeOpenAI, ExecutionModeGroq, ExecutionModeGoogle,
		ExecutionModeDeepgram, ExecutionModeAssemblyAI, ExecutionModeOpenRouter:
		return ProviderTransportHTTPS
	default:
		return ""
	}
}

func ProviderProfileRequiresCredential(profile ProviderProfile) bool {
	switch DefaultProviderAuthRequirement(profile) {
	case "", ProviderAuthNone, ProviderAuthHostDependencies, ProviderAuthOptionalAPIKey:
		return false
	default:
		return true
	}
}

func ProviderCredentialTarget(profile ProviderProfile) string {
	if !ProviderProfileRequiresCredential(profile) {
		return ""
	}
	provider := ProviderIDForProfile(profile)
	if provider == "google" && NormalizeMode(profile.Mode) == ModeDictation {
		return "google_stt"
	}
	return provider
}

func DefaultProviderMatrix() []ProviderMatrixRow {
	grouped := map[string][]ProviderDefault{}
	for _, profile := range DefaultProviderProfiles() {
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
	var out []ProviderDefault
	for _, row := range DefaultProviderMatrix() {
		for _, mode := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent, ModeTTS} {
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

func FindProviderDefault(provider string, mode Mode) (ProviderDefault, bool) {
	provider = NormalizeProviderID(provider)
	mode = NormalizeMode(mode)
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

func providerDefaultFromProfile(provider string, profile ProviderProfile) ProviderDefault {
	profile = ProviderProfileWithDefaults(profile)
	return ProviderDefault{
		Provider:           provider,
		DisplayName:        providerDisplayName(provider),
		Mode:               NormalizeMode(profile.Mode),
		ProfileID:          profile.ID,
		ModelID:            profile.ModelID,
		ProviderKind:       profile.ProviderKind,
		ExecutionMode:      profile.ExecutionMode,
		Support:            supportKindForProfile(profile),
		Capabilities:       append([]Capability(nil), profile.Capabilities...),
		NativeOptions:      nativeOptionsForProfile(provider, profile),
		AuthRequirement:    profile.AuthRequirement,
		CredentialRequired: ProviderProfileRequiresCredential(profile),
		CredentialTarget:   ProviderCredentialTarget(profile),
		Transport:          profile.Transport,
		EvidenceURL:        profile.EvidenceURL,
		Default:            profile.Default,
		Recommended:        profile.Recommended,
		Experimental:       profile.Experimental,
		Variants:           append([]ModelVariant(nil), profile.Variants...),
	}
}

func supportKindForProfile(profile ProviderProfile) ProviderSupportKind {
	if profile.Experimental && !profile.AllowInference {
		return ProviderSupportPlanned
	}
	if NormalizeMode(profile.Mode) == ModeVoiceAgent && profile.HasCapability(CapabilityPipelineFallback) {
		return ProviderSupportCascaded
	}
	switch profile.ExecutionMode {
	case ExecutionModeHFRouted, ExecutionModeOpenRouter:
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
		return support, profile.Mode == ModeDictation && providerDefaultHasCapability(profile, CapabilityTranscription)
	case ProviderFeatureDictationStreaming:
		if profile.Mode != ModeDictation || !providerDefaultHasCapability(profile, CapabilityTranscription) {
			return ProviderFeatureSupport{}, false
		}
		if providerDefaultHasCapability(profile, CapabilityNativeDictationStream) {
			support.Support = ProviderSupportNative
		} else {
			support.Support = ProviderSupportCascaded
		}
		return support, true
	case ProviderFeatureLongTranscription:
		return support, profile.Mode == ModeDictation && providerDefaultHasCapability(profile, CapabilityTranscription)
	case ProviderFeatureSpeakerDiarization:
		return support, profile.Mode == ModeDictation &&
			(providerDefaultHasCapability(profile, CapabilitySpeakerDiarization) ||
				providerDefaultHasCapability(profile, CapabilitySpeakerAttribution) ||
				providerDefaultHasCapability(profile, CapabilitySpeakerIdentification))
	case ProviderFeatureSpeakerIdentification:
		return support, profile.Mode == ModeDictation &&
			(providerDefaultHasCapability(profile, CapabilitySpeakerIdentification) ||
				providerDefaultHasCapability(profile, CapabilitySpeakerAttribution))
	case ProviderFeatureAssist:
		return support, profile.Mode == ModeAssist && providerDefaultHasCapability(profile, CapabilityLLM)
	case ProviderFeatureRealtimeVoice:
		if profile.Mode != ModeVoiceAgent {
			return ProviderFeatureSupport{}, false
		}
		if providerDefaultHasCapability(profile, CapabilityRealtimeAudio) {
			return support, true
		}
		if providerDefaultHasCapability(profile, CapabilityPipelineFallback) {
			support.Support = ProviderSupportCascaded
			return support, true
		}
		return ProviderFeatureSupport{}, false
	case ProviderFeatureTTS:
		return support, profile.Mode == ModeTTS && providerDefaultHasCapability(profile, CapabilityTTS)
	default:
		return ProviderFeatureSupport{}, false
	}
}

func nativeOptionsForProfile(provider string, profile ProviderProfile) []string {
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

func modalityForProviderMode(mode Mode) string {
	switch NormalizeMode(mode) {
	case ModeDictation:
		return provideropts.ModalitySTT
	case ModeVoiceAgent:
		return provideropts.ModalityVoiceAgent
	case ModeTTS:
		return provideropts.ModalityTTS
	default:
		return ""
	}
}

func modeForProviderFeature(feature ProviderFeature) Mode {
	switch feature {
	case ProviderFeatureDictation, ProviderFeatureDictationStreaming, ProviderFeatureLongTranscription,
		ProviderFeatureSpeakerDiarization, ProviderFeatureSpeakerIdentification:
		return ModeDictation
	case ProviderFeatureAssist:
		return ModeAssist
	case ProviderFeatureRealtimeVoice:
		return ModeVoiceAgent
	case ProviderFeatureTTS:
		return ModeTTS
	default:
		return ModeNone
	}
}

func preferredProviderDefault(profiles []ProviderDefault, mode Mode) (ProviderDefault, bool) {
	mode = NormalizeMode(mode)
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

func providerDefaultHasCapability(profile ProviderDefault, capability Capability) bool {
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
		"groq",
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

func modeOrderIndex(mode Mode) int {
	switch NormalizeMode(mode) {
	case ModeDictation:
		return 0
	case ModeAssist:
		return 1
	case ModeVoiceAgent:
		return 2
	case ModeTTS:
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
