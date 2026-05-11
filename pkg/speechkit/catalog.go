package speechkit

import "sort"

const DefaultLocalBuiltInLLMModel = "ggml-org/gemma-4-E4B-it-GGUF:Q4_K_M"

// DefaultProviderProfiles returns the built-in framework provider catalog for
// the three strict SpeechKit modes. The Windows desktop host adapts this
// public catalog into its internal runtime model; the catalog itself belongs to
// the reusable framework layer.
func DefaultProviderProfiles() []ProviderProfile {
	return []ProviderProfile{
		{
			ID:            "stt.local.whispercpp",
			Mode:          ModeDictation,
			Name:          "Whisper.cpp (Local Built-in)",
			ProviderKind:  ProviderKindLocalBuiltIn,
			ExecutionMode: ExecutionModeLocal,
			ModelID:       "whisper.cpp",
			Source:        "Local Built-in",
			Description:   "SpeechKit-managed local runtime for Transcribe. Download options provide the concrete Whisper-compatible transcription models.",
			License:       "mit",
			Capabilities:  []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryPrompt},
			AdapterKind:   "stt_router",
			Variants: []ModelVariant{
				{ID: "whisper.ggml-small", Name: "Whisper Small Multilingual", ModelID: "ggml-small.bin"},
				{ID: "whisper.ggml-large-v3-turbo", Name: "Whisper Large v3 Turbo", ModelID: "ggml-large-v3-turbo.bin", Recommended: true},
				{ID: "whisper.ggml-large-v3", Name: "Whisper Large v3", ModelID: "ggml-large-v3.bin"},
			},
			AllowInference: false,
			Default:        true,
			Recommended:    true,
		},
		{
			ID:             "stt.ollama.gemma4-e4b-transcribe",
			Mode:           ModeDictation,
			Name:           "Gemma 4 E4B Transcribe (Ollama)",
			ProviderKind:   ProviderKindLocalProvider,
			ExecutionMode:  ExecutionModeOllama,
			ModelID:        "gemma4:e4b",
			Source:         "Local Provider",
			Description:    "User-managed Ollama provider through SpeechKit's constrained Dictation adapter. Audio in, transcription text out.",
			License:        "gemma",
			Capabilities:   []Capability{CapabilityTranscription, CapabilityAudioInput, CapabilityDictionaryPrompt},
			AdapterKind:    "ollama_transcription",
			AllowInference: true,
			Experimental:   true,
		},
		{
			ID:             "stt.routed.whisper-large-v3",
			Mode:           ModeDictation,
			Name:           "Whisper Large v3 (Hugging Face)",
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeHFRouted,
			ModelID:        "openai/whisper-large-v3",
			Source:         "Hugging Face",
			Description:    "High-accuracy transcription over the Hugging Face Inference Router. Requires an HF token.",
			License:        "apache-2.0",
			Capabilities:   []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryPrompt},
			AdapterKind:    "stt_router",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "stt.openrouter.whisper-1",
			Mode:           ModeDictation,
			Name:           "Whisper-1 (OpenRouter)",
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeOpenRouter,
			ModelID:        "openai/whisper-1",
			Source:         "OpenRouter",
			Description:    "Gateway-routed transcription through OpenRouter's speech-to-text endpoint.",
			License:        "apache-2.0",
			Capabilities:   []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryPrompt},
			AdapterKind:    "openrouter_stt",
			AllowInference: true,
		},
		{
			ID:             "stt.openai.whisper-1",
			Mode:           ModeDictation,
			Name:           "Whisper-1 (OpenAI)",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeOpenAI,
			ModelID:        "whisper-1",
			Source:         "OpenAI",
			Description:    "Simple fallback transcription path when you want to use one paid API key.",
			License:        "apache-2.0",
			Capabilities:   []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryPrompt},
			AdapterKind:    "stt_router",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "stt.google.chirp-3",
			Mode:           ModeDictation,
			Name:           "Chirp 3 (Google AI)",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeGoogle,
			ModelID:        "chirp_3",
			Source:         "Google AI",
			Description:    "Google-hosted transcription path. Requires a dedicated Google STT key; Gemini's GOOGLE_AI_API_KEY does not enable this profile by itself.",
			License:        "proprietary",
			Capabilities:   []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryNativeHints},
			AdapterKind:    "stt_router",
			AllowInference: true,
		},
		{
			ID:             "stt.groq.whisper-large-v3-turbo",
			Mode:           ModeDictation,
			Name:           "Whisper Large v3 Turbo (Groq)",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeGroq,
			ModelID:        "whisper-large-v3-turbo",
			Source:         "Groq",
			Description:    "Fast direct API transcription profile for low-latency dictation fallbacks.",
			License:        "proprietary",
			Capabilities:   []Capability{CapabilityTranscription, CapabilitySTT, CapabilityAudioInput, CapabilityDictionaryPrompt},
			AdapterKind:    "stt_router",
			AllowInference: true,
		},
		{
			ID:            "assist.builtin.gemma4-e4b",
			Mode:          ModeAssist,
			Name:          "Gemma 4 E4B (Local Built-in)",
			ProviderKind:  ProviderKindLocalBuiltIn,
			ExecutionMode: ExecutionModeLocal,
			ModelID:       DefaultLocalBuiltInLLMModel,
			Source:        "Local Built-in",
			Description:   "SpeechKit-managed llama.cpp runtime for Assist. Download options provide concrete GGUF model files.",
			License:       "gemma",
			Capabilities:  []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			Variants: []ModelVariant{
				{ID: "llamacpp.gemma-4-e4b-it-q4-k-m", Name: "Gemma 4 E4B IT Q4_K_M", ModelID: "gemma-4-E4B-it-Q4_K_M.gguf", Description: "Recommended balanced GGUF model for local Assist usage.", Recommended: true},
				{ID: "llamacpp.gemma-4-e2b-it-q8-0", Name: "Gemma 4 E2B IT Q8_0", ModelID: "gemma-4-E2B-it-Q8_0.gguf", Description: "Smaller Gemma 4 GGUF model for lighter local Assist usage."},
			},
			AllowInference: true,
			Default:        true,
			Recommended:    true,
		},
		{
			ID:             "assist.ollama.gemma4-e4b",
			Mode:           ModeAssist,
			Name:           "Gemma 4 E4B (Ollama)",
			ProviderKind:   ProviderKindLocalProvider,
			ExecutionMode:  ExecutionModeOllama,
			ModelID:        "gemma4:e4b",
			Source:         "Local Provider",
			Description:    "Externally managed Ollama provider for Assist Mode.",
			License:        "gemma",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.routed.qwen35-27b",
			Mode:           ModeAssist,
			Name:           "Qwen 3.5 27B (Hugging Face)",
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeHFRouted,
			ModelID:        "Qwen/Qwen3.5-27B",
			Source:         "Hugging Face",
			Description:    "Strong open-weight Assist model over Hugging Face.",
			License:        "apache-2.0",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.openai.gpt-5.4",
			Mode:           ModeAssist,
			Name:           "GPT-5.4 (OpenAI)",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeOpenAI,
			ModelID:        "gpt-5.4-2026-03-05",
			Source:         "OpenAI",
			Description:    "Frontier hosted LLM for the Assist tier.",
			License:        "proprietary",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.google.gemini-2.5-flash",
			Mode:           ModeAssist,
			Name:           "Gemini 2.5 Flash (Google AI)",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeGoogle,
			ModelID:        "gemini-2.5-flash",
			Source:         "Google AI",
			Description:    "Direct Gemini Assist profile for users who enable Google AI for Voice Agent or Assist.",
			License:        "proprietary",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
		},
		{
			ID:             "assist.openrouter.gemini-2.5-flash",
			Mode:           ModeAssist,
			Name:           "Gemini 2.5 Flash (OpenRouter)",
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeOpenRouter,
			ModelID:        "google/gemini-2.5-flash",
			Source:         "OpenRouter",
			Description:    "Gateway-routed Assist profile. OpenRouter is shown as a cloud router, not a direct provider.",
			License:        "proprietary",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
		},
		{
			ID:             "assist.groq.llama-3.3-70b",
			Mode:           ModeAssist,
			Name:           "Llama 3.3 70B (Groq)",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeGroq,
			ModelID:        "llama-3.3-70b-versatile",
			Source:         "Groq",
			Description:    "Groq-hosted Assist profile for fast one-shot responses.",
			License:        "llama",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
		},
		{
			ID:            "realtime.builtin.pipeline",
			Mode:          ModeVoiceAgent,
			Name:          "SpeechKit Local Voice Pipeline",
			ProviderKind:  ProviderKindLocalBuiltIn,
			ExecutionMode: ExecutionModeLocal,
			ModelID:       "speechkit-local-voice-pipeline",
			Source:        "Local Built-in",
			Description:   "Voice Agent pipeline fallback using local transcription, SpeechKit-managed llama.cpp dialogue, and a TTS path.",
			License:       "mixed",
			Capabilities:  []Capability{CapabilityAudioInput, CapabilityPipelineFallback, CapabilitySessionSummary},
			AdapterKind:   "voice_pipeline",
			Variants: []ModelVariant{
				{ID: "llamacpp.gemma-4-e4b-it-q4-k-m-voice", Name: "Gemma 4 E4B IT Q4_K_M", ModelID: "gemma-4-E4B-it-Q4_K_M.gguf", Description: "Recommended balanced GGUF model for local Voice Agent pipeline fallback.", Recommended: true},
				{ID: "llamacpp.gemma-4-e2b-it-q8-0-voice", Name: "Gemma 4 E2B IT Q8_0", ModelID: "gemma-4-E2B-it-Q8_0.gguf", Description: "Smaller Gemma 4 GGUF model for local Voice Agent pipeline fallback."},
			},
			AllowInference: true,
			Default:        true,
			Recommended:    true,
			Experimental:   true,
		},
		{
			ID:             "realtime.ollama.gemma4-e4b-pipeline",
			Mode:           ModeVoiceAgent,
			Name:           "Gemma 4 E4B Voice Pipeline (Ollama)",
			ProviderKind:   ProviderKindLocalProvider,
			ExecutionMode:  ExecutionModeOllama,
			ModelID:        "gemma4:e4b",
			Source:         "Local Provider",
			Description:    "Voice Agent pipeline fallback with Ollama as the dialogue model.",
			License:        "gemma",
			Capabilities:   []Capability{CapabilityAudioInput, CapabilityPipelineFallback, CapabilitySessionSummary},
			AdapterKind:    "voice_pipeline",
			AllowInference: true,
			Experimental:   true,
		},
		{
			ID:             "realtime.hf.qwen35-27b",
			Mode:           ModeVoiceAgent,
			Name:           "Qwen 3.5 27B Voice Fallback (Hugging Face)",
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeHFRouted,
			ModelID:        "Qwen/Qwen3.5-27B",
			Source:         "Hugging Face",
			Description:    "Voice Agent fallback over Hugging Face. SpeechKit uses the capture pipeline when Gemini Live is unavailable or not selected.",
			License:        "apache-2.0",
			Capabilities:   []Capability{CapabilityAudioInput, CapabilityPipelineFallback, CapabilitySessionSummary},
			AdapterKind:    "voice_pipeline",
			AllowInference: true,
			Recommended:    true,
			Experimental:   true,
		},
		{
			ID:             "realtime.openrouter.gemini-2.5-flash-pipeline",
			Mode:           ModeVoiceAgent,
			Name:           "Gemini 2.5 Flash Voice Pipeline (OpenRouter)",
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeOpenRouter,
			ModelID:        "google/gemini-2.5-flash",
			Source:         "OpenRouter",
			Description:    "Voice Agent pipeline fallback with OpenRouter as the dialogue model.",
			License:        "proprietary",
			Capabilities:   []Capability{CapabilityAudioInput, CapabilityPipelineFallback, CapabilitySessionSummary},
			AdapterKind:    "voice_pipeline",
			AllowInference: true,
			Experimental:   true,
		},
		{
			ID:             "realtime.google.gemini-native-audio",
			Mode:           ModeVoiceAgent,
			Name:           "Gemini Live Native Audio",
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeGoogle,
			ModelID:        "gemini-2.5-flash-native-audio-preview-12-2025",
			Source:         "Google",
			Description:    "Native real-time voice conversation over the Google Live API. Requires a Google AI API key.",
			License:        "proprietary",
			Capabilities:   []Capability{CapabilityAudioInput, CapabilityRealtimeAudio, CapabilitySessionSummary},
			AdapterKind:    "gemini_live",
			AllowInference: true,
			Recommended:    true,
		},
	}
}

func ProfilesForMode(mode Mode) []ProviderProfile {
	mode = NormalizeMode(mode)
	var profiles []ProviderProfile
	for _, profile := range DefaultProviderProfiles() {
		if NormalizeMode(profile.Mode) == mode {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

func ProviderKindsForMode(mode Mode) []ProviderKind {
	seen := map[ProviderKind]bool{}
	for _, profile := range ProfilesForMode(mode) {
		seen[profile.ProviderKind] = true
	}
	kinds := make([]ProviderKind, 0, len(seen))
	for _, kind := range []ProviderKind{
		ProviderKindLocalBuiltIn,
		ProviderKindLocalProvider,
		ProviderKindCloudProvider,
		ProviderKindDirectProvider,
	} {
		if seen[kind] {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// ValidateDefaultCatalog verifies the framework invariant that every strict
// mode exposes all four provider groups and every visible profile satisfies its
// mode contract.
func ValidateDefaultCatalog() error {
	for _, mode := range []Mode{ModeDictation, ModeAssist, ModeVoiceAgent} {
		kinds := ProviderKindsForMode(mode)
		if len(kinds) != 4 {
			sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
			return catalogContractError{mode: mode, kinds: kinds}
		}
		for _, profile := range ProfilesForMode(mode) {
			if err := ValidateProfileForMode(profile, mode); err != nil {
				return err
			}
		}
	}
	return nil
}

type catalogContractError struct {
	mode  Mode
	kinds []ProviderKind
}

func (e catalogContractError) Error() string {
	return "speechkit: default catalog does not expose four provider groups for " + string(e.mode)
}
