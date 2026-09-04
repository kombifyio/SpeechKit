package catalog

import (
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
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
	// Microsoft MAI speech models (Azure Speech on a Foundry resource) and the
	// brains Voice Live hosts without a deployment.
	ModelFoundryMAITranscribe2        = "MAI-Transcribe-2"
	ModelFoundryMAITranscribe15       = "MAI-Transcribe-1.5"
	ModelFoundryMAIVoice2             = "MAI-Voice-2"
	ModelFoundryMAIVoice2Flash        = "MAI-Voice-2-Flash"
	ModelFoundryVoiceLiveRealtimeMini = "gpt-realtime-mini"
	ModelFoundryVoiceLiveGPT5Mini     = "gpt-5-mini"
	ModelFoundryVoiceLivePhi4MM       = "phi4-mm-realtime"
)

// ProviderModelDescriptor is the public source-of-truth row for model IDs that
// SpeechKit treats as framework defaults or first-class live-provider choices.
type ProviderModelDescriptor struct {
	Provider    string                   `json:"provider"`
	ModelID     string                   `json:"modelId"`
	ProfileID   string                   `json:"profileId,omitempty"`
	Mode        speechkit.Mode           `json:"mode"`
	Name        string                   `json:"name"`
	Lifecycle   speechkit.ModelLifecycle `json:"lifecycle"`
	Default     bool                     `json:"default,omitempty"`
	Recommended bool                     `json:"recommended,omitempty"`
	SourceURL   string                   `json:"sourceUrl"`

	// Freshness metadata (kombify-SpeechKit-glnc). Dates are calendar days
	// (YYYY-MM-DD) from vendor documentation. LastVerifiedAt is the day the
	// row was last checked against those docs. TestDefaultModelRegistryFreshnessSLA
	// always fails when a default/recommended row lacks it; the age check
	// against ModelFreshnessSLA only fails under SPEECHKIT_MODEL_FRESHNESS_GATE,
	// which the scheduled model-freshness-gate workflow sets.
	ReleasedAt           string `json:"releasedAt,omitempty"`
	DeprecatedAt         string `json:"deprecatedAt,omitempty"`
	SunsetAt             string `json:"sunsetAt,omitempty"`
	LastVerifiedAt       string `json:"lastVerifiedAt,omitempty"`
	MultilanguageCapable bool   `json:"multilanguageCapable,omitempty"`
}

// ModelFreshnessSLA is the maximum age of LastVerifiedAt before a default
// or recommended model row is considered stale.
const ModelFreshnessSLA = 7 * 24 * time.Hour

// modelRegistryVerifiedAt is the calendar day the registry rows were last
// checked against vendor documentation. Bump this after a vendor-doc pass.
//
// 2026-09-02 pass: all default/recommended rows still listed by their vendors,
// none deprecated. OpenAI now steers new file transcription to gpt-transcribe
// and voice agents to gpt-realtime-2.1; both remain unpromoted here pending
// the documented evaluation (see the gpt-realtime-2.1 note below).
const modelRegistryVerifiedAt = "2026-09-02"

// foundryRegistryVerifiedAt is the day the Microsoft Foundry rows were checked
// against MS Learn model-availability docs (separate vendor-doc pass).
//
// 2026-09-04 pass: the OpenAI realtime rows are unchanged; the MAI rows were
// verified live against a Foundry resource (Speech voices list, fast
// transcription enhancedMode, Voice Live session.update allow-lists).
const foundryRegistryVerifiedAt = "2026-09-04"

// MissingFreshnessReports lists default/recommended registry rows that still
// lack LastVerifiedAt.
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

// StaleFreshnessReports lists default/recommended rows whose LastVerifiedAt
// is missing, unparsable, or older than ModelFreshnessSLA relative to now.
func StaleFreshnessReports(rows []ProviderModelDescriptor, now time.Time) []string {
	var stale []string
	for _, row := range rows {
		if !row.Default && !row.Recommended {
			continue
		}
		verified, ok := parseFreshnessDay(row.LastVerifiedAt)
		if !ok || now.Sub(verified) > ModelFreshnessSLA {
			stale = append(stale, row.Provider+":"+row.ModelID)
		}
	}
	return stale
}

func parseFreshnessDay(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if day, err := time.Parse("2006-01-02", value); err == nil {
		return day, true
	}
	if day, err := time.Parse(time.RFC3339, value); err == nil {
		return day, true
	}
	return time.Time{}, false
}

func DefaultModelRegistry() []ProviderModelDescriptor {
	return []ProviderModelDescriptor{
		{
			Provider:             "assemblyai",
			ModelID:              ModelAssemblyAIUniversal35ProRealtime,
			ProfileID:            "stt.assemblyai.universal",
			Mode:                 speechkit.ModeDictation,
			Name:                 "Universal-3.5 Pro Realtime",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://www.assemblyai.com/docs/streaming/select-the-speech-model",
			ReleasedAt:           "2026-03-03",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "assemblyai",
			ModelID:              ModelAssemblyAIU3RTPro,
			ProfileID:            "stt.assemblyai.universal",
			Mode:                 speechkit.ModeDictation,
			Name:                 "Universal-3 Pro Streaming",
			Lifecycle:            speechkit.ModelLifecycleLegacy,
			SourceURL:            "https://www.assemblyai.com/docs/streaming/select-the-speech-model",
			ReleasedAt:           "2026-03-03",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "assemblyai",
			ModelID:              ModelAssemblyAIVoiceAgent,
			ProfileID:            "realtime.assemblyai.voice-agent",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "AssemblyAI Voice Agent API",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://www.assemblyai.com/docs/voice-agents/voice-agent-api",
			ReleasedAt:           "2026-04-14",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "deepgram",
			ModelID:              ModelDeepgramFluxGeneralMulti,
			ProfileID:            "realtime.deepgram.voice-agent",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Flux General Multilingual",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://developers.deepgram.com/docs/models-languages-overview",
			ReleasedAt:           "2026-04-29",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "deepgram",
			ModelID:              ModelDeepgramFluxGeneralEN,
			ProfileID:            "realtime.deepgram.voice-agent",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Flux General English",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Recommended:          true,
			SourceURL:            "https://developers.deepgram.com/docs/flux/nova-3-migration",
			ReleasedAt:           "2025-10-02",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: false,
		},
		{
			Provider:             "deepgram",
			ModelID:              ModelDeepgramNova3,
			ProfileID:            "stt.deepgram.nova-3",
			Mode:                 speechkit.ModeDictation,
			Name:                 "Nova-3",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://developers.deepgram.com/docs/models-languages-overview",
			ReleasedAt:           "2025-02-12",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "google",
			ModelID:              ModelGemini35LiveTranslatePreview,
			ProfileID:            "realtime.google.gemini-live-translate",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Gemini 3.5 Live Translate Preview",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			Recommended:          true,
			SourceURL:            "https://ai.google.dev/gemini-api/docs/models/gemini-3.5-live-translate-preview",
			ReleasedAt:           "2026-06-09",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "google",
			ModelID:              ModelGemini31FlashLivePreview,
			ProfileID:            "realtime.google.gemini-native-audio",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Gemini 3.1 Flash Live Preview",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://ai.google.dev/gemini-api/docs/models/gemini-3.1-flash-live-preview",
			ReleasedAt:           "2026-03-26",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "google",
			ModelID:              ModelGemini25FlashNativeAudioPreview,
			ProfileID:            "realtime.google.gemini-native-audio",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Gemini 2.5 Flash Native Audio Preview",
			Lifecycle:            speechkit.ModelLifecycleLegacy,
			SourceURL:            "https://ai.google.dev/gemini-api/docs/live-api/capabilities",
			ReleasedAt:           "2025-12-12",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "openai",
			ModelID:              ModelOpenAIGPT4OTranscribe,
			ProfileID:            "stt.openai.gpt-4o-transcribe",
			Mode:                 speechkit.ModeDictation,
			Name:                 "GPT-4o Transcribe",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://platform.openai.com/docs/guides/speech-to-text",
			ReleasedAt:           "2025-03-20",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "openai",
			ModelID:              ModelOpenAIRealtime2,
			ProfileID:            "realtime.openai.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://platform.openai.com/docs/guides/realtime",
			ReleasedAt:           "2026-05-07",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
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
			Provider:             "openai",
			ModelID:              ModelOpenAIRealtime21,
			ProfileID:            "realtime.openai.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              false,
			Recommended:          false,
			SourceURL:            "https://platform.openai.com/docs/guides/realtime",
			ReleasedAt:           "2026-07-06",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "openai",
			ModelID:              ModelOpenAIRealtime21Mini,
			ProfileID:            "realtime.openai.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 mini",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              false,
			Recommended:          false,
			SourceURL:            "https://platform.openai.com/docs/guides/realtime",
			ReleasedAt:           "2026-07-06",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		// Microsoft Foundry serves the OpenAI realtime family through the
		// Azure-hosted v1 surface. ModelID doubles as the default deployment
		// name; users may override it per Foundry deployment.
		{
			Provider:             "foundry",
			ModelID:              ModelOpenAIRealtime2,
			ProfileID:            "realtime.foundry.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2 (Foundry)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://learn.microsoft.com/azure/ai-foundry/openai/concepts/models",
			ReleasedAt:           "2026-05-07",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry",
			ModelID:              ModelOpenAIRealtime21,
			ProfileID:            "realtime.foundry.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 (Foundry)",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			SourceURL:            "https://learn.microsoft.com/azure/ai-foundry/openai/concepts/models",
			ReleasedAt:           "2026-07-06",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry",
			ModelID:              ModelOpenAIRealtime21Mini,
			ProfileID:            "realtime.foundry.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 mini (Foundry)",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			SourceURL:            "https://learn.microsoft.com/azure/ai-foundry/openai/concepts/models",
			ReleasedAt:           "2026-07-06",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		// MAI-Transcribe is served by Azure Speech fast transcription on the
		// Foundry resource; the model id is sent verbatim (no deployment).
		{
			Provider:             "foundry",
			ModelID:              ModelFoundryMAITranscribe2,
			ProfileID:            "stt.foundry.mai-transcribe-2",
			Mode:                 speechkit.ModeDictation,
			Name:                 "MAI-Transcribe-2 (Foundry)",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			Recommended:          true,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/mai-transcribe",
			ReleasedAt:           "2026-09-03",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry",
			ModelID:              ModelFoundryMAITranscribe15,
			ProfileID:            "stt.foundry.mai-transcribe-2",
			Mode:                 speechkit.ModeDictation,
			Name:                 "MAI-Transcribe-1.5 (Foundry)",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/mai-transcribe",
			ReleasedAt:           "2026-07-23",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		// Voice Live brains. The provider id is the Voice Live adapter, not the
		// OpenAI-Realtime-on-Foundry adapter, so the two descriptors list only
		// the models their wire protocol can actually dial.
		{
			Provider:             "foundry-voicelive",
			ModelID:              ModelOpenAIRealtime2,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2 (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2026-05-07",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              ModelFoundryVoiceLiveRealtimeMini,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime mini (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2025-11-18",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              ModelFoundryVoiceLiveGPT5Mini,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT-5 mini (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2025-11-18",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              ModelFoundryVoiceLivePhi4MM,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Phi-4 multimodal realtime (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2025-11-18",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
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
