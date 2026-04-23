package speechkit

import "sort"

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
			ID:            "assist.builtin.gemma4-e4b",
			Mode:          ModeAssist,
			Name:          "llama.cpp (Local Built-in)",
			ProviderKind:  ProviderKindLocalBuiltIn,
			ExecutionMode: ExecutionModeLocal,
			ModelID:       "gemma4:e4b",
			Source:        "Local Built-in",
			Description:   "SpeechKit-managed llama.cpp runtime for Assist. Download options provide concrete GGUF model files.",
			License:       "gemma",
			Capabilities:  []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			Variants: []ModelVariant{
				{ID: "llamacpp.gemma-3-4b-it-q4-k-m", Name: "Gemma 3 4B IT Q4_K_M", ModelID: "gemma-3-4b-it-Q4_K_M.gguf", Description: "Balanced GGUF model for local Assist usage.", Recommended: true},
				{ID: "llamacpp.gemma-3-4b-it-q8-0", Name: "Gemma 3 4B IT Q8_0", ModelID: "gemma-3-4b-it-Q8_0.gguf", Description: "Larger GGUF model when local quality is more important than disk and memory use."},
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
				{ID: "llamacpp.gemma-3-4b-it-q4-k-m-voice", Name: "Gemma 3 4B IT Q4_K_M", ModelID: "gemma-3-4b-it-Q4_K_M.gguf", Description: "Balanced GGUF model for local Voice Agent pipeline fallback.", Recommended: true},
				{ID: "llamacpp.gemma-3-4b-it-q8-0-voice", Name: "Gemma 3 4B IT Q8_0", ModelID: "gemma-3-4b-it-Q8_0.gguf", Description: "Larger GGUF model for local Voice Agent pipeline fallback."},
			},
			AllowInference: true,
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
			Default:        true,
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
