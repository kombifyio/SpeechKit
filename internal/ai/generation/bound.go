package generation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type BoundModel struct {
	Info Model
	Call func(context.Context, Request) (Result, error)
}

type BoundGenerator struct {
	models []BoundModel
}

func NewBound(models []BoundModel) *BoundGenerator {
	return &BoundGenerator{models: append([]BoundModel(nil), models...)}
}

func (g *BoundGenerator) Models(_ context.Context, query ModelQuery) (Catalog, error) {
	if g == nil {
		return Catalog{}, &Error{Kind: ErrorConfiguration, Operation: "models", Err: errors.New("model runtime unavailable")}
	}
	catalog := Catalog{Models: make([]Model, 0, len(g.models))}
	for _, binding := range g.models {
		if binding.Call == nil || !binding.Info.Supports(query.Purpose) {
			continue
		}
		catalog.Models = append(catalog.Models, binding.Info)
	}
	return catalog, nil
}

func (g *BoundGenerator) Generate(ctx context.Context, request Request) (Result, error) {
	if g == nil {
		return Result{}, &Error{Kind: ErrorConfiguration, Operation: "generate", Err: errors.New("model runtime unavailable")}
	}
	candidates := g.candidates(request)
	if len(candidates) == 0 {
		return Result{}, &Error{Kind: ErrorConfiguration, Operation: "generate", Model: request.ModelID, Err: errors.New("no matching model configured")}
	}

	nativeRequest := request
	nativeRequest.Prompt = renderPrompt(request)
	nativeRequest.Messages = nil
	nativeRequest.StructuredHint = ""

	var lastErr error
	for _, candidate := range candidates {
		started := time.Now()
		result, err := candidate.Call(ctx, nativeRequest)
		if err != nil {
			lastErr = classifyGenerateError(candidate.Info, err)
			if ctx.Err() != nil {
				return Result{}, lastErr
			}
			continue
		}
		if result.Provider == "" {
			result.Provider = candidate.Info.Provider
		}
		if result.Model == "" {
			result.Model = candidate.Info.Name
		}
		if result.Latency == 0 {
			result.Latency = time.Since(started)
		}
		return result, nil
	}
	return Result{}, lastErr
}

func (g *BoundGenerator) candidates(request Request) []BoundModel {
	out := make([]BoundModel, 0, len(g.models))
	for _, candidate := range g.models {
		if candidate.Call == nil || !candidate.Info.Supports(request.Purpose) {
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

func classifyGenerateError(model Model, err error) error {
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
