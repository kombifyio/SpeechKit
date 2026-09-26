package catalog

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// ProviderSupportKind grades how a provider delivers a feature, from
// [ProviderSupportUnsupported] to [ProviderSupportNative]; the matrix keeps
// the best grade any of the provider's profiles reaches.
type ProviderSupportKind string

// Support grades, lowest to highest.
const (
	// ProviderSupportUnsupported means no profile of the provider offers the
	// feature.
	ProviderSupportUnsupported ProviderSupportKind = "unsupported"
	// ProviderSupportPlanned marks an experimental, catalog-only profile that
	// is not yet allowed to run inference.
	ProviderSupportPlanned ProviderSupportKind = "planned"
	// ProviderSupportCascaded means the feature is composed from other
	// capabilities: a voice agent built as an STT/LLM/TTS pipeline, or
	// dictation streaming emulated over batch transcription.
	ProviderSupportCascaded ProviderSupportKind = "cascaded"
	// ProviderSupportRouted means the model is reached through a routing
	// platform (Hugging Face, OpenRouter) rather than the vendor's own API.
	ProviderSupportRouted ProviderSupportKind = "routed"
	// ProviderSupportNative means the provider's own API serves the feature
	// directly.
	ProviderSupportNative ProviderSupportKind = "native"
)

// ProviderFeature is one column of the provider matrix: a product feature a
// setup UI asks "does this provider do it, and how" about.
type ProviderFeature string

// Matrix features, in the order rows list them: five Dictation features, then
// one each for Assist, Voice Agent and TTS.
const (
	ProviderFeatureDictation ProviderFeature = "dictation"
	// ProviderFeatureDictationStreaming is native only for profiles with
	// [speechkit.CapabilityNativeDictationStream]; other dictation profiles
	// grade as cascaded.
	ProviderFeatureDictationStreaming    ProviderFeature = "dictation_streaming"
	ProviderFeatureLongTranscription     ProviderFeature = "long_transcription"
	ProviderFeatureSpeakerDiarization    ProviderFeature = "speaker_diarization"
	ProviderFeatureSpeakerIdentification ProviderFeature = "speaker_identification"
	ProviderFeatureAssist                ProviderFeature = "assist"
	// ProviderFeatureRealtimeVoice is native for realtime-audio voice agents
	// and cascaded for pipeline-fallback ones.
	ProviderFeatureRealtimeVoice ProviderFeature = "realtime_voice"
	ProviderFeatureTTS           ProviderFeature = "tts"
)

// Values of the AuthRequirement and Transport fields of
// [speechkit.ProviderProfile], as filled in by [DefaultProviderAuthRequirement]
// and [DefaultProviderTransport]. Both are semantic classes: hosts map them
// onto their own secret stores and network policy.
const (
	// ProviderAuthNone needs no secret (Ollama, a local provider).
	ProviderAuthNone = "none"
	// ProviderAuthAPIKey needs the vendor's API key.
	ProviderAuthAPIKey = "api_key"
	// ProviderAuthToken needs a platform access token (Hugging Face).
	ProviderAuthToken = "token"
	// ProviderAuthHostDependencies needs no secret but host-installed runtime
	// files, such as the whisper.cpp binary and model of a local built-in.
	ProviderAuthHostDependencies = "host_dependencies"
	// ProviderAuthOptionalAPIKey may take a key, as a self-hosted server might
	// require one.
	ProviderAuthOptionalAPIKey = "optional_api_key"

	// ProviderTransportLocal runs in-process or as a managed subprocess.
	ProviderTransportLocal = "local"
	// ProviderTransportHTTP is a plain-http local or self-hosted endpoint.
	ProviderTransportHTTP = "http"
	// ProviderTransportHTTPS is a public https vendor API.
	ProviderTransportHTTPS = "https"
	// ProviderTransportWebSocket is a native realtime voice session.
	ProviderTransportWebSocket = "websocket"
	// ProviderTransportPipeline is a cascaded STT/LLM/TTS voice agent.
	ProviderTransportPipeline = "pipeline"
)

// ProviderDefault is one catalog profile as a setup UI sees it: the profile's
// identity and flags plus the provider metadata the matrix derives (canonical
// provider id and display name, support grade, native option ids, whether a
// credential is required and where it is stored). It serialises to JSON for
// server settings surfaces.
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

// ProviderFeatureSupport is one matrix cell: the support grade a provider
// reaches for a feature and the profile (mode, profile and model id, native
// options, evidence URL) that earns it. Mode is set even when unsupported.
type ProviderFeatureSupport struct {
	Feature       ProviderFeature     `json:"feature"`
	Support       ProviderSupportKind `json:"support"`
	Mode          speechkit.Mode      `json:"mode,omitempty"`
	ProfileID     string              `json:"profileId,omitempty"`
	ModelID       string              `json:"modelId,omitempty"`
	NativeOptions []string            `json:"nativeOptions,omitempty"`
	EvidenceURL   string              `json:"evidenceUrl,omitempty"`
}

// ProviderMatrixRow is one provider in the setup matrix: its canonical id
// and display name, every profile it contributes (sorted, see
// [DefaultProviderMatrix]) and one [ProviderFeatureSupport] per matrix
// feature.
type ProviderMatrixRow struct {
	Provider    string                   `json:"provider"`
	DisplayName string                   `json:"displayName"`
	Profiles    []ProviderDefault        `json:"profiles"`
	Features    []ProviderFeatureSupport `json:"features"`
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
