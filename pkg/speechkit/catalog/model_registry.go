package catalog

import (
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Model ids known to the catalog: the strings vendors accept on the wire (for
// Foundry, the default deployment names), kept as constants so registry rows,
// profiles and retirements share one spelling. They are unexported on purpose:
// vendors retire models, and an exported constant would turn every retirement
// into a breaking API change. Hosts read model ids as data through
// [DefaultModelRegistry], [FindModelDescriptor], [FindProviderDefault] and
// [CurrentModelID] (ADR 0005, decision 2).
const (
	// modelAssemblyAIUniversal35ProRealtime is Universal-3.5 Pro, the
	// AssemblyAI pre-recorded flagship (GA 2026-09-02) that also streams.
	modelAssemblyAIUniversal35ProRealtime = "universal-3-5-pro"
	// modelAssemblyAIUniversal36ProRealtime is Universal-3.6 Pro Realtime
	// (2026-09-29): streaming-only, 32 languages, the streaming default.
	modelAssemblyAIUniversal36ProRealtime = "universal-3-6-pro"
	// modelAssemblyAIU3RTPro is the Universal-3 Pro streaming id AssemblyAI
	// stopped accepting around 2026-09-25. Retired: configs carrying it are
	// upgraded to modelAssemblyAIUniversal36ProRealtime on load.
	modelAssemblyAIU3RTPro = "u3-rt-pro"
	// modelAssemblyAIVoiceAgent is SpeechKit's own id for the AssemblyAI Voice
	// Agent API, which has no vendor model name.
	modelAssemblyAIVoiceAgent = "assemblyai-voice-agent"
	// modelAssemblyAIGatewayGemini37Flash is the newest Gemini the AssemblyAI
	// LLM Gateway lists; it powers the gateway's agent tier.
	modelAssemblyAIGatewayGemini37Flash = "gemini-3.7-flash"
	// modelDeepgramFluxGeneralEN and modelDeepgramFluxGeneralMulti are the
	// English-only and multilingual (code-switching) Flux conversational STT
	// models.
	modelDeepgramFluxGeneralEN    = "flux-general-en"
	modelDeepgramFluxGeneralMulti = "flux-general-multi"
	// modelDeepgramFluxTTSDefaultEN is Deepgram's default Flux TTS voice. Flux
	// TTS (GA 2026-08-12) is English-only; Aura-2 remains the multilingual
	// speak leg.
	modelDeepgramFluxTTSDefaultEN = "flux-kit-en"
	modelDeepgramNova3            = "nova-3"
	// modelGroqWhisperLargeV3 and modelGroqWhisperLargeV3Turbo are the
	// Groq-hosted Whisper variants; Turbo is the Groq dictation default.
	modelGroqWhisperLargeV3      = "whisper-large-v3"
	modelGroqWhisperLargeV3Turbo = "whisper-large-v3-turbo"
	// modelGroqGPTOSS20B and modelGroqGPTOSS120B are the text models Groq
	// names as replacements for its retired Llama 3.x models; modelGroqQwen38
	// is Groq's preview Qwen3.8 27B.
	modelGroqGPTOSS20B  = "openai/gpt-oss-20b"
	modelGroqGPTOSS120B = "openai/gpt-oss-120b"
	modelGroqQwen38     = "qwen/qwen3.8-27b"
	// modelGemini38Live is the GA Gemini Live model (2026-09); it rejects a
	// thinking config. modelGemini38LiveExtendedThinking adds background
	// reasoning and requires non-blocking tools.
	modelGemini38Live                 = "gemini-3.8-live"
	modelGemini38LiveExtendedThinking = "gemini-3.8-live-extended-thinking"
	// modelGemini35LiveTranslatePreview is the Gemini Live speech translation
	// preview.
	modelGemini35LiveTranslatePreview = "gemini-3.5-live-translate-preview"
	// modelGemini31FlashLivePreview and modelGemini25FlashNativeAudioPreview
	// are the Gemini Live previews gemini-3.8-live replaced. Retired: configs
	// carrying them migrate to modelGemini38Live.
	modelGemini31FlashLivePreview        = "gemini-3.1-flash-live-preview"
	modelGemini25FlashNativeAudioPreview = "gemini-2.5-flash-native-audio-preview-12-2025"
	// modelGemini38Flash and modelGemini35FlashLite are the Gemini API text
	// models Google recommends for new projects (2026-09).
	modelGemini38Flash     = "gemini-3.8-flash"
	modelGemini35FlashLite = "gemini-3.5-flash-lite"
	// modelGemini35Transcribe is Google's Gemini Transcribe model on the
	// Gemini API Interactions endpoint.
	modelGemini35Transcribe = "gemini-3.5-transcribe"
	// modelGemini38FlashTTS and modelGemini38FlashLiteTTS are the Gemini 3.8
	// speech models (GA 2026-09-22) on the Interactions endpoint.
	modelGemini38FlashTTS     = "gemini-3.8-flash-tts"
	modelGemini38FlashLiteTTS = "gemini-3.8-flash-lite-tts"
	// modelOpenAIGPTTranscribe is OpenAI's file transcription model (GA
	// 2026-07-28), the successor of the gpt-4o transcribe family and whisper-1.
	modelOpenAIGPTTranscribe = "gpt-transcribe"
	// modelOpenAIGPTLiveTranscribe is OpenAI's streaming transcription model
	// (2026-07-28), used for live dictation on the gpt-transcribe profile.
	modelOpenAIGPTLiveTranscribe = "gpt-live-transcribe"
	// modelOpenAIGPT61Sol, modelOpenAIGPT6Sol, modelOpenAIGPT6Luna and
	// modelOpenAIGPT6Astra are the GPT-6 text models (2026-09). Luna is the
	// efficient high-volume tier, Sol the everyday frontier tier, Astra the
	// flagship.
	modelOpenAIGPT61Sol  = "gpt-6.1-sol"
	modelOpenAIGPT6Sol   = "gpt-6-sol"
	modelOpenAIGPT6Luna  = "gpt-6-luna"
	modelOpenAIGPT6Astra = "gpt-6-astra"
	// modelOpenAIGPT4OMiniTTS is OpenAI's current text-to-speech model.
	modelOpenAIGPT4OMiniTTS = "gpt-4o-mini-tts"
	// modelOpenAIRealtime21 is the default OpenAI Realtime model; the mini is
	// the low-cost variant.
	modelOpenAIRealtime21     = "gpt-realtime-2.1"
	modelOpenAIRealtime21Mini = "gpt-realtime-2.1-mini"
	// modelOpenAIRealtime2 is the Realtime model 2.1 replaced. Retired: configs
	// carrying it migrate to modelOpenAIRealtime21.
	modelOpenAIRealtime2 = "gpt-realtime-2"
	// modelOpenAIGPTLive1 is OpenAI's full-duplex GPT-Live voice model (API
	// launch 2026-09-10), served by the "gpt-live" adapter.
	modelOpenAIGPTLive1 = "gpt-live-1"
	// modelOpenRouterGPTTranscribe, modelOpenRouterGemini35Flash and
	// modelOpenRouterGemini35FlashLite are OpenRouter slugs for the current
	// vendor models.
	modelOpenRouterGPTTranscribe     = "openai/gpt-transcribe"
	modelOpenRouterGemini35Flash     = "google/gemini-3.5-flash"
	modelOpenRouterGemini35FlashLite = "google/gemini-3.5-flash-lite"
	// modelHFQwen38 is Qwen3.8 27B (Apache-2.0, 2026-08-14) on the Hugging
	// Face Inference Router.
	modelHFQwen38 = "Qwen/Qwen3.8-27B"
	// modelFoundryMAITranscribe2, modelFoundryMAIVoice2 and
	// modelFoundryMAIVoice2Flash are Microsoft MAI speech models served by
	// Azure Speech on a Foundry resource.
	modelFoundryMAITranscribe2 = "MAI-Transcribe-2"
	modelFoundryMAIVoice2      = "MAI-Voice-2"
	modelFoundryMAIVoice2Flash = "MAI-Voice-2-Flash"
	// The modelFoundryVoiceLive ids are brains Voice Live hosts without a
	// deployment (native list as of 2026-09-29).
	modelFoundryVoiceLiveGPT56Terra    = "gpt-5.6-terra"
	modelFoundryVoiceLiveGPT56Luna     = "gpt-5.6-luna"
	modelFoundryVoiceLiveAzureRealtime = "azure-realtime"
	modelFoundryVoiceLivePhi4MM        = "phi4-mm-realtime"
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

	// Freshness metadata. Dates are calendar days
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
// 2026-09-30 pass: model ids checked against the vendors' own SDK sources
// (openai-python, python-genai and the Gemini cookbook, assemblyai-python-sdk,
// deepgram-python-sdk, groq-python) and their docs. gpt-transcribe replaces the
// deprecated gpt-4o transcribe family, gpt-realtime-2.1 replaces
// gpt-realtime-2, gemini-3.8-live (GA) replaces the 3.1 Live preview and
// Universal-3.6 Pro Realtime becomes the AssemblyAI streaming default. The
// retired ids and their successors are listed in RetiredModels.
const modelRegistryVerifiedAt = "2026-09-30"

// foundryRegistryVerifiedAt is the day the Microsoft Foundry rows were checked
// against MS Learn model-availability docs (separate vendor-doc pass).
//
// 2026-09-30 pass against the MicrosoftDocs/azure-ai-docs source: the
// gpt-realtime-2.1 family is GA on Foundry and gpt-realtime-2 is a preview
// that auto-upgrades to it; Voice Live dropped gpt-realtime-2 from its native
// brains and added gpt-5.6-terra, gpt-5.6-luna and azure-realtime.
const foundryRegistryVerifiedAt = "2026-09-30"

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

// DefaultModelRegistry returns the built-in model rows: the framework
// defaults and first-class live-provider choices per provider and mode, each
// with its lifecycle and freshness metadata. Provider ids are canonical
// catalog ids ("foundry-voicelive" is the Voice Live adapter, distinct from
// "foundry"). A fresh slice is returned on every call.
func DefaultModelRegistry() []ProviderModelDescriptor {
	return []ProviderModelDescriptor{
		{
			Provider:             "assemblyai",
			ModelID:              modelAssemblyAIUniversal35ProRealtime,
			ProfileID:            "stt.assemblyai.universal",
			Mode:                 speechkit.ModeDictation,
			Name:                 "Universal-3.5 Pro",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://www.assemblyai.com/docs/getting-started/models",
			ReleasedAt:           "2026-03-03",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			// Streaming-only: the pre-recorded API does not accept it, so it
			// is the realtime dictation default while Universal-3.5 Pro stays
			// the batch model.
			Provider:             "assemblyai",
			ModelID:              modelAssemblyAIUniversal36ProRealtime,
			ProfileID:            "stt.assemblyai.universal",
			Mode:                 speechkit.ModeDictation,
			Name:                 "Universal-3.6 Pro Realtime",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Recommended:          true,
			SourceURL:            "https://www.assemblyai.com/blog/universal-3-6-pro-realtime",
			ReleasedAt:           "2026-09-29",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "assemblyai",
			ModelID:              modelAssemblyAIVoiceAgent,
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
			ModelID:              modelDeepgramFluxGeneralMulti,
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
			ModelID:              modelDeepgramFluxGeneralEN,
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
			ModelID:              modelDeepgramNova3,
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
			ModelID:              modelGemini35LiveTranslatePreview,
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
			ModelID:              modelGemini38Live,
			ProfileID:            "realtime.google.gemini-native-audio",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Gemini 3.8 Live",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://ai.google.dev/gemini-api/docs/models/gemini-3.8-live",
			ReleasedAt:           "2026-09-15",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "google",
			ModelID:              modelGemini38LiveExtendedThinking,
			ProfileID:            "realtime.google.gemini-native-audio",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Gemini 3.8 Live Extended Thinking",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			SourceURL:            "https://ai.google.dev/gemini-api/docs/models/gemini-3.8-live-extended-thinking",
			ReleasedAt:           "2026-09-15",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "openai",
			ModelID:              modelOpenAIGPTTranscribe,
			ProfileID:            "stt.openai.gpt-transcribe",
			Mode:                 speechkit.ModeDictation,
			Name:                 "GPT Transcribe",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://developers.openai.com/api/docs/models/gpt-transcribe",
			ReleasedAt:           "2026-07-28",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "openai",
			ModelID:              modelOpenAIGPTLiveTranscribe,
			ProfileID:            "stt.openai.gpt-transcribe",
			Mode:                 speechkit.ModeDictation,
			Name:                 "GPT Live Transcribe",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://developers.openai.com/api/docs/models/gpt-live-transcribe",
			ReleasedAt:           "2026-07-28",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "openai",
			ModelID:              modelOpenAIRealtime21,
			ProfileID:            "realtime.openai.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://developers.openai.com/api/docs/models/gpt-realtime-2.1",
			ReleasedAt:           "2026-07-06",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "openai",
			ModelID:              modelOpenAIRealtime21Mini,
			ProfileID:            "realtime.openai.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 mini",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://developers.openai.com/api/docs/models/gpt-realtime-2.1",
			ReleasedAt:           "2026-07-06",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "gpt-live",
			ModelID:              modelOpenAIGPTLive1,
			ProfileID:            "realtime.openai.gpt-live-1",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT-Live 1",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			Default:              true,
			SourceURL:            "https://developers.openai.com/api/docs/models/gpt-live-1",
			ReleasedAt:           "2026-09-10",
			LastVerifiedAt:       modelRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		// Microsoft Foundry serves the OpenAI realtime family through the
		// Azure-hosted v1 surface. ModelID doubles as the default deployment
		// name; users may override it per Foundry deployment.
		{
			Provider:             "foundry",
			ModelID:              modelOpenAIRealtime21,
			ProfileID:            "realtime.foundry.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 (Foundry)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://learn.microsoft.com/azure/foundry/openai/concepts/realtime-2",
			ReleasedAt:           "2026-07-07",
			SunsetAt:             "2027-06-25",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry",
			ModelID:              modelOpenAIRealtime21Mini,
			ProfileID:            "realtime.foundry.gpt-realtime-2",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 mini (Foundry)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/foundry/openai/concepts/realtime-2",
			ReleasedAt:           "2026-07-07",
			SunsetAt:             "2027-06-25",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		// GPT-Live on Foundry speaks the OpenAI GPT-Live protocol from
		// /openai/v1/live/sessions; ModelID doubles as the default deployment
		// name, as for the realtime rows above.
		{
			Provider:             "foundry-gpt-live",
			ModelID:              modelOpenAIGPTLive1,
			ProfileID:            "realtime.foundry.gpt-live-1",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT-Live 1 (Foundry)",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			Default:              true,
			SourceURL:            "https://learn.microsoft.com/azure/foundry/openai/how-to/gpt-live",
			ReleasedAt:           "2026-09-10",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		// MAI-Transcribe is served by Azure Speech fast transcription on the
		// Foundry resource; the model id is sent verbatim (no deployment).
		{
			Provider:             "foundry",
			ModelID:              modelFoundryMAITranscribe2,
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
		// Voice Live brains. The provider id is the Voice Live adapter, not the
		// OpenAI-Realtime-on-Foundry adapter, so the two descriptors list only
		// the models their wire protocol can actually dial.
		{
			Provider:             "foundry-voicelive",
			ModelID:              modelOpenAIRealtime21,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			Default:              true,
			Recommended:          true,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2026-07-07",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              modelOpenAIRealtime21Mini,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT Realtime 2.1 mini (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2026-07-07",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              modelFoundryVoiceLiveGPT56Terra,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT-5.6 Terra (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2026-07-09",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              modelFoundryVoiceLiveGPT56Luna,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "GPT-5.6 Luna (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2026-07-09",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              modelFoundryVoiceLiveAzureRealtime,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Azure Realtime (Voice Live)",
			Lifecycle:            speechkit.ModelLifecycleGA,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2026-07-25",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
		{
			Provider:             "foundry-voicelive",
			ModelID:              modelFoundryVoiceLivePhi4MM,
			ProfileID:            "realtime.foundry.voice-live",
			Mode:                 speechkit.ModeVoiceAgent,
			Name:                 "Phi-4 multimodal realtime (Voice Live)",
			Lifecycle:            speechkit.ModelLifecyclePreview,
			SourceURL:            "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live",
			ReleasedAt:           "2025-11-18",
			LastVerifiedAt:       foundryRegistryVerifiedAt,
			MultilanguageCapable: true,
		},
	}
}

// FindModelDescriptor looks a registry row up by exact (not normalised)
// provider id and model id; ok is false when the registry has no such row.
func FindModelDescriptor(provider, modelID string) (ProviderModelDescriptor, bool) {
	for _, descriptor := range DefaultModelRegistry() {
		if descriptor.Provider == provider && descriptor.ModelID == modelID {
			return descriptor, true
		}
	}
	return ProviderModelDescriptor{}, false
}
