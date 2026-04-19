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

type Profile struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Modality       Modality      `json:"modality"`
	ExecutionMode  ExecutionMode `json:"executionMode,omitempty"`
	ModelID        string        `json:"modelId,omitempty"`
	Source         string        `json:"source,omitempty"`
	Description    string        `json:"description,omitempty"`
	License        string        `json:"license,omitempty"`
	AllowInference bool          `json:"inferenceAllowed,omitempty"`
	Default        bool          `json:"default,omitempty"`
	Experimental   bool          `json:"experimental,omitempty"`
}

type Catalog struct {
	Profiles []Profile
}

func DefaultCatalog() Catalog {
	return Catalog{
		Profiles: []Profile{
			// --- STT: Local (download required) ---
			{
				ID:             "stt.local.whispercpp",
				Name:           "Whisper.cpp (Local)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "whisper.cpp",
				Source:         "Local",
				Description:    "Offline Windows dictation with Whisper.cpp. Includes Whisper Small, recommends Large v3 Turbo, and also offers full Large v3.",
				License:        "mit",
				AllowInference: false,
				Default:        true,
			},
			// --- STT: HuggingFace ---
			{
				ID:             "stt.routed.whisper-large-v3",
				Name:           "Whisper Large v3 (Hugging Face)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "openai/whisper-large-v3",
				Source:         "Hugging Face",
				Description:    "High-accuracy transcription over the Hugging Face Inference Router. Requires an HF token.",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- STT: Key provider (OpenAI) ---
			{
				ID:             "stt.openai.whisper-1",
				Name:           "Whisper-1 (OpenAI)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "whisper-1",
				Source:         "OpenAI",
				Description:    "Simple fallback transcription path when you want to use one paid API key.",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- TTS: HuggingFace ---
			{
				ID:             "tts.routed.qwen3-tts-1.7b",
				Name:           "Qwen3 TTS 1.7B (HuggingFace)",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3-TTS-12Hz-1.7B-VoiceDesign",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- TTS: Key provider (OpenAI) ---
			{
				ID:             "tts.openai.tts-1",
				Name:           "OpenAI TTS-1",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "tts-1",
				Source:         "OpenAI",
				License:        "proprietary",
				AllowInference: true,
				Default:        true,
			},
			// --- Utility LLM: Local Built-in ---
			{
				ID:             "utility.builtin.gemma4-e4b",
				Name:           "Gemma 4 E4B (Local Built-in)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "gemma4:e4b",
				Source:         "Local Built-in",
				Description:    "SpeechKit-managed local Gemma runtime for summaries, routing, and command follow-ups.",
				License:        "gemma",
				AllowInference: true,
				Default:        true,
			},
			// --- Utility LLM: Local Provider (Ollama) ---
			{
				ID:             "utility.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Ollama)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e4b",
				Source:         "Local Provider",
				Description:    "Externally managed Ollama provider for summaries, routing, and command follow-ups.",
				License:        "gemma",
				AllowInference: true,
			},
			// --- Utility LLM: HuggingFace ---
			{
				ID:             "utility.routed.qwen35-9b",
				Name:           "Qwen 3.5 9B (Hugging Face)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3.5-9B",
				Source:         "Hugging Face",
				Description:    "Fast open-weight utility model over Hugging Face.",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- Utility LLM: Key provider (OpenAI) ---
			{
				ID:             "utility.openai.gpt-5.4-mini",
				Name:           "GPT-5.4 mini (OpenAI)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "gpt-5.4-mini-2026-03-17",
				Source:         "OpenAI",
				Description:    "Fast paid utility model when you want a single API-key option.",
				License:        "proprietary",
				AllowInference: true,
			},
			// --- Assist LLM: Local Built-in ---
			{
				ID:             "assist.builtin.gemma4-e4b",
				Name:           "Gemma 4 E4B (Local Built-in)",
				Modality:       ModalityAssist,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "gemma4:e4b",
				Source:         "Local Built-in",
				Description:    "SpeechKit-managed local Gemma runtime for Assist Mode without leaving the device.",
				License:        "gemma",
				AllowInference: true,
				Default:        true,
			},
			// --- Assist LLM: Local Provider (Ollama) ---
			{
				ID:             "assist.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Ollama)",
				Modality:       ModalityAssist,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e4b",
				Source:         "Local Provider",
				Description:    "Externally managed Ollama provider for Assist Mode.",
				License:        "gemma",
				AllowInference: true,
			},
			// --- Assist LLM: HuggingFace ---
			{
				ID:             "assist.routed.qwen35-27b",
				Name:           "Qwen 3.5 27B (Hugging Face)",
				Modality:       ModalityAssist,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3.5-27B",
				Source:         "Hugging Face",
				Description:    "Strong open-weight Assist model over Hugging Face.",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- Assist LLM: Key provider (OpenAI) ---
			{
				ID:             "assist.openai.gpt-5.4",
				Name:           "GPT-5.4 (OpenAI)",
				Modality:       ModalityAssist,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "gpt-5.4-2026-03-05",
				Source:         "OpenAI",
				Description:    "Frontier hosted LLM for the Assist tier.",
				License:        "proprietary",
				AllowInference: true,
			},
			// --- Embedding: Google (default) ---
			{
				ID:             "embedding.google.gemini-embedding-2",
				Name:           "Gemini Embedding 2",
				Modality:       ModalityEmbedding,
				ExecutionMode:  ExecutionModeGoogle,
				ModelID:        "gemini-embedding-2",
				Source:         "Google",
				License:        "proprietary",
				AllowInference: true,
				Default:        true,
			},
			// --- Embedding: HuggingFace (fallback) ---
			{
				ID:             "embedding.routed.bge-m3",
				Name:           "BGE M3 (HuggingFace)",
				Modality:       ModalityEmbedding,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "BAAI/bge-m3",
				Source:         "huggingface",
				License:        "mit",
				AllowInference: true,
			},
			// --- Realtime Voice: Google Gemini Live Native Audio ---
			{
				ID:             "realtime.hf.qwen35-27b",
				Name:           "Qwen 3.5 27B Voice Fallback (Hugging Face)",
				Modality:       ModalityRealtimeVoice,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3.5-27B",
				Source:         "Hugging Face",
				Description:    "Voice Agent fallback over Hugging Face. SpeechKit uses the capture pipeline when Gemini Live is unavailable or not selected.",
				License:        "apache-2.0",
				AllowInference: true,
				Experimental:   true,
			},
			{
				ID:             "realtime.google.gemini-native-audio",
				Name:           "Gemini Live Native Audio",
				Modality:       ModalityRealtimeVoice,
				ExecutionMode:  ExecutionModeGoogle,
				ModelID:        "gemini-2.5-flash-native-audio-preview-12-2025",
				Source:         "Google",
				Description:    "Native real-time voice conversation over the Google Live API. Requires a Google AI API key.",
				License:        "proprietary",
				AllowInference: true,
				Default:        true,
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
