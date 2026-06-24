package speechkit

const (
	ModelAssemblyAIUniversal35ProRealtime = "universal-3-5-pro"
	ModelAssemblyAIU3RTPro                = "u3-rt-pro"
	ModelAssemblyAIVoiceAgent             = "assemblyai-voice-agent"
	ModelDeepgramFluxGeneralEN            = "flux-general-en"
	ModelDeepgramFluxGeneralMulti         = "flux-general-multi"
	ModelDeepgramNova3                    = "nova-3"
	ModelGemini31FlashLivePreview         = "gemini-3.1-flash-live-preview"
	ModelGemini25FlashNativeAudioPreview  = "gemini-2.5-flash-native-audio-preview-12-2025"
	ModelOpenAIRealtime2                  = "gpt-realtime-2"
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
			ModelID:     ModelOpenAIRealtime2,
			ProfileID:   "realtime.openai.gpt-realtime-2",
			Mode:        ModeVoiceAgent,
			Name:        "GPT Realtime 2",
			Lifecycle:   ModelLifecycleGA,
			Default:     true,
			Recommended: true,
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
