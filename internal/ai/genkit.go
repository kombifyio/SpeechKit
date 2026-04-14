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
)

// Config holds all provider API keys and model selections for Genkit initialization.
type Config struct {
	GoogleAPIKey     string
	OpenAIAPIKey     string
	GroqAPIKey       string
	HuggingFaceToken string
	OllamaBaseURL    string

	GoogleUtilityModel     string
	GoogleAssistModel      string
	GoogleAgentModel       string
	OpenAIUtilityModel     string
	OpenAIAssistModel      string
	OpenAIAgentModel       string
	GroqUtilityModel       string
	GroqAssistModel        string
	GroqAgentModel         string
	HFUtilityModel         string
	HFAssistModel          string
	HFAgentModel           string
	OllamaUtilityModel     string
	OllamaAssistModel      string
	OllamaAgentModel       string
	OpenRouterAPIKey       string
	OpenRouterUtilityModel string
	OpenRouterAssistModel  string
	OpenRouterAgentModel   string
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

// Init creates a Genkit instance with all configured providers and returns a Runtime.
func Init(ctx context.Context, cfg Config) (*Runtime, error) {
	var plugins []api.Plugin

	if cfg.GoogleAPIKey != "" {
		plugins = append(plugins, &googlegenai.GoogleAI{APIKey: cfg.GoogleAPIKey})
	}
	if cfg.OllamaBaseURL != "" {
		plugins = append(plugins, &ollama.Ollama{ServerAddress: cfg.OllamaBaseURL})
	}

	g := genkit.Init(ctx, genkit.WithPlugins(plugins...))

	// Register custom models for OpenAI-compatible providers.
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

	rt := &Runtime{
		G:         g,
		allModels: make(map[string]ai.Model),
	}

	// Resolve utility models from config.
	utilitySpecs := []struct {
		provider string
		model    string
		enabled  bool
	}{
		{"googleai", cfg.GoogleUtilityModel, cfg.GoogleAPIKey != "" && cfg.GoogleUtilityModel != ""},
		{"openai", cfg.OpenAIUtilityModel, cfg.OpenAIAPIKey != "" && cfg.OpenAIUtilityModel != ""},
		{"groq", cfg.GroqUtilityModel, cfg.GroqAPIKey != "" && cfg.GroqUtilityModel != ""},
		{"huggingface", cfg.HFUtilityModel, cfg.HuggingFaceToken != "" && cfg.HFUtilityModel != ""},
		{"ollama", cfg.OllamaUtilityModel, cfg.OllamaBaseURL != "" && cfg.OllamaUtilityModel != ""},
		{"openrouter", cfg.OpenRouterUtilityModel, cfg.OpenRouterAPIKey != "" && cfg.OpenRouterUtilityModel != ""},
	}

	for _, spec := range utilitySpecs {
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

	// Resolve assist models from config.
	assistSpecs := []struct {
		provider string
		model    string
		enabled  bool
	}{
		{"googleai", cfg.GoogleAssistModel, cfg.GoogleAPIKey != "" && cfg.GoogleAssistModel != ""},
		{"openai", cfg.OpenAIAssistModel, cfg.OpenAIAPIKey != "" && cfg.OpenAIAssistModel != ""},
		{"groq", cfg.GroqAssistModel, cfg.GroqAPIKey != "" && cfg.GroqAssistModel != ""},
		{"huggingface", cfg.HFAssistModel, cfg.HuggingFaceToken != "" && cfg.HFAssistModel != ""},
		{"ollama", cfg.OllamaAssistModel, cfg.OllamaBaseURL != "" && cfg.OllamaAssistModel != ""},
		{"openrouter", cfg.OpenRouterAssistModel, cfg.OpenRouterAPIKey != "" && cfg.OpenRouterAssistModel != ""},
	}

	for _, spec := range assistSpecs {
		if !spec.enabled {
			continue
		}
		m := genkit.LookupModel(g, spec.provider+"/"+spec.model)
		if m == nil {
			slog.Warn("assist model not found", "provider", spec.provider, "model", spec.model)
			continue
		}
		rt.assistModels = append(rt.assistModels, m)
		registerModelInfo(rt, spec.provider, spec.model, m, "assist")
		slog.Info("assist model registered", "provider", spec.provider, "model", spec.model)
	}

	// Resolve agent models from config.
	agentSpecs := []struct {
		provider string
		model    string
		enabled  bool
	}{
		{"googleai", cfg.GoogleAgentModel, cfg.GoogleAPIKey != "" && cfg.GoogleAgentModel != ""},
		{"openai", cfg.OpenAIAgentModel, cfg.OpenAIAPIKey != "" && cfg.OpenAIAgentModel != ""},
		{"groq", cfg.GroqAgentModel, cfg.GroqAPIKey != "" && cfg.GroqAgentModel != ""},
		{"huggingface", cfg.HFAgentModel, cfg.HuggingFaceToken != "" && cfg.HFAgentModel != ""},
		{"ollama", cfg.OllamaAgentModel, cfg.OllamaBaseURL != "" && cfg.OllamaAgentModel != ""},
		{"openrouter", cfg.OpenRouterAgentModel, cfg.OpenRouterAPIKey != "" && cfg.OpenRouterAgentModel != ""},
	}

	for _, spec := range agentSpecs {
		if !spec.enabled {
			continue
		}
		m := genkit.LookupModel(g, spec.provider+"/"+spec.model)
		if m == nil {
			slog.Warn("agent model not found", "provider", spec.provider, "model", spec.model)
			continue
		}
		rt.agentModels = append(rt.agentModels, m)
		registerModelInfo(rt, spec.provider, spec.model, m, "agent")
		slog.Info("agent model registered", "provider", spec.provider, "model", spec.model)
	}

	return rt, nil
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
