// Package models is the desktop host's view of the SpeechKit model catalog.
//
// The catalog itself is owned by pkg/speechkit: profile shape, provider IDs,
// modality, execution mode, capabilities, and readiness metadata all live
// there. This package is a thin alias layer over that source of truth plus
// the host-only support entries (utility, embedding and TTS models a user
// never selects as a mode) that the desktop app appends.
package models

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

// Catalog vocabulary. These are aliases, not copies: a value produced by the
// framework is the same value here, so nothing has to be converted at the
// boundary.
type (
	Modality      = speechkit.Modality
	ExecutionMode = speechkit.ExecutionMode
	ProviderKind  = speechkit.ProviderKind
	Capability    = speechkit.Capability
	ModelVariant  = speechkit.ModelVariant
	Profile       = speechkit.ProviderProfile
)

const (
	ModalitySTT           = speechkit.ModalitySTT
	ModalityTTS           = speechkit.ModalityTTS
	ModalityRealtimeVoice = speechkit.ModalityRealtimeVoice
	ModalityAssist        = speechkit.ModalityAssist
	ModalityUtility       = speechkit.ModalityUtility
	ModalityEmbedding     = speechkit.ModalityEmbedding
	ModalityReranker      = speechkit.ModalityReranker
)

const (
	ExecutionModeLocal          = speechkit.ExecutionModeLocal
	ExecutionModeSelfHostedHTTP = speechkit.ExecutionModeSelfHostedHTTP
	ExecutionModeHFRouted       = speechkit.ExecutionModeHFRouted
	ExecutionModeHFInference    = speechkit.ExecutionModeHFRouted // Legacy alias.
	ExecutionModeOpenAI         = speechkit.ExecutionModeOpenAI
	ExecutionModeGroq           = speechkit.ExecutionModeGroq
	ExecutionModeGoogle         = speechkit.ExecutionModeGoogle
	ExecutionModeDeepgram       = speechkit.ExecutionModeDeepgram
	ExecutionModeAssemblyAI     = speechkit.ExecutionModeAssemblyAI
	ExecutionModeOllama         = speechkit.ExecutionModeOllama
	ExecutionModeOpenRouter     = speechkit.ExecutionModeOpenRouter
	ExecutionModeFoundry        = speechkit.ExecutionModeFoundry
)

const (
	ProviderKindLocalBuiltIn   = speechkit.ProviderKindLocalBuiltIn
	ProviderKindLocalProvider  = speechkit.ProviderKindLocalProvider
	ProviderKindCloudProvider  = speechkit.ProviderKindCloudProvider
	ProviderKindDirectProvider = speechkit.ProviderKindDirectProvider
)

const (
	CapabilityTranscription         = speechkit.CapabilityTranscription
	CapabilitySTT                   = speechkit.CapabilitySTT
	CapabilityAudioInput            = speechkit.CapabilityAudioInput
	CapabilityLLM                   = speechkit.CapabilityLLM
	CapabilityTTS                   = speechkit.CapabilityTTS
	CapabilityRealtimeAudio         = speechkit.CapabilityRealtimeAudio
	CapabilityPipelineFallback      = speechkit.CapabilityPipelineFallback
	CapabilityToolCalling           = speechkit.CapabilityToolCalling
	CapabilityDictionaryPrompt      = speechkit.CapabilityDictionaryPrompt
	CapabilityDictionaryNativeHints = speechkit.CapabilityDictionaryNativeHints
	CapabilityWordsPrompt           = speechkit.CapabilityWordsPrompt
	CapabilityWordsNativeHints      = speechkit.CapabilityWordsNativeHints
	CapabilityPostSTTReplacements   = speechkit.CapabilityPostSTTReplacements
	CapabilitySessionSummary        = speechkit.CapabilitySessionSummary
	CapabilityTranscript            = speechkit.CapabilityTranscript
	CapabilityInterruptions         = speechkit.CapabilityInterruptions
	CapabilitySessionResume         = speechkit.CapabilitySessionResume
	CapabilityNativeContextPrompt   = speechkit.CapabilityNativeContextPrompt
	CapabilityNativeKeyterms        = speechkit.CapabilityNativeKeyterms
	CapabilityNativeDictationStream = speechkit.CapabilityNativeDictationStream
	CapabilityLanguageHints         = speechkit.CapabilityLanguageHints
	CapabilitySpeakerStreaming      = speechkit.CapabilitySpeakerStreaming
	CapabilityPrivacyRedaction      = speechkit.CapabilityPrivacyRedaction
	CapabilityVoiceFocus            = speechkit.CapabilityVoiceFocus
	CapabilityMedicalDomain         = speechkit.CapabilityMedicalDomain
	CapabilityReasoningEffort       = speechkit.CapabilityReasoningEffort
	CapabilityTranslation           = speechkit.CapabilityTranslation
	CapabilityTranscriptionOnly     = speechkit.CapabilityTranscriptionOnly
	CapabilitySpeakerDiarization    = speechkit.CapabilitySpeakerDiarization
	CapabilitySpeakerIdentification = speechkit.CapabilitySpeakerIdentification
	CapabilitySpeakerAttribution    = speechkit.CapabilitySpeakerAttribution
	CapabilitySpeakerEnrollment     = speechkit.CapabilitySpeakerEnrollment
)

type Catalog struct {
	Profiles []Profile
}

// DefaultCatalog is the framework catalog plus the host-only support entries,
// with framework defaults applied to every profile.
func DefaultCatalog() Catalog {
	profiles := append(catalog.DefaultProviderProfiles(), supportProfiles()...)
	for i := range profiles {
		profiles[i] = catalog.ProviderProfileWithDefaults(profiles[i])
	}
	return Catalog{Profiles: profiles}
}

func supportProfiles() []Profile {
	return []Profile{
		{
			ID:             "tts.routed.qwen3-tts-1.7b",
			Name:           "Qwen3 TTS 1.7B (HuggingFace)",
			Modality:       ModalityTTS,
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeHFRouted,
			ModelID:        "Qwen/Qwen3-TTS-12Hz-1.7B-Base",
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
			ModelID:        catalog.DefaultLocalBuiltInLLMModel,
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
			ID:             "utility.openrouter.llama-3.1-8b",
			Name:           "Llama 3.1 8B (OpenRouter)",
			Modality:       ModalityUtility,
			ProviderKind:   ProviderKindCloudProvider,
			ExecutionMode:  ExecutionModeOpenRouter,
			ModelID:        "meta-llama/llama-3.1-8b-instruct",
			Source:         "OpenRouter",
			Description:    "Gateway-routed utility model for summaries, routing, and short follow-ups.",
			License:        "llama",
			Capabilities:   []Capability{CapabilityLLM, CapabilityToolCalling, CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
		},
		{
			ID:             "utility.foundry.gpt-5-mini",
			Name:           "GPT-5 mini (Microsoft Foundry)",
			Modality:       ModalityUtility,
			ProviderKind:   ProviderKindDirectProvider,
			ExecutionMode:  ExecutionModeFoundry,
			ModelID:        "gpt-5-mini",
			Source:         "Microsoft Foundry",
			Description:    "Fast Azure-hosted utility model over the Foundry OpenAI-compatible surface. Model id doubles as the default deployment name.",
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
