package catalog

import (
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// assistProviderProfiles returns the built-in Assist (text LLM) section of the
// default provider catalog.
func assistProviderProfiles() []speechkit.ProviderProfile {
	return []speechkit.ProviderProfile{
		{
			ID:            "assist.builtin.gemma4-e4b",
			Mode:          speechkit.ModeAssist,
			Name:          "Gemma 4 E4B (Local Built-in)",
			ProviderKind:  speechkit.ProviderKindLocalBuiltIn,
			ExecutionMode: speechkit.ExecutionModeLocal,
			ModelID:       DefaultLocalBuiltInLLMModel,
			Source:        "Local Built-in",
			Description:   "SpeechKit-managed llama.cpp runtime for Assist. Download options provide concrete GGUF model files.",
			License:       "gemma",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			Variants: []speechkit.ModelVariant{
				{ID: "llamacpp.gemma-4-e2b-it-q8-0", Name: "Gemma 4 E2B IT Q8_0", ModelID: "gemma-4-E2B-it-Q8_0.gguf", Description: "Default lightweight GGUF model for local Assist usage.", Recommended: true},
				{ID: "llamacpp.gemma-4-e4b-it-q4-k-m", Name: "Gemma 4 E4B IT Q4_K_M", ModelID: "gemma-4-E4B-it-Q4_K_M.gguf", Description: "Stronger optional GGUF model for devices with enough memory."},
			},
			AllowInference: true,
			Default:        true,
			Recommended:    true,
		},
		{
			ID:            "assist.ollama.gemma4-e4b",
			Mode:          speechkit.ModeAssist,
			Name:          "Gemma 4 E4B (Ollama)",
			ProviderKind:  speechkit.ProviderKindLocalProvider,
			ExecutionMode: speechkit.ExecutionModeOllama,
			ModelID:       "gemma4:e4b",
			Source:        "Local Provider",
			Description:   "Externally managed Ollama provider for Assist Mode.",
			License:       "gemma",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			Variants: []speechkit.ModelVariant{
				{ID: "ollama.gemma4-e4b-assist", Name: "Gemma 4 E4B", ModelID: "gemma4:e4b", Recommended: true},
				{ID: "ollama.gemma4-12b-assist", Name: "Gemma 4 12B", ModelID: "gemma4:12b", Description: "Larger unified Gemma 4 for hosts with enough memory."},
			},
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.routed.qwen38-27b",
			Mode:           speechkit.ModeAssist,
			Name:           "Qwen 3.8 27B (Hugging Face)",
			ProviderKind:   speechkit.ProviderKindCloudProvider,
			ExecutionMode:  speechkit.ExecutionModeHFRouted,
			ModelID:        modelHFQwen38,
			Source:         "Hugging Face",
			Description:    "Strong open-weight Assist model over Hugging Face.",
			License:        "apache-2.0",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:            "assist.openai.gpt-6",
			Mode:          speechkit.ModeAssist,
			Name:          "GPT-6 (OpenAI)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeOpenAI,
			ModelID:       modelOpenAIGPT61Sol,
			Source:        "OpenAI",
			Description:   "Frontier hosted LLM for the Assist tier. GPT-6.1 Sol is the everyday default; Luna is the efficient high-volume tier and Astra the flagship.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			EvidenceURL:   "https://openai.com/index/introducing-gpt-6-sol-and-luna/",
			Variants: []speechkit.ModelVariant{
				{ID: "openai.gpt-6.1-sol", Name: "GPT-6.1 Sol", ModelID: modelOpenAIGPT61Sol, Recommended: true, Description: "Newest Sol release (2026-09-29) with a 1M-token context."},
				{ID: "openai.gpt-6-sol", Name: "GPT-6 Sol", ModelID: modelOpenAIGPT6Sol},
				{ID: "openai.gpt-6-luna", Name: "GPT-6 Luna", ModelID: modelOpenAIGPT6Luna, Description: "Most efficient GPT-6 tier for focused, high-volume tasks."},
				{ID: "openai.gpt-6-astra", Name: "GPT-6 Astra", ModelID: modelOpenAIGPT6Astra, Description: "Flagship for the hardest reasoning and agentic work."},
			},
			AllowInference: true,
			Recommended:    true,
		},
		{
			// GPT-6 ships on Foundry as Sol, Luna and Astra plus the newer
			// GPT-6.1 Sol (verified 2026-09-30 against the Foundry model list;
			// Global Standard in most regions). Configs that still name the
			// previous ids "assist.foundry.gpt-5.6" or "assist.foundry.gpt-5.1"
			// resolve here via NormalizeProviderProfileID; their saved
			// deployment names are user-owned and kept as they are.
			ID:            "assist.foundry.gpt-6",
			Mode:          speechkit.ModeAssist,
			Name:          "GPT-6 (Microsoft Foundry)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeFoundry,
			Provider:      "foundry",
			ModelID:       modelOpenAIGPT61Sol,
			Source:        "Microsoft Foundry",
			Description:   "Azure-hosted frontier LLM for Assist and meeting summaries via the Microsoft Foundry OpenAI-compatible v1 surface. The model id is the default deployment name — override it with your own deployment name in the Foundry integration settings.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			EvidenceURL:   "https://learn.microsoft.com/azure/foundry/foundry-models/concepts/models-sold-directly-by-azure",
			Variants: []speechkit.ModelVariant{
				{ID: "foundry.gpt-6.1-sol", Name: "GPT-6.1 Sol", ModelID: modelOpenAIGPT61Sol, Recommended: true, Description: "Everyday frontier model (2026-09-29) with a 1.05M-token context."},
				{ID: "foundry.gpt-6-sol", Name: "GPT-6 Sol", ModelID: modelOpenAIGPT6Sol},
				{ID: "foundry.gpt-6-luna", Name: "GPT-6 Luna", ModelID: modelOpenAIGPT6Luna, Description: "Fastest and cheapest of the family for summaries and routing."},
				{ID: "foundry.gpt-6-astra", Name: "GPT-6 Astra", ModelID: modelOpenAIGPT6Astra, Description: "Flagship for extended, agentic and code-heavy work."},
				// Microsoft-publisher deployment; served on /mai/v1 rather than
				// /openai/v1, text only, and quota starts at zero until requested.
				{ID: "foundry.mai-thinking-1", Name: "MAI-Thinking-1 (Preview)", ModelID: "MAI-Thinking-1", Description: "Microsoft's reasoning model (256k context, 64k output). Needs a MAI-Thinking-1 deployment plus a quota request in the Foundry project; text only."},
			},
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.openrouter.gemini-3.5-flash",
			Mode:           speechkit.ModeAssist,
			Name:           "Gemini 3.5 Flash (OpenRouter)",
			ProviderKind:   speechkit.ProviderKindCloudProvider,
			ExecutionMode:  speechkit.ExecutionModeOpenRouter,
			ModelID:        modelOpenRouterGemini35Flash,
			Source:         "OpenRouter",
			Description:    "Gateway-routed Assist profile. OpenRouter is shown as a cloud router, not a direct provider.",
			License:        "proprietary",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
		},
		{
			ID:            "assist.groq.gpt-oss-120b",
			Mode:          speechkit.ModeAssist,
			Name:          "GPT-OSS 120B (Groq)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeGroq,
			ModelID:       modelGroqGPTOSS120B,
			Source:        "Groq",
			Description:   "Groq-hosted Assist profile for fast one-shot responses. Replaces the Llama 3.x models Groq retired for free and developer tiers on 2026-08-16.",
			License:       "apache-2.0",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			EvidenceURL:   "https://console.groq.com/docs/models",
			Variants: []speechkit.ModelVariant{
				{ID: "groq.gpt-oss-120b", Name: "GPT-OSS 120B", ModelID: modelGroqGPTOSS120B, Recommended: true},
				{ID: "groq.gpt-oss-20b", Name: "GPT-OSS 20B", ModelID: modelGroqGPTOSS20B, Description: "Smaller and cheaper; Groq's replacement for Llama 3.1 8B Instant."},
				{ID: "groq.qwen3.8-27b", Name: "Qwen3.8 27B (Preview)", ModelID: modelGroqQwen38, Description: "Groq preview model; Groq retires preview ids at short notice."},
			},
			AllowInference: true,
		},
	}
}
