package flows

import (
	"context"
	"math"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"google.golang.org/genai"
)

type testModel string

func (m testModel) Name() string { return string(m) }

func (m testModel) Generate(context.Context, *ai.ModelRequest, ai.ModelStreamCallback) (*ai.ModelResponse, error) {
	return nil, nil
}

func (m testModel) Register(api.Registry) {}

func TestGenerationConfigForModelUsesGoogleGenAIConfig(t *testing.T) {
	cfg, ok := generationConfigForModel(testModel("googleai/gemini-2.5-flash"), 1024, 0.4).(*genai.GenerateContentConfig)
	if !ok {
		t.Fatalf("config type = %T, want *genai.GenerateContentConfig", cfg)
	}
	if cfg.MaxOutputTokens != 1024 {
		t.Fatalf("MaxOutputTokens = %d, want 1024", cfg.MaxOutputTokens)
	}
	if cfg.Temperature == nil || *cfg.Temperature != 0.4 {
		t.Fatalf("Temperature = %v, want 0.4", cfg.Temperature)
	}
}

func TestGenerationConfigForModelUsesCommonConfigForCustomModels(t *testing.T) {
	cfg, ok := generationConfigForModel(testModel("openai/gpt-4o"), 256, 0.7).(*ai.GenerationCommonConfig)
	if !ok {
		t.Fatalf("config type = %T, want *ai.GenerationCommonConfig", cfg)
	}
	if cfg.MaxOutputTokens != 256 {
		t.Fatalf("MaxOutputTokens = %d, want 256", cfg.MaxOutputTokens)
	}
	if math.Abs(cfg.Temperature-0.7) > 0.000001 {
		t.Fatalf("Temperature = %v, want 0.7", cfg.Temperature)
	}
}
