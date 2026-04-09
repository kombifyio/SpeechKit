package ai

import (
	"context"
	"log/slog"

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

	GoogleUtilityModel  string
	GoogleAgentModel    string
	OpenAIUtilityModel  string
	OpenAIAgentModel    string
	GroqUtilityModel    string
	GroqAgentModel      string
	HFUtilityModel      string
	HFAgentModel        string
	OllamaUtilityModel  string
	OllamaAgentModel    string
}

// ModelInfo describes a registered model for the UI.
type ModelInfo struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Tier     string `json:"tier"` // "utility", "agent", or "both"
}

// Runtime holds the Genkit instance and categorized model references.
type Runtime struct {
	G             *genkit.Genkit
	utilityModels []ai.Model
	agentModels   []ai.Model
	allModels     map[string]ai.Model
	modelInfos    []ModelInfo
}

// UtilityModels returns the models configured for utility tasks (summarize, codewords).
func (r *Runtime) UtilityModels() []ai.Model { return r.utilityModels }

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
		id := spec.provider + "/" + spec.model
		rt.allModels[id] = m
		rt.modelInfos = append(rt.modelInfos, ModelInfo{ID: id, Provider: spec.provider, Name: spec.model, Tier: "utility"})
		slog.Info("utility model registered", "provider", spec.provider, "model", spec.model)
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
		id := spec.provider + "/" + spec.model
		if _, ok := rt.allModels[id]; ok {
			// Already in allModels from utility, update tier.
			for i := range rt.modelInfos {
				if rt.modelInfos[i].ID == id {
					rt.modelInfos[i].Tier = "both"
					break
				}
			}
		} else {
			rt.allModels[id] = m
			rt.modelInfos = append(rt.modelInfos, ModelInfo{ID: id, Provider: spec.provider, Name: spec.model, Tier: "agent"})
		}
		slog.Info("agent model registered", "provider", spec.provider, "model", spec.model)
	}

	return rt, nil
}
