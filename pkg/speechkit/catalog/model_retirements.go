package catalog

import "strings"

// ModelRetirement records a vendor model id SpeechKit no longer offers and
// the id that replaces it. Hosts use it to migrate persisted configs so a
// retired id never reaches the wire again.
//
// Provider is a canonical catalog provider id (see [NormalizeProviderID]),
// plus the adapter-scoped ids "foundry-voicelive" (Voice Live brains, which
// the service hosts without a deployment) and "assemblyai-llm-gateway" (the
// AssemblyAI LLM Gateway, whose ids differ from the Gemini API's). Foundry
// deployment names are deliberately absent: they are user-owned names, not
// model ids, and renaming them would point a config at a deployment that
// does not exist.
type ModelRetirement struct {
	Provider    string `json:"provider"`
	ModelID     string `json:"modelId"`
	Replacement string `json:"replacement"`
	// RetiredAt is the day SpeechKit stopped offering the model.
	RetiredAt string `json:"retiredAt"`
	// VendorSunsetAt is the vendor's announced shutdown day, when known.
	VendorSunsetAt string `json:"vendorSunsetAt,omitempty"`
	Reason         string `json:"reason"`
	SourceURL      string `json:"sourceUrl"`
}

// modelRetirementDay is the vendor-doc pass that retired the rows below.
const modelRetirementDay = "2026-09-30"

// Reasons shared by the retirement rows.
const (
	retiredReasonSuperseded = "superseded by a newer model from the same vendor"
	retiredReasonDeprecated = "deprecated by the vendor"
	retiredReasonShutDown   = "shut down by the vendor for new or non-enterprise users"
)

// RetiredModels returns every model SpeechKit retired, with its successor.
// A fresh slice is returned on every call.
func RetiredModels() []ModelRetirement {
	rows := retiredModelRows()
	for i := range rows {
		if rows[i].RetiredAt == "" {
			rows[i].RetiredAt = modelRetirementDay
		}
	}
	return rows
}

func retiredModelRows() []ModelRetirement {
	return []ModelRetirement{
		// OpenAI transcription: the whole gpt-4o transcribe family and
		// whisper-1 were deprecated on 2026-08-26 in favour of gpt-transcribe.
		{Provider: "openai", ModelID: "whisper-1", Replacement: modelOpenAIGPTTranscribe, VendorSunsetAt: "2027-02-26", Reason: retiredReasonDeprecated, SourceURL: "https://developers.openai.com/api/docs/deprecations"},
		{Provider: "openai", ModelID: "gpt-4o-transcribe", Replacement: modelOpenAIGPTTranscribe, VendorSunsetAt: "2027-02-26", Reason: retiredReasonDeprecated, SourceURL: "https://developers.openai.com/api/docs/deprecations"},
		{Provider: "openai", ModelID: "gpt-4o-mini-transcribe", Replacement: modelOpenAIGPTTranscribe, VendorSunsetAt: "2027-02-26", Reason: retiredReasonDeprecated, SourceURL: "https://developers.openai.com/api/docs/deprecations"},
		{Provider: "openai", ModelID: "gpt-4o-transcribe-diarize", Replacement: modelOpenAIGPTTranscribe, VendorSunsetAt: "2027-02-26", Reason: retiredReasonDeprecated, SourceURL: "https://developers.openai.com/api/docs/deprecations"},
		// OpenAI text models: GPT-6 (2026-09-22) and GPT-6.1 Sol (2026-09-29).
		{Provider: "openai", ModelID: "gpt-5.4", Replacement: modelOpenAIGPT61Sol, Reason: retiredReasonSuperseded, SourceURL: "https://openai.com/index/introducing-gpt-6-sol-and-luna/"},
		{Provider: "openai", ModelID: "gpt-5.4-2026-03-05", Replacement: modelOpenAIGPT61Sol, Reason: retiredReasonSuperseded, SourceURL: "https://openai.com/index/introducing-gpt-6-sol-and-luna/"},
		{Provider: "openai", ModelID: "gpt-5.4-mini", Replacement: modelOpenAIGPT6Luna, Reason: retiredReasonSuperseded, SourceURL: "https://openai.com/index/introducing-gpt-6-sol-and-luna/"},
		{Provider: "openai", ModelID: "gpt-5.4-mini-2026-03-17", Replacement: modelOpenAIGPT6Luna, Reason: retiredReasonSuperseded, SourceURL: "https://openai.com/index/introducing-gpt-6-sol-and-luna/"},
		// OpenAI Realtime: 2.1 replaces 2 (Foundry already auto-upgrades 2
		// deployments); the first-generation ids shut down 2027-01-20.
		{Provider: "openai", ModelID: modelOpenAIRealtime2, Replacement: modelOpenAIRealtime21, Reason: retiredReasonSuperseded, SourceURL: "https://developers.openai.com/api/docs/models/gpt-realtime-2.1"},
		{Provider: "openai", ModelID: "gpt-realtime", Replacement: modelOpenAIRealtime21, VendorSunsetAt: "2027-01-20", Reason: retiredReasonDeprecated, SourceURL: "https://developers.openai.com/api/docs/deprecations"},
		{Provider: "openai", ModelID: "gpt-realtime-mini", Replacement: modelOpenAIRealtime21Mini, VendorSunsetAt: "2027-01-20", Reason: retiredReasonDeprecated, SourceURL: "https://developers.openai.com/api/docs/deprecations"},
		// OpenAI speech: gpt-4o-mini-tts replaces the first-generation TTS.
		{Provider: "openai", ModelID: "tts-1", Replacement: modelOpenAIGPT4OMiniTTS, Reason: retiredReasonSuperseded, SourceURL: "https://developers.openai.com/api/docs/models/gpt-4o-mini-tts"},
		{Provider: "openai", ModelID: "tts-1-hd", Replacement: modelOpenAIGPT4OMiniTTS, Reason: retiredReasonSuperseded, SourceURL: "https://developers.openai.com/api/docs/models/gpt-4o-mini-tts"},

		// Microsoft Foundry Voice Live no longer lists gpt-realtime-2 as a
		// native brain; the older mini and gpt-5.4 brains have successors.
		{Provider: "foundry-voicelive", ModelID: modelOpenAIRealtime2, Replacement: modelOpenAIRealtime21, Reason: "no longer a Voice Live native brain", SourceURL: "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live"},
		{Provider: "foundry-voicelive", ModelID: "gpt-realtime-mini", Replacement: modelOpenAIRealtime21Mini, Reason: retiredReasonSuperseded, SourceURL: "https://learn.microsoft.com/azure/foundry/openai/concepts/model-retirement-schedule"},
		{Provider: "foundry-voicelive", ModelID: "gpt-5.4", Replacement: modelFoundryVoiceLiveGPT56Terra, VendorSunsetAt: "2027-09-02", Reason: retiredReasonSuperseded, SourceURL: "https://learn.microsoft.com/azure/ai-services/speech-service/voice-live"},

		// Google Gemini API: 2.5 models are restricted to prior users since
		// 2026-09-18, so a new key gets a 404; the 3.1 Live preview and the
		// 2.5 native-audio preview are superseded by the GA gemini-3.8-live.
		{Provider: "google", ModelID: "gemini-2.5-flash-lite", Replacement: modelGemini35FlashLite, Reason: retiredReasonShutDown, SourceURL: "https://ai.google.dev/gemini-api/docs/changelog"},
		{Provider: "google", ModelID: "gemini-2.5-flash", Replacement: modelGemini38Flash, Reason: retiredReasonShutDown, SourceURL: "https://ai.google.dev/gemini-api/docs/changelog"},
		{Provider: "google", ModelID: "gemini-2.5-pro", Replacement: modelGemini38Flash, Reason: retiredReasonShutDown, SourceURL: "https://ai.google.dev/gemini-api/docs/changelog"},
		{Provider: "google", ModelID: modelGemini31FlashLivePreview, Replacement: modelGemini38Live, Reason: retiredReasonSuperseded, SourceURL: "https://ai.google.dev/gemini-api/docs/deprecations"},
		{Provider: "google", ModelID: modelGemini25FlashNativeAudioPreview, Replacement: modelGemini38Live, Reason: retiredReasonSuperseded, SourceURL: "https://ai.google.dev/gemini-api/docs/deprecations"},

		// Groq shut the Llama 3.x text models down on free and developer
		// tiers on 2026-08-16 and names gpt-oss as the replacements.
		{Provider: "groq", ModelID: "llama-3.1-8b-instant", Replacement: modelGroqGPTOSS20B, VendorSunsetAt: "2026-08-16", Reason: retiredReasonShutDown, SourceURL: "https://console.groq.com/docs/deprecations"},
		{Provider: "groq", ModelID: "llama-3.3-70b-versatile", Replacement: modelGroqGPTOSS120B, VendorSunsetAt: "2026-08-16", Reason: retiredReasonShutDown, SourceURL: "https://console.groq.com/docs/deprecations"},

		// OpenRouter routes the same vendor models, so it inherits their
		// retirements.
		{Provider: "openrouter", ModelID: "openai/whisper-1", Replacement: modelOpenRouterGPTTranscribe, VendorSunsetAt: "2027-02-26", Reason: retiredReasonDeprecated, SourceURL: "https://developers.openai.com/api/docs/deprecations"},
		{Provider: "openrouter", ModelID: "google/gemini-2.5-flash", Replacement: modelOpenRouterGemini35Flash, Reason: retiredReasonShutDown, SourceURL: "https://ai.google.dev/gemini-api/docs/changelog"},
		{Provider: "openrouter", ModelID: "meta-llama/llama-3.1-8b-instruct", Replacement: modelOpenRouterGemini35FlashLite, Reason: retiredReasonSuperseded, SourceURL: "https://openrouter.ai/google/gemini-3.5-flash-lite"},

		// Hugging Face: Qwen3.8 27B (2026-08-14) supersedes Qwen3.5 27B.
		{Provider: "huggingface", ModelID: "Qwen/Qwen3.5-27B", Replacement: modelHFQwen38, Reason: retiredReasonSuperseded, SourceURL: "https://huggingface.co/Qwen/Qwen3.8-27B"},

		// AssemblyAI stopped accepting the Universal-3 Pro streaming ids around
		// 2026-09-25; the pre-recorded Universal-3 Pro id errors as well.
		{Provider: "assemblyai", ModelID: modelAssemblyAIU3RTPro, Replacement: modelAssemblyAIUniversal36ProRealtime, VendorSunsetAt: "2026-09-25", Reason: retiredReasonShutDown, SourceURL: "https://www.assemblyai.com/docs/streaming/select-the-speech-model"},
		{Provider: "assemblyai", ModelID: "u3-pro", Replacement: modelAssemblyAIUniversal36ProRealtime, VendorSunsetAt: "2026-09-25", Reason: retiredReasonShutDown, SourceURL: "https://www.assemblyai.com/docs/streaming/select-the-speech-model"},
		{Provider: "assemblyai", ModelID: "universal-3-pro", Replacement: modelAssemblyAIUniversal35ProRealtime, Reason: retiredReasonShutDown, SourceURL: "https://www.assemblyai.com/docs/getting-started/models"},

		// AssemblyAI LLM Gateway: move the agent tier off Gemini 2.5.
		{Provider: "assemblyai-llm-gateway", ModelID: "gemini-2.5-flash", Replacement: modelAssemblyAIGatewayGemini37Flash, Reason: retiredReasonSuperseded, SourceURL: "https://www.assemblyai.com/docs/llm-gateway/available-models"},
	}
}

// ReplacementModelID returns the successor of a retired model id for
// provider. Retirement chains are followed to the newest id. ok is false when
// modelID is not retired. provider is matched after [NormalizeProviderID]
// except for the adapter-scoped ids listed on [ModelRetirement].
func ReplacementModelID(provider, modelID string) (string, bool) {
	provider = normalizeRetirementProvider(provider)
	current := strings.TrimSpace(modelID)
	if provider == "" || current == "" {
		return "", false
	}
	rows := RetiredModels()
	replaced := false
	// Bound the walk by the table size so a mistaken cycle cannot hang.
	for range rows {
		next, ok := directReplacement(rows, provider, current)
		if !ok {
			break
		}
		current, replaced = next, true
	}
	return current, replaced
}

// CurrentModelID returns modelID, or its successor when modelID is retired.
func CurrentModelID(provider, modelID string) string {
	if replacement, ok := ReplacementModelID(provider, modelID); ok {
		return replacement
	}
	return modelID
}

func directReplacement(rows []ModelRetirement, provider, modelID string) (string, bool) {
	for _, row := range rows {
		if row.Provider == provider && strings.EqualFold(row.ModelID, modelID) {
			return row.Replacement, true
		}
	}
	return "", false
}

func normalizeRetirementProvider(provider string) string {
	value := strings.ToLower(strings.TrimSpace(provider))
	switch value {
	case "foundry-voicelive", "voicelive", "voice-live", "foundry-voice-live":
		return "foundry-voicelive"
	case "assemblyai-llm-gateway", "assemblyai-gateway", "llm-gateway":
		return "assemblyai-llm-gateway"
	default:
		return NormalizeProviderID(value)
	}
}
