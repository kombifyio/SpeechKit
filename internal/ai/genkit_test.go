package ai

import (
	"context"
	"testing"

	"github.com/firebase/genkit/go/genkit"
)

func TestInit_EmptyConfig(t *testing.T) {
	rt, err := Init(context.Background(), Config{})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if rt == nil {
		t.Fatal("expected non-nil runtime")
	}
	if rt.G == nil {
		t.Fatal("expected non-nil Genkit instance")
	}
	if len(rt.UtilityModels()) != 0 {
		t.Errorf("expected 0 utility models, got %d", len(rt.UtilityModels()))
	}
	if len(rt.AssistModels()) != 0 {
		t.Errorf("expected 0 assist models, got %d", len(rt.AssistModels()))
	}
	if len(rt.AgentModels()) != 0 {
		t.Errorf("expected 0 agent models, got %d", len(rt.AgentModels()))
	}
	if len(rt.AllModels()) != 0 {
		t.Errorf("expected 0 models, got %d", len(rt.AllModels()))
	}
	if len(rt.ModelInfos()) != 0 {
		t.Errorf("expected 0 model infos, got %d", len(rt.ModelInfos()))
	}
}

func TestInit_CustomModelRegistration(t *testing.T) {
	// Init with OpenAI key registers custom models; then LookupModel finds them.
	rt, err := Init(context.Background(), Config{
		OpenAIAPIKey:       "test-key",
		OpenAIUtilityModel: "gpt-4o-mini",
		OpenAIAssistModel:  "gpt-4o",
		OpenAIAgentModel:   "gpt-4-turbo",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if len(rt.UtilityModels()) != 1 {
		t.Errorf("utility models = %d, want 1", len(rt.UtilityModels()))
	}
	if len(rt.AssistModels()) != 1 {
		t.Errorf("assist models = %d, want 1", len(rt.AssistModels()))
	}
	if len(rt.AgentModels()) != 1 {
		t.Errorf("agent models = %d, want 1", len(rt.AgentModels()))
	}

	// Both should be in AllModels
	if len(rt.AllModels()) != 3 {
		t.Errorf("all models = %d, want 3", len(rt.AllModels()))
	}
	if _, ok := rt.AllModels()["openai/gpt-4o-mini"]; !ok {
		t.Error("expected openai/gpt-4o-mini in AllModels")
	}
	if _, ok := rt.AllModels()["openai/gpt-4o"]; !ok {
		t.Error("expected openai/gpt-4o in AllModels")
	}
	if _, ok := rt.AllModels()["openai/gpt-4-turbo"]; !ok {
		t.Error("expected openai/gpt-4-turbo in AllModels")
	}

	// ModelInfos should have correct tiers
	infos := rt.ModelInfos()
	if len(infos) != 3 {
		t.Fatalf("model infos = %d, want 3", len(infos))
	}
	for _, info := range infos {
		if info.Provider != "openai" {
			t.Errorf("provider = %q", info.Provider)
		}
		switch info.Name {
		case "gpt-4o-mini":
			if info.Tier != "utility" {
				t.Errorf("gpt-4o-mini tier = %q, want utility", info.Tier)
			}
		case "gpt-4o":
			if info.Tier != "assist" {
				t.Errorf("gpt-4o tier = %q, want assist", info.Tier)
			}
		case "gpt-4-turbo":
			if info.Tier != "agent" {
				t.Errorf("gpt-4-turbo tier = %q, want agent", info.Tier)
			}
		default:
			t.Errorf("unexpected model name %q", info.Name)
		}
	}
}

func TestInit_SameModelUtilityAndAssistTiers(t *testing.T) {
	rt, err := Init(context.Background(), Config{
		GroqAPIKey:       "test-key",
		GroqUtilityModel: "llama-3.1-8b-instant",
		GroqAssistModel:  "llama-3.1-8b-instant",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if len(rt.UtilityModels()) != 1 {
		t.Errorf("utility models = %d, want 1", len(rt.UtilityModels()))
	}
	if len(rt.AssistModels()) != 1 {
		t.Errorf("assist models = %d, want 1", len(rt.AssistModels()))
	}
	if len(rt.AgentModels()) != 0 {
		t.Errorf("agent models = %d, want 0", len(rt.AgentModels()))
	}
	// AllModels deduplicates
	if len(rt.AllModels()) != 1 {
		t.Errorf("all models = %d, want 1", len(rt.AllModels()))
	}

	infos := rt.ModelInfos()
	if len(infos) != 1 {
		t.Fatalf("model infos = %d, want 1", len(infos))
	}
	if infos[0].Tier != "utility+assist" {
		t.Errorf("tier = %q, want utility+assist", infos[0].Tier)
	}
}

func TestInit_SameModelAllTiers(t *testing.T) {
	rt, err := Init(context.Background(), Config{
		GroqAPIKey:       "test-key",
		GroqUtilityModel: "llama-3.1-8b-instant",
		GroqAssistModel:  "llama-3.1-8b-instant",
		GroqAgentModel:   "llama-3.1-8b-instant",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if len(rt.UtilityModels()) != 1 {
		t.Errorf("utility models = %d, want 1", len(rt.UtilityModels()))
	}
	if len(rt.AssistModels()) != 1 {
		t.Errorf("assist models = %d, want 1", len(rt.AssistModels()))
	}
	if len(rt.AgentModels()) != 1 {
		t.Errorf("agent models = %d, want 1", len(rt.AgentModels()))
	}
	// AllModels deduplicates
	if len(rt.AllModels()) != 1 {
		t.Errorf("all models = %d, want 1", len(rt.AllModels()))
	}

	// Tier should be "both"
	infos := rt.ModelInfos()
	if len(infos) != 1 {
		t.Fatalf("model infos = %d, want 1", len(infos))
	}
	if infos[0].Tier != "all" {
		t.Errorf("tier = %q, want all", infos[0].Tier)
	}
}

func TestInit_MultipleProviders(t *testing.T) {
	rt, err := Init(context.Background(), Config{
		OpenAIAPIKey:       "openai-key",
		OpenAIUtilityModel: "gpt-4o-mini",
		GroqAPIKey:         "groq-key",
		GroqAssistModel:    "llama-3.1-8b-instant",
		HuggingFaceToken:   "hf-token",
		HFUtilityModel:     "Qwen/Qwen2.5-7B-Instruct",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if len(rt.UtilityModels()) != 2 {
		t.Errorf("utility models = %d, want 2", len(rt.UtilityModels()))
	}
	if len(rt.AssistModels()) != 1 {
		t.Errorf("assist models = %d, want 1", len(rt.AssistModels()))
	}
	if len(rt.AgentModels()) != 0 {
		t.Errorf("agent models = %d, want 0", len(rt.AgentModels()))
	}
	if len(rt.AllModels()) != 3 {
		t.Errorf("all models = %d, want 3", len(rt.AllModels()))
	}
}

func TestInit_DisabledProviderIgnored(t *testing.T) {
	// API key empty → provider disabled, model selection ignored.
	rt, err := Init(context.Background(), Config{
		OpenAIAPIKey:       "",
		OpenAIUtilityModel: "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(rt.UtilityModels()) != 0 {
		t.Errorf("expected 0 utility models when provider disabled, got %d", len(rt.UtilityModels()))
	}
}

func TestInit_LookupModelDirectly(t *testing.T) {
	rt, err := Init(context.Background(), Config{
		OpenAIAPIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// All registered OpenAI models should be findable
	for _, name := range []string{"gpt-4o-mini", "gpt-4o"} {
		m := genkit.LookupModel(rt.G, "openai/"+name)
		if m == nil {
			t.Errorf("LookupModel(%q) returned nil", "openai/"+name)
		}
	}
}

func TestInit_GroqModelsRegistered(t *testing.T) {
	rt, err := Init(context.Background(), Config{
		GroqAPIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, name := range []string{"llama-3.1-8b-instant", "llama-3.3-70b-versatile", "gemma2-9b-it", "mixtral-8x7b-32768"} {
		m := genkit.LookupModel(rt.G, "groq/"+name)
		if m == nil {
			t.Errorf("LookupModel(%q) returned nil", "groq/"+name)
		}
	}
}

func TestInit_HFModelsRegistered(t *testing.T) {
	rt, err := Init(context.Background(), Config{
		HuggingFaceToken: "test-token",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, name := range []string{"Qwen/Qwen2.5-7B-Instruct", "Qwen/Qwen2.5-32B-Instruct", "meta-llama/Llama-3.1-8B-Instruct"} {
		m := genkit.LookupModel(rt.G, "huggingface/"+name)
		if m == nil {
			t.Errorf("LookupModel(%q) returned nil", "huggingface/"+name)
		}
	}
}
