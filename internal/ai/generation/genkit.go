package generation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	firebaseai "github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"google.golang.org/genai"
)

type GenkitModel struct {
	Model firebaseai.Model
	Info  Model
}

type GenkitGenerator struct {
	runtime *genkit.Genkit
	models  []GenkitModel
}

func NewGenkit(runtime *genkit.Genkit, models []GenkitModel) *GenkitGenerator {
	return &GenkitGenerator{runtime: runtime, models: append([]GenkitModel(nil), models...)}
}

func (g *GenkitGenerator) Models(_ context.Context, query ModelQuery) (Catalog, error) {
	if g == nil {
		return Catalog{}, &Error{Kind: ErrorConfiguration, Operation: "models", Err: errors.New("genkit runtime unavailable")}
	}
	catalog := Catalog{Models: make([]Model, 0, len(g.models))}
	for _, binding := range g.models {
		if binding.Model == nil || !binding.Info.Supports(query.Purpose) {
			continue
		}
		catalog.Models = append(catalog.Models, binding.Info)
	}
	return catalog, nil
}

func (g *GenkitGenerator) Generate(ctx context.Context, request Request) (Result, error) {
	if g == nil || g.runtime == nil {
		return Result{}, &Error{Kind: ErrorConfiguration, Operation: "generate", Err: errors.New("genkit runtime unavailable")}
	}
	candidates := g.candidates(request)
	if len(candidates) == 0 {
		return Result{}, &Error{Kind: ErrorConfiguration, Operation: "generate", Model: request.ModelID, Err: errors.New("no matching model configured")}
	}

	var lastErr error
	for _, candidate := range candidates {
		started := time.Now()
		options := []firebaseai.GenerateOption{
			firebaseai.WithModel(candidate.Model),
			firebaseai.WithSystem(request.System),
			firebaseai.WithPrompt(renderPrompt(request)),
			firebaseai.WithConfig(genkitConfig(candidate.Model, request.MaxOutputTokens, request.Temperature)),
		}
		response, err := genkit.Generate(ctx, g.runtime, options...)
		if err != nil {
			lastErr = classifyGenkitError(candidate.Info, err)
			if ctx.Err() != nil {
				return Result{}, lastErr
			}
			continue
		}
		result := Result{
			Text:         response.Text(),
			Provider:     candidate.Info.Provider,
			Model:        candidate.Info.Name,
			FinishReason: string(response.FinishReason),
			Latency:      time.Since(started),
		}
		if response.Usage != nil {
			result.Usage = &Usage{
				InputTokens:  response.Usage.InputTokens,
				OutputTokens: response.Usage.OutputTokens,
				TotalTokens:  response.Usage.TotalTokens,
			}
		}
		return result, nil
	}
	return Result{}, lastErr
}

func (g *GenkitGenerator) candidates(request Request) []GenkitModel {
	out := make([]GenkitModel, 0, len(g.models))
	for _, candidate := range g.models {
		if candidate.Model == nil || !candidate.Info.Supports(request.Purpose) {
			continue
		}
		if request.ModelID != "" && request.ModelID != candidate.Info.ID {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func renderPrompt(request Request) string {
	var out strings.Builder
	for _, message := range request.Messages {
		if strings.TrimSpace(message.Content) == "" || message.Role == RoleSystem {
			continue
		}
		fmt.Fprintf(&out, "%s: %s\n\n", message.Role, message.Content)
	}
	out.WriteString(request.Prompt)
	if request.StructuredHint != "" {
		fmt.Fprintf(&out, "\n\nReturn data matching this structure:\n%s", request.StructuredHint)
	}
	return out.String()
}

func genkitConfig(model firebaseai.Model, maxOutputTokens int, temperature float64) any {
	if maxOutputTokens <= 0 {
		maxOutputTokens = 1024
	}
	if model != nil && strings.HasPrefix(model.Name(), "googleai/") {
		value := float32(temperature)
		return &genai.GenerateContentConfig{
			MaxOutputTokens: int32(maxOutputTokens),
			Temperature:     &value,
		}
	}
	return &firebaseai.GenerationCommonConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     temperature,
	}
}

func classifyGenkitError(model Model, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Kind: ErrorCancelled, Operation: "generate", Provider: model.Provider, Model: model.Name, Err: err}
	}
	message := strings.ToLower(err.Error())
	kind := ErrorPermanent
	retryable := false
	switch {
	case isConnectionError(err, message):
		// The model server did not answer: llama-server still loading after a
		// start or an update, or a remote endpoint dropping the connection.
		// Nothing about the request is wrong, so the work must stay
		// retryable. Classified permanent, the startup pass of 2026-09-03
		// marked every pending meeting summary of three meetings "failed"
		// within a second of launch, before the local runtime had come up.
		kind = ErrorTransient
		retryable = true
	case strings.Contains(message, "context") && (strings.Contains(message, "limit") || strings.Contains(message, "length") || strings.Contains(message, "exceed")):
		kind = ErrorContextLimit
	case strings.Contains(message, "unauthorized"), strings.Contains(message, "authentication"), strings.Contains(message, "api key"):
		kind = ErrorAuthentication
	case strings.Contains(message, "quota"), strings.Contains(message, "rate limit"), strings.Contains(message, "429"):
		kind = ErrorQuota
		retryable = true
	case strings.Contains(message, "timeout"), strings.Contains(message, "temporar"), strings.Contains(message, "unavailable"), strings.Contains(message, "500"), strings.Contains(message, "502"), strings.Contains(message, "503"):
		kind = ErrorTransient
		retryable = true
	}
	return &Error{Kind: kind, Operation: "generate", Provider: model.Provider, Model: model.Name, Retryable: retryable, Err: err}
}

// isConnectionError reports transport-level failures — the request never got
// an answer from the model server. The typed checks cover the Go net stack
// (dial failures, resets, dropped connections); the text checks cover errors a
// plugin flattened to a string on the way up.
func isConnectionError(err error, message string) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	for _, needle := range []string{
		"connection refused",
		"actively refused",
		"no connection could be made",
		"connection reset",
		"dial tcp",
		"unexpected eof",
		"broken pipe",
	} {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}
