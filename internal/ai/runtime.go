// Package ai wires SpeechKit's model catalog into a single LLM surface used
// by Assist, summaries, meeting notes, and the Voice Agent pipeline-fallback
// path.
//
// It owns provider keys, model selection, OpenAI-compatible execution, and
// the per-modality plumbing (Utility, Assist, Agent). Routing decisions live
// in [github.com/kombifyio/SpeechKit/internal/router] and
// [github.com/kombifyio/SpeechKit/internal/tts]; this package is the
// model substrate they call into.
package ai

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/ai/generation"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Message is one text-only turn sent to a model boundary.
type Message struct {
	Role    string
	Content string
}

// Request is the provider-neutral generation contract used by SpeechKit
// flows. It deliberately excludes provider credentials and routing controls.
type Request struct {
	System      string
	Prompt      string
	Messages    []Message
	MaxTokens   int
	Temperature *float64
}

// Response is the text result returned by a model boundary.
type Response struct {
	Text         string
	FinishReason string
	Usage        *generation.Usage
}

// Model is the only model capability SpeechKit flows require.
type Model interface {
	ID() string
	Generate(context.Context, Request) (Response, error)
}

// Config holds all provider API keys and model selections for runtime
// initialization.
type Config struct {
	OpenAIAPIKey     string
	GroqAPIKey       string
	HuggingFaceToken string
	OllamaBaseURL    string
	LocalLLMBaseURL  string
	// LocalLLMTransport wraps the HTTP transport that carries requests to the
	// bundled local model server. The desktop host uses it to wake a server it
	// paused to free memory and to notice when the server was last used. Nil
	// leaves the transport as built.
	LocalLLMTransport func(next http.RoundTripper) http.RoundTripper

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
	// FoundryBearerToken mints a token per request when the Foundry resource
	// is used with a Microsoft sign-in instead of the resource key. Either
	// the key or the token source enables the Foundry models.
	FoundryBearerToken speechkit.BearerTokenFunc
	// FoundryBaseURL is the OpenAI-compatible base (https://<host>/openai/v1);
	// FoundryMAIBaseURL serves Microsoft-publisher deployments such as
	// MAI-Thinking-1 (https://<host>/mai/v1). Empty skips those deployments.
	FoundryBaseURL         string
	FoundryMAIBaseURL      string
	FoundryUtilityModel    string
	FoundryAssistModel     string
	FoundryAgentModel      string
	OrderedAssistModels    []OrderedModelSelection
	OrderedAgentModels     []OrderedModelSelection
	UseOrderedAssistModels bool
	UseOrderedAgentModels  bool
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

// Runtime groups model choices by SpeechKit modality. It contains no
// framework state.
type Runtime struct {
	utilityModels []Model
	assistModels  []Model
	agentModels   []Model
	allModels     map[string]Model
	modelInfos    []ModelInfo
}

// UtilityModels returns the models configured for utility tasks (summarize, codewords).
func (r *Runtime) UtilityModels() []Model { return r.utilityModels }

// AssistModels returns the models configured for direct Assist replies.
func (r *Runtime) AssistModels() []Model { return r.assistModels }

// AgentModels returns the models configured for agent tasks (reasoning, autonomous).
func (r *Runtime) AgentModels() []Model { return r.agentModels }

// AllModels returns all registered models keyed by their full ID.
func (r *Runtime) AllModels() map[string]Model {
	if r == nil {
		return nil
	}
	return r.allModels
}

// ModelInfos returns metadata about all registered models for the UI.
func (r *Runtime) ModelInfos() []ModelInfo {
	if r == nil {
		return nil
	}
	return r.modelInfos
}

// Generator exposes the configured model pool through SpeechKit's
// provider-neutral generation boundary.
func (r *Runtime) Generator() generation.Generator {
	return r.GeneratorWhere(nil)
}

// GeneratorWhere exposes the configured models the filter keeps, in the same
// order. A nil filter keeps every model.
func (r *Runtime) GeneratorWhere(keep func(generation.Model) bool) generation.Generator {
	if r == nil {
		return generation.NewBound(nil)
	}
	bindings := make([]generation.BoundModel, 0, len(r.modelInfos))
	for _, info := range r.modelInfos {
		model := r.allModels[info.ID]
		if model == nil {
			continue
		}
		described := generation.Model{
			ID:                       info.ID,
			Provider:                 info.Provider,
			Name:                     info.Name,
			Purposes:                 generationPurposes(info.Tier),
			ContextWindowTokens:      generation.ConservativeContextWindow(info.Provider, info.Name),
			SupportsStructuredOutput: true,
			Cloud:                    info.Provider != "local" && info.Provider != "ollama",
		}
		if keep != nil && !keep(described) {
			continue
		}
		bindings = append(bindings, bindNativeModel(model, described))
	}
	return generation.NewBound(bindings)
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

func bindNativeModel(model Model, info generation.Model) generation.BoundModel {
	return generation.BoundModel{
		Info: info,
		Call: func(ctx context.Context, req generation.Request) (generation.Result, error) {
			started := time.Now()
			native := Request{
				System:    req.System,
				Prompt:    req.Prompt,
				MaxTokens: req.MaxOutputTokens,
			}
			if req.Temperature != 0 {
				temp := req.Temperature
				native.Temperature = &temp
			}
			for _, message := range req.Messages {
				native.Messages = append(native.Messages, Message{
					Role:    string(message.Role),
					Content: message.Content,
				})
			}
			resp, err := model.Generate(ctx, native)
			if err != nil {
				return generation.Result{}, err
			}
			return generation.Result{
				Text:         resp.Text,
				Provider:     info.Provider,
				Model:        info.Name,
				FinishReason: resp.FinishReason,
				Latency:      time.Since(started),
				Usage:        resp.Usage,
			}, nil
		},
	}
}

// Init resolves configured models. Network requests happen only in Model.Generate.
func Init(_ context.Context, cfg Config) (*Runtime, error) {
	rt := &Runtime{allModels: make(map[string]Model)}

	for _, spec := range tierModelSpecs(cfg, "utility") {
		if !spec.enabled {
			continue
		}
		rt.register(cfg, spec.provider, spec.model, "utility", &rt.utilityModels)
	}

	resolveOrderedOrLegacyModels(rt, cfg, "assist", cfg.UseOrderedAssistModels, cfg.OrderedAssistModels, tierModelSpecs(cfg, "assist"), &rt.assistModels)
	resolveOrderedOrLegacyModels(rt, cfg, "agent", cfg.UseOrderedAgentModels, cfg.OrderedAgentModels, tierModelSpecs(cfg, "agent"), &rt.agentModels)

	return rt, nil
}

func resolveOrderedOrLegacyModels(
	rt *Runtime,
	cfg Config,
	tier string,
	useOrdered bool,
	ordered []OrderedModelSelection,
	legacy []modelSpec,
	destination *[]Model,
) {
	if useOrdered {
		for _, spec := range ordered {
			rt.register(cfg, spec.Provider, spec.Model, tier, destination)
		}
		return
	}

	for _, spec := range legacy {
		if !spec.enabled {
			continue
		}
		rt.register(cfg, spec.provider, spec.model, tier, destination)
	}
}

func (rt *Runtime) register(cfg Config, provider, model, tier string, destination *[]Model) {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" {
		return
	}
	key := provider + "/" + model
	resolved, ok := rt.allModels[key]
	if !ok {
		var err error
		resolved, err = newModel(cfg, provider, model)
		if err != nil {
			slog.Warn(tier+" model configuration ignored", "provider", provider, "model", model, "reason", err)
			return
		}
		rt.allModels[key] = resolved
		rt.modelInfos = append(rt.modelInfos, ModelInfo{
			ID:       key,
			Provider: provider,
			Name:     model,
			Tier:     tier,
		})
	} else {
		for i := range rt.modelInfos {
			if rt.modelInfos[i].ID == key {
				rt.modelInfos[i].Tier = mergeModelTier(rt.modelInfos[i].Tier, tier)
				break
			}
		}
	}
	*destination = append(*destination, resolved)
	slog.Info(tier+" model registered", "provider", provider, "model", model)
}

func tierModelSpecs(cfg Config, tier string) []modelSpec {
	switch tier {
	case "utility":
		return []modelSpec{
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
	return (cfg.FoundryAPIKey != "" || cfg.FoundryBearerToken != nil) && cfg.FoundryBaseURL != ""
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
