package flows

import (
	"strings"

	"github.com/firebase/genkit/go/ai"
	"google.golang.org/genai"
)

func generationConfigForModel(model ai.Model, maxOutputTokens int32, temperature float64) any {
	if model != nil && strings.HasPrefix(model.Name(), "googleai/") {
		googleTemperature := float32(temperature)
		return &genai.GenerateContentConfig{
			MaxOutputTokens: maxOutputTokens,
			Temperature:     &googleTemperature,
		}
	}

	return &ai.GenerationCommonConfig{
		MaxOutputTokens: int(maxOutputTokens),
		Temperature:     temperature,
	}
}

func generationConfigOption(model ai.Model, maxOutputTokens int32, temperature float64) ai.GenerateOption {
	return ai.WithConfig(generationConfigForModel(model, maxOutputTokens, temperature))
}
