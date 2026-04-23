package models

import "github.com/kombifyio/SpeechKit/pkg/speechkit"

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

// DefaultCatalog adapts the public framework catalog into the desktop host's
// internal runtime model and appends host-only support profiles. The three
// strict user modes are owned by pkg/speechkit.
func DefaultCatalog() Catalog {
	profiles := profilesFromFrameworkCatalog(speechkit.DefaultProviderProfiles())
	profiles = append(profiles, supportProfiles()...)
	return Catalog{Profiles: profiles}
}

func profilesFromFrameworkCatalog(frameworkProfiles []speechkit.ProviderProfile) []Profile {
	profiles := make([]Profile, 0, len(frameworkProfiles))
	for _, profile := range frameworkProfiles {
		profiles = append(profiles, profileFromFramework(profile))
	}
	return profiles
}

func profileFromFramework(profile speechkit.ProviderProfile) Profile {
	return Profile{
		ID:             profile.ID,
		Name:           profile.Name,
		Modality:       modalityFromFrameworkMode(profile.Mode),
		ProviderKind:   ProviderKind(profile.ProviderKind),
		ExecutionMode:  ExecutionMode(profile.ExecutionMode),
		ModelID:        profile.ModelID,
		Source:         profile.Source,
		Description:    profile.Description,
		License:        profile.License,
		Capabilities:   capabilitiesFromFramework(profile.Capabilities),
		AdapterKind:    profile.AdapterKind,
		Variants:       variantsFromFramework(profile.Variants),
		AllowInference: profile.AllowInference,
		Default:        profile.Default,
		Recommended:    profile.Recommended,
		Experimental:   profile.Experimental,
	}
}

func modalityFromFrameworkMode(mode speechkit.Mode) Modality {
	switch speechkit.NormalizeMode(mode) {
	case speechkit.ModeDictation:
		return ModalitySTT
	case speechkit.ModeAssist:
		return ModalityAssist
	case speechkit.ModeVoiceAgent:
		return ModalityRealtimeVoice
	default:
		return ""
	}
}

func capabilitiesFromFramework(input []speechkit.Capability) []Capability {
	if len(input) == 0 {
		return nil
	}
	out := make([]Capability, 0, len(input))
	for _, capability := range input {
		out = append(out, Capability(capability))
	}
	return out
}

func variantsFromFramework(input []speechkit.ModelVariant) []ModelVariant {
	if len(input) == 0 {
		return nil
	}
	out := make([]ModelVariant, 0, len(input))
	for _, variant := range input {
		out = append(out, ModelVariant{
			ID:          variant.ID,
			Name:        variant.Name,
			ModelID:     variant.ModelID,
			Description: variant.Description,
			Recommended: variant.Recommended,
		})
	}
	return out
}

func supportProfiles() []Profile {
	return []Profile{
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
		{
			ID:             "utility.builtin.gemma4-e4b",
			Name:           "Gemma 4 E4B (Local Built-in)",
			Modality:       ModalityUtility,
			ProviderKind:   ProviderKindLocalBuiltIn,
			ExecutionMode:  ExecutionModeLocal,
			ModelID:        "gemma4:e4b",
			Source:         "Local Built-in",
			Description:    "SpeechKit-managed llama.cpp runtime for summaries, routing, and command follow-ups.",
			License:        "gemma",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Default:        true,
			Recommended:    true,
		},
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
