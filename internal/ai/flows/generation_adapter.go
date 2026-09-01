package flows

import (
	firebaseai "github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/kombifyio/SpeechKit/internal/ai/generation"
)

func generatorForModels(g *genkit.Genkit, models []firebaseai.Model, purposes ...generation.Purpose) generation.Generator {
	bindings := make([]generation.GenkitModel, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		provider, name := splitModelName(model.Name())
		bindings = append(bindings, generation.GenkitModel{
			Model: model,
			Info: generation.Model{
				ID:                       model.Name(),
				Provider:                 provider,
				Name:                     name,
				Purposes:                 append([]generation.Purpose(nil), purposes...),
				ContextWindowTokens:      generation.ConservativeContextWindow(provider, name),
				SupportsStructuredOutput: true,
				Cloud:                    provider != "local" && provider != "ollama",
			},
		})
	}
	return generation.NewGenkit(g, bindings)
}

func splitModelName(full string) (string, string) {
	for index, char := range full {
		if char == '/' {
			return full[:index], full[index+1:]
		}
	}
	return "", full
}
