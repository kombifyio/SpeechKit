package flows

import (
	"context"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/generation"
)

func generatorForModels(models []appai.Model, purposes ...generation.Purpose) generation.Generator {
	bindings := make([]generation.BoundModel, 0, len(models))
	for _, model := range models {
		if model == nil {
			continue
		}
		provider, name := splitModelName(model.ID())
		bindings = append(bindings, bindNativeModel(model, generation.Model{
			ID:                       model.ID(),
			Provider:                 provider,
			Name:                     name,
			Purposes:                 append([]generation.Purpose(nil), purposes...),
			ContextWindowTokens:      generation.ConservativeContextWindow(provider, name),
			SupportsStructuredOutput: true,
			Cloud:                    provider != "local" && provider != "ollama",
		}))
	}
	return generation.NewBound(bindings)
}

func bindNativeModel(model appai.Model, info generation.Model) generation.BoundModel {
	return generation.BoundModel{
		Info: info,
		Call: func(ctx context.Context, req generation.Request) (generation.Result, error) {
			native := appai.Request{
				System:    req.System,
				Prompt:    req.Prompt,
				MaxTokens: req.MaxOutputTokens,
			}
			if req.Temperature != 0 {
				temp := req.Temperature
				native.Temperature = &temp
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
				Usage:        resp.Usage,
			}, nil
		},
	}
}

func splitModelName(full string) (string, string) {
	for index, char := range full {
		if char == '/' {
			return full[:index], full[index+1:]
		}
	}
	return "", full
}
