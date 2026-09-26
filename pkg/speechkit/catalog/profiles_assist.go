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
			ID:             "assist.ollama.gemma4-e4b",
			Mode:           speechkit.ModeAssist,
			Name:           "Gemma 4 E4B (Ollama)",
			ProviderKind:   speechkit.ProviderKindLocalProvider,
			ExecutionMode:  speechkit.ExecutionModeOllama,
			ModelID:        "gemma4:e4b",
			Source:         "Local Provider",
			Description:    "Externally managed Ollama provider for Assist Mode.",
			License:        "gemma",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.routed.qwen35-27b",
			Mode:           speechkit.ModeAssist,
			Name:           "Qwen 3.5 27B (Hugging Face)",
			ProviderKind:   speechkit.ProviderKindCloudProvider,
			ExecutionMode:  speechkit.ExecutionModeHFRouted,
			ModelID:        "Qwen/Qwen3.5-27B",
			Source:         "Hugging Face",
			Description:    "Strong open-weight Assist model over Hugging Face.",
			License:        "apache-2.0",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.openai.gpt-5.4",
			Mode:           speechkit.ModeAssist,
			Name:           "GPT-5.4 (OpenAI)",
			ProviderKind:   speechkit.ProviderKindDirectProvider,
			ExecutionMode:  speechkit.ExecutionModeOpenAI,
			ModelID:        "gpt-5.4-2026-03-05",
			Source:         "OpenAI",
			Description:    "Frontier hosted LLM for the Assist tier.",
			License:        "proprietary",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
			Recommended:    true,
		},
		{
			// GPT-5.6 ships on Foundry as three purpose-built variants
			// (verified 2026-09-05): Terra balanced, Luna fast and cheap with
			// a 1M context, Sol the reasoning flagship. Configs that still
			// name the previous id "assist.foundry.gpt-5.1" resolve here via
			// NormalizeProviderProfileID.
			ID:            "assist.foundry.gpt-5.6",
			Mode:          speechkit.ModeAssist,
			Name:          "GPT-5.6 (Microsoft Foundry)",
			ProviderKind:  speechkit.ProviderKindDirectProvider,
			ExecutionMode: speechkit.ExecutionModeFoundry,
			Provider:      "foundry",
			ModelID:       "gpt-5.6-terra",
			Source:        "Microsoft Foundry",
			Description:   "Azure-hosted frontier LLM for Assist and meeting summaries via the Microsoft Foundry OpenAI-compatible v1 surface. The model id is the default deployment name — override it with your own deployment name in the Foundry integration settings.",
			License:       "proprietary",
			Capabilities:  []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:   "genkit_llm",
			EvidenceURL:   "https://learn.microsoft.com/azure/ai-foundry/openai/reference",
			Variants: []speechkit.ModelVariant{
				{ID: "foundry.gpt-5.6-terra", Name: "GPT-5.6 Terra", ModelID: "gpt-5.6-terra", Recommended: true, Description: "Balanced everyday model; GPT-5.5-class quality at lower cost."},
				{ID: "foundry.gpt-5.6-sol", Name: "GPT-5.6 Sol", ModelID: "gpt-5.6-sol", Description: "Reasoning flagship for extended, agentic and code-heavy work."},
				{ID: "foundry.gpt-5.6-luna", Name: "GPT-5.6 Luna", ModelID: "gpt-5.6-luna", Description: "Fastest and cheapest of the family; 1M-token context."},
				// Microsoft-publisher deployment; served on /mai/v1 rather than
				// /openai/v1, text only, and quota starts at zero until requested.
				{ID: "foundry.mai-thinking-1", Name: "MAI-Thinking-1 (Preview)", ModelID: "MAI-Thinking-1", Description: "Microsoft's reasoning model (256k context, 64k output). Needs a MAI-Thinking-1 deployment plus a quota request in the Foundry project; text only."},
			},
			AllowInference: true,
			Recommended:    true,
		},
		{
			ID:             "assist.openrouter.gemini-2.5-flash",
			Mode:           speechkit.ModeAssist,
			Name:           "Gemini 2.5 Flash (OpenRouter)",
			ProviderKind:   speechkit.ProviderKindCloudProvider,
			ExecutionMode:  speechkit.ExecutionModeOpenRouter,
			ModelID:        "google/gemini-2.5-flash",
			Source:         "OpenRouter",
			Description:    "Gateway-routed Assist profile. OpenRouter is shown as a cloud router, not a direct provider.",
			License:        "proprietary",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
		},
		{
			ID:             "assist.groq.llama-3.3-70b",
			Mode:           speechkit.ModeAssist,
			Name:           "Llama 3.3 70B (Groq)",
			ProviderKind:   speechkit.ProviderKindDirectProvider,
			ExecutionMode:  speechkit.ExecutionModeGroq,
			ModelID:        "llama-3.3-70b-versatile",
			Source:         "Groq",
			Description:    "Groq-hosted Assist profile for fast one-shot responses.",
			License:        "llama",
			Capabilities:   []speechkit.Capability{speechkit.CapabilityLLM, speechkit.CapabilityToolCalling, speechkit.CapabilitySessionSummary},
			AdapterKind:    "genkit_llm",
			AllowInference: true,
		},
	}
}
