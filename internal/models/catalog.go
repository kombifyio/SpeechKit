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
			// --- STT: Local ---
			{
				ID:             "stt.local.whispercpp",
				Name:           "Whisper.cpp (Bundled Local)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "whisper.cpp",
				Source:         "Bundled local runtime",
				Description:    "Offline Windows dictation with the bundled whisper-server runtime.",
				License:        "mit",
				AllowInference: false,
				Default:        true,
			},
			// --- STT: HF Routed ---
			{
				ID:             "stt.routed.whisper-large-v3",
				Name:           "Whisper Large v3 (HF Routed)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "openai/whisper-large-v3",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- STT: Cloud Providers ---
			{
				ID:             "stt.groq.whisper-large-v3-turbo",
				Name:           "Whisper Large v3 Turbo (Groq)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeGroq,
				ModelID:        "whisper-large-v3-turbo",
				Source:         "Groq",
				License:        "proprietary",
				AllowInference: true,
			},
			{
				ID:             "stt.groq.whisper-large-v3",
				Name:           "Whisper Large v3 (Groq)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeGroq,
				ModelID:        "whisper-large-v3",
				Source:         "Groq",
				License:        "proprietary",
				AllowInference: true,
			},
			{
				ID:             "stt.google.chirp3",
				Name:           "Chirp 3 (Google Cloud Speech)",
				Modality:       ModalitySTT,
				ExecutionMode:  ExecutionModeGoogle,
				ModelID:        "chirp_3",
				Source:         "Google",
				License:        "proprietary",
				AllowInference: true,
			},
			// --- TTS: Local ---
			{
				ID:             "tts.local.kokoro-82m",
				Name:           "Kokoro 82M",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "hexgrad/Kokoro-82M",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: false,
			},
			{
				ID:             "tts.local.qwen3-tts-1.7b",
				Name:           "Qwen3 TTS 1.7B",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "Qwen/Qwen3-TTS-12Hz-1.7B-VoiceDesign",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: false,
				Default:        true,
			},
			{
				ID:             "tts.local.qwen3-tts-0.6b",
				Name:           "Qwen3 TTS 0.6B",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeLocal,
				ModelID:        "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: false,
			},
			// --- TTS: HF Routed ---
			{
				ID:             "tts.routed.qwen3-tts-1.7b",
				Name:           "Qwen3 TTS 1.7B (HF Routed)",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3-TTS-12Hz-1.7B-VoiceDesign",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: true,
			},
			{
				ID:             "tts.routed.parler-mini-multilingual",
				Name:           "Parler TTS Mini Multilingual (HF Routed)",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "parler-tts/parler-tts-mini-multilingual-v1.1",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: true,
			},
			// --- TTS: Cloud Providers ---
			{
				ID:             "tts.openai.tts-1",
				Name:           "OpenAI TTS-1",
				Modality:       ModalityTTS,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "tts-1",
				Source:         "OpenAI",
				License:        "proprietary",
				AllowInference: true,
			},
			// --- Utility LLM: small/fast models for summarize, codewords, text transforms ---
			{
				ID:             "utility.routed.qwen35-9b",
				Name:           "Qwen3.5 9B (HF Routed)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3.5-9B",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: true,
			},
			{
				ID:             "utility.openai.gpt5.4-nano",
				Name:           "GPT-5.4 Nano",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "gpt-5.4-nano",
				Source:         "OpenAI",
				License:        "proprietary",
				AllowInference: true,
			},
			{
				ID:             "utility.openai.gpt5.4-mini",
				Name:           "GPT-5.4 Mini",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "gpt-5.4-mini",
				Source:         "OpenAI",
				License:        "proprietary",
				AllowInference: true,
				Default:        true,
			},
			{
				ID:             "utility.groq.llama-3.1-8b",
				Name:           "LLaMA 3.1 8B Instant (Groq)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeGroq,
				ModelID:        "llama-3.1-8b-instant",
				Source:         "Groq (Meta)",
				License:        "llama3.1",
				AllowInference: true,
			},
			{
				ID:             "utility.google.gemini-31-flash-lite",
				Name:           "Gemini 3.1 Flash Lite",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeGoogle,
				ModelID:        "gemini-3.1-flash-lite-preview",
				Source:         "Google",
				License:        "proprietary",
				AllowInference: true,
			},
			{
				ID:             "utility.ollama.gemma4-e2b",
				Name:           "Gemma 4 E2B (Edge)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e2b",
				Source:         "Local (Ollama)",
				Description:    "Smallest Gemma 4 local profile for low-memory Windows devices and phone-class edge targets.",
				License:        "varies",
				AllowInference: true,
			},
			{
				ID:             "utility.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Local)",
				Modality:       ModalityUtility,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e4b",
				Source:         "Local (Ollama)",
				Description:    "Laptop-friendly Gemma 4 profile for summaries, transforms, and quick actions.",
				License:        "varies",
				AllowInference: true,
			},
			// --- Agent LLM: strong models for reasoning, autonomous actions ---
			{
				ID:             "agent.openai.gpt5.4",
				Name:           "GPT-5.4",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeOpenAI,
				ModelID:        "gpt-5.4",
				Source:         "OpenAI",
				License:        "proprietary",
				AllowInference: true,
				Default:        true,
			},
			{
				ID:             "agent.groq.llama-3.3-70b",
				Name:           "LLaMA 3.3 70B (Groq)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeGroq,
				ModelID:        "llama-3.3-70b-versatile",
				Source:         "Groq (Meta)",
				License:        "llama3.3",
				AllowInference: true,
			},
			{
				ID:             "agent.google.gemini-31-pro",
				Name:           "Gemini 3.1 Pro",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeGoogle,
				ModelID:        "gemini-3.1-pro-preview",
				Source:         "Google",
				License:        "proprietary",
				AllowInference: true,
			},
			{
				ID:             "agent.routed.qwen35-32b",
				Name:           "Qwen3.5 32B (HF Routed)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "Qwen/Qwen3.5-32B",
				Source:         "huggingface",
				License:        "apache-2.0",
				AllowInference: true,
			},
			{
				ID:             "agent.ollama.gemma4-e2b",
				Name:           "Gemma 4 E2B (Edge)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e2b",
				Source:         "Local (Ollama)",
				Description:    "Minimal local agent profile for thin devices, low-memory systems, and smartphone-adjacent edge footprints.",
				License:        "varies",
				AllowInference: true,
			},
			{
				ID:             "agent.ollama.gemma4-e4b",
				Name:           "Gemma 4 E4B (Local)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:e4b",
				Source:         "Local (Ollama)",
				Description:    "Best default local agent profile when the app runs on typical Windows hardware.",
				License:        "varies",
				AllowInference: true,
			},
			{
				ID:             "agent.ollama.gemma4-26b",
				Name:           "Gemma 4 26B (Local Workstation)",
				Modality:       ModalityAgent,
				ExecutionMode:  ExecutionModeOllama,
				ModelID:        "gemma4:26b",
				Source:         "Local (Ollama)",
				Description:    "Higher-quality local agent profile for workstation-class GPUs or high-memory systems.",
				License:        "varies",
				AllowInference: true,
			},
			// --- Embedding: Google (Default) ---
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
			// --- Embedding: HF Routed (Fallback) ---
			{
				ID:             "embedding.routed.bge-m3",
				Name:           "BGE M3 (HF Routed)",
				Modality:       ModalityEmbedding,
				ExecutionMode:  ExecutionModeHFRouted,
				ModelID:        "BAAI/bge-m3",
				Source:         "huggingface",
				License:        "mit",
				AllowInference: true,
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
