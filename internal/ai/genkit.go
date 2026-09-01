// Package ai wires the Genkit runtime and the SpeechKit model catalog into
// a single LLM/embedding/reranker surface used by Assist and the Voice
// Agent pipeline-fallback path.
//
// It owns provider keys, model selection, OpenAI-compatible model
// registration, and the per-Modality plumbing (Utility, Assist, Agent).
// Routing decisions live in [github.com/kombifyio/SpeechKit/internal/router]
// and [github.com/kombifyio/SpeechKit/internal/tts]; this package
// is the model substrate they call into.
package ai

import (
	"context"
	"log/slog"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"github.com/firebase/genkit/go/plugins/ollama"

	"github.com/kombifyio/SpeechKit/internal/ai/generation"
)

// Config holds all provider API keys and model selections for Genkit initialization.
type Config struct {
	GoogleAPIKey     string
	OpenAIAPIKey     string
	GroqAPIKey       string
	HuggingFaceToken string
	OllamaBaseURL    string
	LocalLLMBaseURL  string

	GoogleUtilityModel          string
	GoogleAssistModel           string
	GoogleAgentModel            string
	OpenAIUtilityModel          string
	OpenAIAssistModel           string
	OpenAIAgentModel            string
	GroqUtilityModel            string
	GroqAssistModel             string
	GroqAgentModel              string
	HFUtilityModel              string
	HFAssistModel               string
	HFAgentModel                string
	LocalLLMUtilityModel        string
	LocalLLMAssistModel         string
	LocalLLMAgentModel          string
	OllamaUtilityModel          string
	OllamaAssistModel           string
	OllamaAgentModel            string
	OpenRouterAPIKey            string
	OpenRouterUtilityModel      string
	OpenRouterAssistModel       string
	OpenRouterAgentModel        string
	AssemblyAIAPIKey            string
	AssemblyAILLMGatewayBaseURL string
	AssemblyAIUtilityModel      string
	AssemblyAIAssistModel       string
	AssemblyAIAgentModel        string
	CloudflareAPIKey            string
	CloudflareAccountID         string
	CloudflareGatewayID         string
	CloudflareUtilityModel      string
	CloudflareAssistModel       string
	CloudflareAgentModel        string
	FoundryAPIKey               string
	FoundryBaseURL              string
	FoundryUtilityModel         string
	FoundryAssistModel          string
	FoundryAgentModel           string
	OrderedAssistModels         []OrderedModelSelection
	OrderedAgentModels          []OrderedModelSelection
	UseOrderedAssistModels      bool
	UseOrderedAgentModels       bool
}

type OrderedModelSelection struct {
	Provider string
	Model    string
}

type modelSpec struct {
	provider string
	model    string
	enabled  bool
}

// ModelInfo describes a registered model for the UI.
type ModelInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Tier     string `json:"tier"` // e.g. "utility", "assist", "agent", "utility+assist", or "all"
}

// Runtime holds the Genkit instance and categorized model references.
type Runtime struct {
	G             *genkit.Genkit
	utilityModels []ai.Model
	assistModels  []ai.Model
	agentModels   []ai.Model
	allModels     map[string]ai.Model
	modelInfos    []ModelInfo
}

// UtilityModels returns the models configured for utility tasks (summarize, codewords).
func (r *Runtime) UtilityModels() []ai.Model { return r.utilityModels }

// AssistModels returns the models configured for direct Assist replies.
func (r *Runtime) AssistModels() []ai.Model { return r.assistModels }

// AgentModels returns the models configured for agent tasks (reasoning, autonomous).
func (r *Runtime) AgentModels() []ai.Model { return r.agentModels }

// AllModels returns all registered models keyed by their full ID.
func (r *Runtime) AllModels() map[string]ai.Model { return r.allModels }

// ModelInfos returns metadata about all registered models for the UI.
func (r *Runtime) ModelInfos() []ModelInfo { return r.modelInfos }

// Generator exposes the configured model pool through SpeechKit's
// provider-neutral generation boundary.
func (r *Runtime) Generator() generation.Generator {
	if r == nil {
		return generation.NewGenkit(nil, nil)
	}
	bindings := make([]generation.GenkitModel, 0, len(r.modelInfos))
	for _, info := range r.modelInfos {
		model := r.allModels[info.ID]
		if model == nil {
			continue
		}
		bindings = append(bindings, generation.GenkitModel{
			Model: model,
			Info: generation.Model{
				ID:                       info.ID,
				Provider:                 info.Provider,
				Name:                     info.Name,
				Purposes:                 generationPurposes(info.Tier),
				ContextWindowTokens:      generation.ConservativeContextWindow(info.Provider, info.Name),
				SupportsStructuredOutput: true,
				Cloud:                    info.Provider != "local" && info.Provider != "ollama",
			},
		})
	}
	return generation.NewGenkit(r.G, bindings)
}

func generationPurposes(tier string) []generation.Purpose {
	purposes := make([]generation.Purpose, 0, 5)
	if tier == "all" || strings.Contains(tier, "utility") {
		purposes = append(purposes,
			generation.PurposeUtility,
			generation.PurposeMeetingExtraction,
			generation.PurposeMeetingSynthesis,
		)
	}
	if tier == "all" || strings.Contains(tier, "assist") {
		purposes = append(purposes, generation.PurposeAssist)
	}
	if tier == "all" || strings.Contains(tier, "agent") {
		purposes = append(purposes, generation.PurposeVoiceAgentThink)
	}
	return purposes
}

// Init creates a Genkit instance with all configured providers and returns a Runtime.
func Init(ctx context.Context, cfg Config) (*Runtime, error) {
	g := genkit.Init(ctx, genkit.WithPlugins(configuredPlugins(cfg)...))
	registerConfiguredProviderModels(g, cfg)

	rt := &Runtime{
		G:         g,
		allModels: make(map[string]ai.Model),
	}

	for _, spec := range tierModelSpecs(cfg, "utility") {
		if !spec.enabled {
			continue
		}
		m := genkit.LookupModel(g, spec.provider+"/"+spec.model)
		if m == nil {
			slog.Warn("utility model not found", "provider", spec.provider, "model", spec.model)
			continue
		}
		rt.utilityModels = append(rt.utilityModels, m)
		registerModelInfo(rt, spec.provider, spec.model, m, "utility")
		slog.Info("utility model registered", "provider", spec.provider, "model", spec.model)
	}

	resolveOrderedOrLegacyModels(rt, g, "assist", cfg.UseOrderedAssistModels, cfg.OrderedAssistModels, tierModelSpecs(cfg, "assist"), func(model ai.Model) {
		rt.assistModels = append(rt.assistModels, model)
	})

	resolveOrderedOrLegacyModels(rt, g, "agent", cfg.UseOrderedAgentModels, cfg.OrderedAgentModels, tierModelSpecs(cfg, "agent"), func(model ai.Model) {
		rt.agentModels = append(rt.agentModels, model)
	})

	return rt, nil
}

func configuredPlugins(cfg Config) []api.Plugin {
	var plugins []api.Plugin
	if cfg.GoogleAPIKey != "" {
		plugins = append(plugins, &googlegenai.GoogleAI{APIKey: cfg.GoogleAPIKey})
	}
	if cfg.OllamaBaseURL != "" {
		plugins = append(plugins, &ollama.Ollama{ServerAddress: cfg.OllamaBaseURL})
	}
	return plugins
}

func registerConfiguredProviderModels(g *genkit.Genkit, cfg Config) {
	if cfg.OpenAIAPIKey != "" {
		registerOpenAIModels(g, cfg.OpenAIAPIKey)
	}
	if cfg.GroqAPIKey != "" {
		registerGroqModels(g, cfg.GroqAPIKey)
	}
	if cfg.HuggingFaceToken != "" {
		registerHFModels(g, cfg.HuggingFaceToken)
	}
	if cfg.OpenRouterAPIKey != "" {
		registerOpenRouterModels(g, cfg.OpenRouterAPIKey)
	}
	if cfg.AssemblyAIAPIKey != "" {
		registerAssemblyAILLMModels(g, cfg.AssemblyAIAPIKey, cfg.AssemblyAILLMGatewayBaseURL, []string{
			cfg.AssemblyAIUtilityModel,
			cfg.AssemblyAIAssistModel,
			cfg.AssemblyAIAgentModel,
		})
	}
	if cfg.CloudflareAPIKey != "" && cfg.CloudflareAccountID != "" {
		registerCloudflareAIGatewayModels(g, cfg.CloudflareAPIKey, cfg.CloudflareAccountID, cfg.CloudflareGatewayID, []string{
			cfg.CloudflareUtilityModel,
			cfg.CloudflareAssistModel,
			cfg.CloudflareAgentModel,
		})
	}
	if cfg.FoundryAPIKey != "" && cfg.FoundryBaseURL != "" {
		registerFoundryModels(g, cfg.FoundryAPIKey, cfg.FoundryBaseURL, []string{
			cfg.FoundryUtilityModel,
			cfg.FoundryAssistModel,
			cfg.FoundryAgentModel,
		})
	}
	if cfg.LocalLLMBaseURL != "" {
		registerLocalLLMModels(g, cfg.LocalLLMBaseURL, localLLMModelNames(cfg))
	}
}

func localLLMModelNames(cfg Config) []string {
	names := []string{
		cfg.LocalLLMUtilityModel,
		cfg.LocalLLMAssistModel,
		cfg.LocalLLMAgentModel,
	}
	for _, spec := range append(cfg.OrderedAssistModels, cfg.OrderedAgentModels...) {
		if strings.TrimSpace(spec.Provider) == "local" {
			names = append(names, spec.Model)
		}
	}
	return names
}

func tierModelSpecs(cfg Config, tier string) []modelSpec {
	switch tier {
	case "utility":
		return []modelSpec{
			{"googleai", cfg.GoogleUtilityModel, cfg.GoogleAPIKey != "" && cfg.GoogleUtilityModel != ""},
			{"openai", cfg.OpenAIUtilityModel, cfg.OpenAIAPIKey != "" && cfg.OpenAIUtilityModel != ""},
			{"groq", cfg.GroqUtilityModel, cfg.GroqAPIKey != "" && cfg.GroqUtilityModel != ""},
			{"huggingface", cfg.HFUtilityModel, cfg.HuggingFaceToken != "" && cfg.HFUtilityModel != ""},
			{"local", cfg.LocalLLMUtilityModel, cfg.LocalLLMBaseURL != "" && cfg.LocalLLMUtilityModel != ""},
			{"ollama", cfg.OllamaUtilityModel, cfg.OllamaBaseURL != "" && cfg.OllamaUtilityModel != ""},
			{"openrouter", cfg.OpenRouterUtilityModel, cfg.OpenRouterAPIKey != "" && cfg.OpenRouterUtilityModel != ""},
			{"assemblyai", cfg.AssemblyAIUtilityModel, cfg.AssemblyAIAPIKey != "" && cfg.AssemblyAIUtilityModel != ""},
			{"cloudflare", cfg.CloudflareUtilityModel, cloudflareModelEnabled(cfg) && cfg.CloudflareUtilityModel != ""},
			{"foundry", cfg.FoundryUtilityModel, foundryModelEnabled(cfg) && cfg.FoundryUtilityModel != ""},
		}
	case "assist":
		return []modelSpec{
			{"googleai", cfg.GoogleAssistModel, cfg.GoogleAPIKey != "" && cfg.GoogleAssistModel != ""},
			{"openai", cfg.OpenAIAssistModel, cfg.OpenAIAPIKey != "" && cfg.OpenAIAssistModel != ""},
			{"groq", cfg.GroqAssistModel, cfg.GroqAPIKey != "" && cfg.GroqAssistModel != ""},
			{"huggingface", cfg.HFAssistModel, cfg.HuggingFaceToken != "" && cfg.HFAssistModel != ""},
			{"local", cfg.LocalLLMAssistModel, cfg.LocalLLMBaseURL != "" && cfg.LocalLLMAssistModel != ""},
			{"ollama", cfg.OllamaAssistModel, cfg.OllamaBaseURL != "" && cfg.OllamaAssistModel != ""},
			{"openrouter", cfg.OpenRouterAssistModel, cfg.OpenRouterAPIKey != "" && cfg.OpenRouterAssistModel != ""},
			{"assemblyai", cfg.AssemblyAIAssistModel, cfg.AssemblyAIAPIKey != "" && cfg.AssemblyAIAssistModel != ""},
			{"cloudflare", cfg.CloudflareAssistModel, cloudflareModelEnabled(cfg) && cfg.CloudflareAssistModel != ""},
			{"foundry", cfg.FoundryAssistModel, foundryModelEnabled(cfg) && cfg.FoundryAssistModel != ""},
		}
	case "agent":
		return []modelSpec{
			{"googleai", cfg.GoogleAgentModel, cfg.GoogleAPIKey != "" && cfg.GoogleAgentModel != ""},
			{"openai", cfg.OpenAIAgentModel, cfg.OpenAIAPIKey != "" && cfg.OpenAIAgentModel != ""},
			{"groq", cfg.GroqAgentModel, cfg.GroqAPIKey != "" && cfg.GroqAgentModel != ""},
			{"huggingface", cfg.HFAgentModel, cfg.HuggingFaceToken != "" && cfg.HFAgentModel != ""},
			{"local", cfg.LocalLLMAgentModel, cfg.LocalLLMBaseURL != "" && cfg.LocalLLMAgentModel != ""},
			{"ollama", cfg.OllamaAgentModel, cfg.OllamaBaseURL != "" && cfg.OllamaAgentModel != ""},
			{"openrouter", cfg.OpenRouterAgentModel, cfg.OpenRouterAPIKey != "" && cfg.OpenRouterAgentModel != ""},
			{"assemblyai", cfg.AssemblyAIAgentModel, cfg.AssemblyAIAPIKey != "" && cfg.AssemblyAIAgentModel != ""},
			{"cloudflare", cfg.CloudflareAgentModel, cloudflareModelEnabled(cfg) && cfg.CloudflareAgentModel != ""},
			{"foundry", cfg.FoundryAgentModel, foundryModelEnabled(cfg) && cfg.FoundryAgentModel != ""},
		}
	default:
		return nil
	}
}

func cloudflareModelEnabled(cfg Config) bool {
	return cfg.CloudflareAPIKey != "" && cfg.CloudflareAccountID != ""
}

func foundryModelEnabled(cfg Config) bool {
	return cfg.FoundryAPIKey != "" && cfg.FoundryBaseURL != ""
}

func resolveOrderedOrLegacyModels(
	rt *Runtime,
	g *genkit.Genkit,
	tier string,
	useOrdered bool,
	ordered []OrderedModelSelection,
	legacy []modelSpec,
	appendModel func(ai.Model),
) {
	if useOrdered {
		for _, spec := range ordered {
			registerResolvedModel(rt, g, spec.Provider, spec.Model, tier, appendModel)
		}
		return
	}

	for _, spec := range legacy {
		if !spec.enabled {
			continue
		}
		registerResolvedModel(rt, g, spec.provider, spec.model, tier, appendModel)
	}
}

func registerResolvedModel(
	rt *Runtime,
	g *genkit.Genkit,
	provider string,
	model string,
	tier string,
	appendModel func(ai.Model),
) {
	m := genkit.LookupModel(g, provider+"/"+model)
	if m == nil {
		slog.Warn(tier+" model not found", "provider", provider, "model", model)
		return
	}
	appendModel(m)
	registerModelInfo(rt, provider, model, m, tier)
	slog.Info(tier+" model registered", "provider", provider, "model", model)
}

func registerModelInfo(rt *Runtime, provider, model string, m ai.Model, tier string) {
	id := provider + "/" + model
	if _, ok := rt.allModels[id]; ok {
		for i := range rt.modelInfos {
			if rt.modelInfos[i].ID == id {
				rt.modelInfos[i].Tier = mergeModelTier(rt.modelInfos[i].Tier, tier)
				return
			}
		}
		return
	}

	rt.allModels[id] = m
	rt.modelInfos = append(rt.modelInfos, ModelInfo{
		ID:       id,
		Provider: provider,
		Name:     model,
		Tier:     tier,
	})
}

func mergeModelTier(existing, added string) string {
	roles := map[string]bool{}
	for _, role := range strings.Split(existing, "+") {
		role = strings.TrimSpace(role)
		if role == "" || role == "all" {
			continue
		}
		roles[role] = true
	}
	if added != "" && added != "all" {
		roles[added] = true
	}

	switch {
	case roles["utility"] && roles["assist"] && roles["agent"]:
		return "all"
	case roles["utility"] && roles["assist"]:
		return "utility+assist"
	case roles["utility"] && roles["agent"]:
		return "utility+agent"
	case roles["assist"] && roles["agent"]:
		return "assist+agent"
	case roles["utility"]:
		return "utility"
	case roles["assist"]:
		return "assist"
	case roles["agent"]:
		return "agent"
	default:
		return added
	}
}
