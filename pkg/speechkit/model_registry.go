package speechkit

import (
	"strings"
	"time"
)

const (
	ModelAssemblyAIUniversal35ProRealtime = "universal-3-5-pro"
	ModelAssemblyAIU3RTPro                = "u3-rt-pro"
	ModelAssemblyAIVoiceAgent             = "assemblyai-voice-agent"
	ModelDeepgramFluxGeneralEN            = "flux-general-en"
	ModelDeepgramFluxGeneralMulti         = "flux-general-multi"
	// ModelDeepgramFluxTTSDefaultEN is Deepgram's default Flux TTS voice. Flux
	// TTS is English-only; Aura-2 remains the multilingual speak leg.
	ModelDeepgramFluxTTSDefaultEN        = "flux-kit-en"
	ModelDeepgramNova3                   = "nova-3"
	ModelGroqWhisperLargeV3              = "whisper-large-v3"
	ModelGroqWhisperLargeV3Turbo         = "whisper-large-v3-turbo"
	ModelGemini35LiveTranslatePreview    = "gemini-3.5-live-translate-preview"
	ModelGemini31FlashLivePreview        = "gemini-3.1-flash-live-preview"
	ModelGemini25FlashNativeAudioPreview = "gemini-2.5-flash-native-audio-preview-12-2025"
	ModelOpenAIGPT4OTranscribe           = "gpt-4o-transcribe"
	ModelOpenAIGPT4OMiniTranscribe       = "gpt-4o-mini-transcribe"
	ModelOpenAIGPT4OTranscribeDiarize    = "gpt-4o-transcribe-diarize"
	ModelOpenAIRealtime2                 = "gpt-realtime-2"
	ModelOpenAIRealtime21                = "gpt-realtime-2.1"
	ModelOpenAIRealtime21Mini            = "gpt-realtime-2.1-mini"
)

type ModelLifecycle string

const (
	ModelLifecycleGA         ModelLifecycle = "ga"
	ModelLifecyclePreview    ModelLifecycle = "preview"
	ModelLifecycleLegacy     ModelLifecycle = "legacy"
	ModelLifecycleDeprecated ModelLifecycle = "deprecated"
)

// ProviderModelDescriptor is the public source-of-truth row for model IDs that
// SpeechKit treats as framework defaults or first-class live-provider choices.
type ProviderModelDescriptor struct {
	Provider    string         `json:"provider"`
	ModelID     string         `json:"modelId"`
	ProfileID   string         `json:"profileId,omitempty"`
	Mode        Mode           `json:"mode"`
	Name        string         `json:"name"`
	Lifecycle   ModelLifecycle `json:"lifecycle"`
	Default     bool           `json:"default,omitempty"`
	Recommended bool           `json:"recommended,omitempty"`
	SourceURL   string         `json:"sourceUrl"`

	// Freshness metadata (kombify-SpeechKit-glnc). Dates are RFC3339 calendar
	// days from vendor documentation. Leave empty until a row is researched;
	// the staleness gate must stay off until every default/recommended row
	// carries LastVerifiedAt, otherwise the gate would fail closed on noise.
	ReleasedAt           string `json:"releasedAt,omitempty"`
	DeprecatedAt         string `json:"deprecatedAt,omitempty"`
	SunsetAt             string `json:"sunsetAt,omitempty"`
	LastVerifiedAt       string `json:"lastVerifiedAt,omitempty"`
	MultilanguageCapable bool   `json:"multilanguageCapable,omitempty"`
}

// ModelFreshnessSLA is the maximum age of LastVerifiedAt before a default
// model row is considered stale. The CI gate that enforces this is not
// enabled until registry rows are populated from vendor docs.
const ModelFreshnessSLA = 7 * 24 * time.Hour

// MissingFreshnessReports lists default/recommended registry rows that still
// lack LastVerifiedAt. Callers that enable the SLA gate should fail when this
// returns a non-empty list.
func MissingFreshnessReports(rows []ProviderModelDescriptor) []string {
	var missing []string
	for _, row := range rows {
		if !row.Default && !row.Recommended {
			continue
		}
		if strings.TrimSpace(row.LastVerifiedAt) == "" {
			missing = append(missing, row.Provider+":"+row.ModelID)
		}
	}
	return missing
}

func DefaultModelRegistry() []ProviderModelDescriptor {
	return []ProviderModelDescriptor{
		{
			Provider:    "assemblyai",
			ModelID:     ModelAssemblyAIUniversal35ProRealtime,
			ProfileID:   "stt.assemblyai.universal",
			Mode:        ModeDictation,
			Name:        "Universal-3.5 Pro Realtime",
			Lifecycle:   ModelLifecyclePreview,
			Default:     true,
			Recommended: true,
			SourceURL:   "https://www.assemblyai.com/docs/streaming/select-the-speech-model",
		},
		{
			Provider:  "assemblyai",
			ModelID:   ModelAssemblyAIU3RTPro,
			ProfileID: "stt.assemblyai.universal",
			Mode:      ModeDictation,
			Name:      "Universal-3 Pro Streaming",
			Lifecycle: ModelLifecycleLegacy,
			SourceURL: "https://www.assemblyai.com/docs/streaming/select-the-speech-model",
		},
		{
			Provider:    "assemblyai",
			ModelID:     ModelAssemblyAIVoiceAgent,
			ProfileID:   "realtime.assemblyai.voice-agent",
			Mode:        ModeVoiceAgent,
			Name:        "AssemblyAI Voice Agent API",
			Lifecycle:   ModelLifecycleGA,
			Default:     true,
			Recommended: true,
			SourceURL:   "https://www.assemblyai.com/docs/voice-agents/voice-agent-api",
		},
		{
			Provider:    "deepgram",
			ModelID:     ModelDeepgramFluxGeneralMulti,
			ProfileID:   "realtime.deepgram.voice-agent",
			Mode:        ModeVoiceAgent,
			Name:        "Flux General Multilingual",
			Lifecycle:   ModelLifecycleGA,
			Default:     true,
			Recommended: true,
			SourceURL:   "https://developers.deepgram.com/docs/models-languages-overview",
		},
		{
			Provider:    "deepgram",
			ModelID:     ModelDeepgramFluxGeneralEN,
			ProfileID:   "realtime.deepgram.voice-agent",
			Mode:        ModeVoiceAgent,
			Name:        "Flux General English",
			Lifecycle:   ModelLifecycleGA,
			Recommended: true,
			SourceURL:   "https://developers.deepgram.com/docs/flux/nova-3-migration",
		},
		{
			Provider:    "deepgram",
			ModelID:     ModelDeepgramNova3,
			ProfileID:   "stt.deepgram.nova-3",
			Mode:        ModeDictation,
			Name:        "Nova-3",
			Lifecycle:   ModelLifecycleGA,
			Default:     true,
			Recommended: true,
			SourceURL:   "https://developers.deepgram.com/docs/models-languages-overview",
		},
		{
			Provider:    "google",
			ModelID:     ModelGemini35LiveTranslatePreview,
			ProfileID:   "realtime.google.gemini-live-translate",
			Mode:        ModeVoiceAgent,
			Name:        "Gemini 3.5 Live Translate Preview",
			Lifecycle:   ModelLifecyclePreview,
			Recommended: true,
			SourceURL:   "https://ai.google.dev/gemini-api/docs/live-api/translation",
		},
		{
			Provider:    "google",
			ModelID:     ModelGemini31FlashLivePreview,
			ProfileID:   "realtime.google.gemini-native-audio",
			Mode:        ModeVoiceAgent,
			Name:        "Gemini 3.1 Flash Live Preview",
			Lifecycle:   ModelLifecyclePreview,
			Default:     true,
			Recommended: true,
			SourceURL:   "https://ai.google.dev/gemini-api/docs/models/gemini-3.1-flash-live-preview",
		},
		{
			Provider:  "google",
			ModelID:   ModelGemini25FlashNativeAudioPreview,
			ProfileID: "realtime.google.gemini-native-audio",
			Mode:      ModeVoiceAgent,
			Name:      "Gemini 2.5 Flash Native Audio Preview",
			Lifecycle: ModelLifecycleLegacy,
			SourceURL: "https://ai.google.dev/gemini-api/docs/live-api/capabilities",
		},
		{
			Provider:    "openai",
			ModelID:     ModelOpenAIGPT4OTranscribe,
			ProfileID:   "stt.openai.gpt-4o-transcribe",
			Mode:        ModeDictation,
			Name:        "GPT-4o Transcribe",
			Lifecycle:   ModelLifecycleGA,
			Default:     true,
			Recommended: true,
			SourceURL:   "https://platform.openai.com/docs/guides/speech-to-text",
		},
		{
			Provider:    "openai",
			ModelID:     ModelOpenAIRealtime2,
			ProfileID:   "realtime.openai.gpt-realtime-2",
			Mode:        ModeVoiceAgent,
			Name:        "GPT Realtime 2",
			Lifecycle:   ModelLifecycleGA,
			Default:     true,
			Recommended: true,
			SourceURL:   "https://platform.openai.com/docs/guides/realtime",
		},
		// 2.1 and its mini are selectable but neither is Default or
		// Recommended yet. AI-VOICE-SPEECHKIT-TARGET.md names gpt-realtime-2.1
		// "the standing OpenAI promotion candidate" and holds the default until
		// a documented quality/latency/cost evaluation passes; moving Default
		// here without that evidence is exactly what the rule forbids. Listing
		// them is not promotion - it is what lets the evaluation address them
		// by name and what stops a caller from having to guess a model string
		// the registry never heard of.
		{
			Provider:    "openai",
			ModelID:     ModelOpenAIRealtime21,
			ProfileID:   "realtime.openai.gpt-realtime-2",
			Mode:        ModeVoiceAgent,
			Name:        "GPT Realtime 2.1",
			Lifecycle:   ModelLifecycleGA,
			Default:     false,
			Recommended: false,
			SourceURL:   "https://platform.openai.com/docs/guides/realtime",
		},
		{
			Provider:    "openai",
			ModelID:     ModelOpenAIRealtime21Mini,
			ProfileID:   "realtime.openai.gpt-realtime-2",
			Mode:        ModeVoiceAgent,
			Name:        "GPT Realtime 2.1 mini",
			Lifecycle:   ModelLifecycleGA,
			Default:     false,
			Recommended: false,
			SourceURL:   "https://platform.openai.com/docs/guides/realtime",
		},
	}
}

func FindModelDescriptor(provider, modelID string) (ProviderModelDescriptor, bool) {
	for _, descriptor := range DefaultModelRegistry() {
		if descriptor.Provider == provider && descriptor.ModelID == modelID {
			return descriptor, true
		}
	}
	return ProviderModelDescriptor{}, false
}
