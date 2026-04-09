package models

type Modality string

const (
	ModalitySTT           Modality = "stt"
	ModalityTTS           Modality = "tts"
	ModalityRealtimeVoice Modality = "realtime_voice"
	ModalityAgent         Modality = "agent"
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
				Description:    "Offline Windows dictation. Download a model to get started.",
				License:        "mit",
				AllowInference: false,
				Default:        true,
			},
			// --- STT: HuggingFace ---
			{
				ID:             "stt.routed.whisper-large-v3",
				Name:           "Whisper Large v3 (HuggingFace)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "openai/whisper-large-v3",
				Source:         "huggingface",
				Description:    "High-accuracy transcription via HuggingFace Inference. Requires HF token.",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- STT: Key provider (Groq — fast, free tier) ---
			{
				ID:             "stt.groq.whisper-large-v3-turbo",
				Name:           "Whisper Large v3 Turbo (Groq)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeGroq,
				ModelID:        "whisper-large-v3-turbo",
				Source:         "Groq",
				Description:    "Ultra-fast cloud transcription. Free Groq tier available.",
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
			// --- Utility LLM: Local (Ollama download) ---
			{
				ID:             "utility.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Local)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e4b",
				Source:         "Local (Ollama)",
				Description:    "Best local LLM for Assist Mode on modern laptops. Download via Ollama.",
				License:        "gemma",
				AllowInference: true,
				Default:        true,
			},
			// --- Utility LLM: HuggingFace ---
			{
				ID:             "utility.routed.qwen25-7b",
				Name:           "Qwen 2.5 7B (HuggingFace)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen2.5-7B-Instruct",
				Source:         "huggingface",
				Description:    "Fast and capable open-weight LLM via HuggingFace Inference.",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- Utility LLM: Key provider (Groq) ---
			{
				ID:             "utility.groq.llama-3.1-8b",
				Name:           "LLaMA 3.1 8B (Groq)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeGroq,
				ModelID:        "llama-3.1-8b-instant",
				Source:         "Groq",
				Description:    "Fastest cloud LLM for quick actions. Free Groq tier available.",
				License:        "llama3.1",
				AllowInference: true,
			},
			// --- Utility LLM: OpenRouter ---
			{
				ID:             "utility.openrouter.llama-3.1-8b",
				Name:           "LLaMA 3.1 8B (OpenRouter)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOpenRouter,
				ModelID:        "meta-llama/llama-3.1-8b-instruct",
				Source:         "OpenRouter",
				Description:    "Flexible routing via OpenRouter. Single key for 300+ models.",
				License:        "llama3.1",
				AllowInference: true,
			},
			// --- Agent LLM: Local (Ollama download) ---
			{
				ID:             "agent.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Local)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e4b",
				Source:         "Local (Ollama)",
				Description:    "Best local agent model for Assist Mode on typical Windows hardware.",
				License:        "gemma",
				AllowInference: true,
				Default:        true,
			},
			// --- Agent LLM: HuggingFace ---
			{
				ID:             "agent.routed.qwen25-32b",
				Name:           "Qwen 2.5 32B (HuggingFace)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen2.5-32B-Instruct",
				Source:         "huggingface",
				Description:    "Strong open-weight reasoning model via HuggingFace Inference.",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- Agent LLM: Key provider (Groq) ---
			{
				ID:             "agent.groq.llama-3.3-70b",
				Name:           "LLaMA 3.3 70B (Groq)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeGroq,
				ModelID:        "llama-3.3-70b-versatile",
				Source:         "Groq",
				Description:    "High-capability cloud agent. Fast Groq inference, generous free tier.",
				License:        "llama3.3",
				AllowInference: true,
			},
			// --- Agent LLM: OpenRouter ---
			{
				ID:             "agent.openrouter.gemini-25-flash",
				Name:           "Gemini 2.5 Flash (OpenRouter)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeOpenRouter,
				ModelID:        "google/gemini-2.5-flash",
				Source:         "OpenRouter",
				Description:    "Fast and capable Gemini model via OpenRouter.",
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
			// --- Realtime Voice: Google Gemini Live ---
			{
				ID:             "realtime.google.gemini-live-25-flash",
				Name:           "Gemini Live 2.5 Flash",
				Modality:       ModalityRealtimeVoice,
				ExecutionMode:  ExecutionModeGoogle,
				ModelID:        "gemini-live-2.5-flash-preview",
				Source:         "Google",
				Description:    "Ultra-low-latency real-time voice conversation. Requires Google API key.",
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
