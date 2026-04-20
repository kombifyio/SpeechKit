package models

type Modality string

const (
	ModalitySTT           Modality = "stt"
	ModalityTTS           Modality = "tts"
	ModalityRealtimeVoice Modality = "realtime_voice"
	ModalityAssist        Modality = "assist"
	ModalityUtility       Modality = "utility"
	ModalityEmbedding     Modality = "embedding"
	ModalityReranker      Modality = "reranker"
)

type ExecutionMode string

const (
	ExecutionModeLocal          ExecutionMode = "local"
	ExecutionModeSelfHostedHTTP ExecutionMode = "self_hosted_http"
	ExecutionModeHFRouted       ExecutionMode = "hf_routed"
	ExecutionModeHFInference    ExecutionMode = ExecutionModeHFRouted // Legacy alias.
	ExecutionModeOpenAI         ExecutionMode = "openai_api"
	ExecutionModeGroq           ExecutionMode = "groq_api"
	ExecutionModeGoogle         ExecutionMode = "google_api"
	ExecutionModeOllama         ExecutionMode = "ollama_local"
	ExecutionModeOpenRouter     ExecutionMode = "openrouter_api"
)

type ProviderKind string

const (
	ProviderKindLocalBuiltIn   ProviderKind = "local_built_in"
	ProviderKindLocalProvider  ProviderKind = "local_provider"
	ProviderKindCloudProvider  ProviderKind = "cloud_provider"
	ProviderKindDirectProvider ProviderKind = "direct_provider"
)

type Capability string

const (
	CapabilityTranscription         Capability = "transcription"
	CapabilitySTT                   Capability = "stt"
	CapabilityAudioInput            Capability = "audio_input"
	CapabilityLLM                   Capability = "llm"
	CapabilityTTS                   Capability = "tts"
	CapabilityRealtimeAudio         Capability = "realtime_audio"
	CapabilityPipelineFallback      Capability = "pipeline_fallback"
	CapabilityToolCalling           Capability = "tool_calling"
	CapabilityDictionaryPrompt      Capability = "dictionary_prompt"
	CapabilityDictionaryNativeHints Capability = "dictionary_native_hints"
	CapabilitySessionSummary        Capability = "session_summary"
)

type ModelVariant struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ModelID     string `json:"modelId"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

type Profile struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Modality       Modality       `json:"modality"`
	ProviderKind   ProviderKind   `json:"providerKind,omitempty"`
	ExecutionMode  ExecutionMode  `json:"executionMode,omitempty"`
	ModelID        string         `json:"modelId,omitempty"`
	Source         string         `json:"source,omitempty"`
	Description    string         `json:"description,omitempty"`
	License        string         `json:"license,omitempty"`
	Capabilities   []Capability   `json:"capabilities,omitempty"`
	AdapterKind    string         `json:"adapterKind,omitempty"`
	Variants       []ModelVariant `json:"variants,omitempty"`
	AllowInference bool           `json:"inferenceAllowed,omitempty"`
	Default        bool           `json:"default,omitempty"`
	Recommended    bool           `json:"recommended,omitempty"`
	Experimental   bool           `json:"experimental,omitempty"`
}

func (p Profile) HasCapability(capability Capability) bool {
	for _, candidate := range p.Capabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

type Catalog struct {
	Profiles []Profile
}

func DefaultCatalog() Catalog {
	return Catalog{
		Profiles: []Profile{
			// --- STT: Local (download required) ---
			{
				ID:            "stt.local.whispercpp",
				Name:          "Whisper.cpp (Local Built-in)",
				Modality:      ModalitySTT,
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
			// --- STT: Local Provider (Ollama) ---
			{
				ID:             "stt.ollama.gemma4-e4b-transcribe",
				Name:           "Gemma 4 E4B Transcribe (Ollama)",
				Modality:       ModalitySTT,
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
			// --- STT: HuggingFace ---
			{
				ID:             "stt.routed.whisper-large-v3",
				Name:           "Whisper Large v3 (Hugging Face)",
				Modality:       ModalitySTT,
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
			// --- STT: Key provider (OpenAI) ---
			{
				ID:             "stt.openai.whisper-1",
				Name:           "Whisper-1 (OpenAI)",
				Modality:       ModalitySTT,
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
			// --- TTS: HuggingFace ---
			{
				ID:             "tts.routed.qwen3-tts-1.7b",
				Name:           "Qwen3 TTS 1.7B (HuggingFace)",
				Modality:       ModalityTTS,
				ProviderKind:   ProviderKindCloudProvider,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3-TTS-12Hz-1.7B-VoiceDesign",
				Source:         "huggingface",
				License:        "apache-2.0",
				Capabilities:   []Capability{CapabilityTTS},
				AllowInference: true,
			},
			// --- TTS: Key provider (OpenAI) ---
			{
				ID:             "tts.openai.tts-1",
				Name:           "OpenAI TTS-1",
				Modality:       ModalityTTS,
				ProviderKind:   ProviderKindDirectProvider,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "tts-1",
				Source:         "OpenAI",
				License:        "proprietary",
				Capabilities:   []Capability{CapabilityTTS},
				AllowInference: true,
				Default:        true,
				Recommended:    true,
			},
			// --- Utility LLM: Local Built-in ---
			{
				ID:             "utility.builtin.gemma4-e4b",
				Name:           "Gemma 4 E4B (Local Built-in)",
				Modality:       ModalityUtility,
				ProviderKind:   ProviderKindLocalBuiltIn,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "gemma4:e4b",
				Source:         "Local Built-in",
				Description:    "SpeechKit-managed local Gemma runtime for summaries, routing, and command follow-ups.",
				License:        "gemma",
				Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
				AdapterKind:    "genkit_llm",
				AllowInference: true,
				Default:        true,
				Recommended:    true,
			},
			// --- Utility LLM: Local Provider (Ollama) ---
			{
				ID:             "utility.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Ollama)",
				Modality:       ModalityUtility,
				ProviderKind:   ProviderKindLocalProvider,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e4b",
				Source:         "Local Provider",
				Description:    "Externally managed Ollama provider for summaries, routing, and command follow-ups.",
				License:        "gemma",
				Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
				AdapterKind:    "genkit_llm",
				AllowInference: true,
			},
			// --- Utility LLM: HuggingFace ---
			{
				ID:             "utility.routed.qwen35-9b",
				Name:           "Qwen 3.5 9B (Hugging Face)",
				Modality:       ModalityUtility,
				ProviderKind:   ProviderKindCloudProvider,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3.5-9B",
				Source:         "Hugging Face",
				Description:    "Fast open-weight utility model over Hugging Face.",
				License:        "apache-2.0",
				Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
				AdapterKind:    "genkit_llm",
				AllowInference: true,
			},
			// --- Utility LLM: Key provider (OpenAI) ---
			{
				ID:             "utility.openai.gpt-5.4-mini",
				Name:           "GPT-5.4 mini (OpenAI)",
				Modality:       ModalityUtility,
				ProviderKind:   ProviderKindDirectProvider,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "gpt-5.4-mini-2026-03-17",
				Source:         "OpenAI",
				Description:    "Fast paid utility model when you want a single API-key option.",
				License:        "proprietary",
				Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
				AdapterKind:    "genkit_llm",
				AllowInference: true,
			},
			// --- Assist LLM: Local Built-in ---
			{
				ID:            "assist.builtin.gemma4-e4b",
				Name:          "llama.cpp (Local Built-in)",
				Modality:      ModalityAssist,
				ProviderKind:  ProviderKindLocalBuiltIn,
				ExecutionMode: ExecutionModeLocal,
				ModelID:       "gemma4:e4b",
				Source:        "Local Built-in",
				Description:   "SpeechKit-managed llama.cpp runtime for Assist Mode. Download options provide the concrete local GGUF model.",
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
			// --- Assist LLM: Local Provider (Ollama) ---
			{
				ID:             "assist.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Ollama)",
				Modality:       ModalityAssist,
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
			// --- Assist LLM: HuggingFace ---
			{
				ID:             "assist.routed.qwen35-27b",
				Name:           "Qwen 3.5 27B (Hugging Face)",
				Modality:       ModalityAssist,
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
			// --- Assist LLM: Key provider (OpenAI) ---
			{
				ID:             "assist.openai.gpt-5.4",
				Name:           "GPT-5.4 (OpenAI)",
				Modality:       ModalityAssist,
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
			// --- Embedding: Google (default) ---
			{
				ID:             "embedding.google.gemini-embedding-2",
				Name:           "Gemini Embedding 2",
				Modality:       ModalityEmbedding,
				ProviderKind:   ProviderKindDirectProvider,
				ExecutionMode:  ExecutionModeGoogle,
				ModelID:        "gemini-embedding-2",
				Source:         "Google",
				License:        "proprietary",
				AllowInference: true,
				Default:        true,
				Recommended:    true,
			},
			// --- Embedding: HuggingFace (fallback) ---
			{
				ID:             "embedding.routed.bge-m3",
				Name:           "BGE M3 (HuggingFace)",
				Modality:       ModalityEmbedding,
				ProviderKind:   ProviderKindCloudProvider,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "BAAI/bge-m3",
				Source:         "huggingface",
				License:        "mit",
				AllowInference: true,
			},
			// --- Realtime Voice: Local Built-in pipeline fallback ---
			{
				ID:             "realtime.builtin.pipeline",
				Name:           "SpeechKit Local Voice Pipeline",
				Modality:       ModalityRealtimeVoice,
				ProviderKind:   ProviderKindLocalBuiltIn,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "speechkit-local-voice-pipeline",
				Source:         "Local Built-in",
				Description:    "Voice Agent pipeline fallback using SpeechKit-managed local transcription, local LLM, and TTS runtimes.",
				License:        "mixed",
				Capabilities:   []Capability{CapabilityAudioInput, CapabilityPipelineFallback, CapabilitySessionSummary},
				AdapterKind:    "voice_pipeline",
				AllowInference: true,
				Experimental:   true,
			},
			// --- Realtime Voice: Local Provider (Ollama) pipeline fallback ---
			{
				ID:             "realtime.ollama.gemma4-e4b-pipeline",
				Name:           "Gemma 4 E4B Voice Pipeline (Ollama)",
				Modality:       ModalityRealtimeVoice,
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
			// --- Realtime Voice: Hugging Face pipeline fallback ---
			{
				ID:             "realtime.hf.qwen35-27b",
				Name:           "Qwen 3.5 27B Voice Fallback (Hugging Face)",
				Modality:       ModalityRealtimeVoice,
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
			// --- Realtime Voice: Google Gemini Live Native Audio ---
			{
				ID:             "realtime.google.gemini-native-audio",
				Name:           "Gemini Live Native Audio",
				Modality:       ModalityRealtimeVoice,
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
		},
	}
}

func (c Catalog) DefaultProfile(modality Modality) (Profile, bool) {
	for _, profile := range c.Profiles {
		if profile.Modality == modality && profile.Default {
			return profile, true
		}
	}
	return Profile{}, false
}
