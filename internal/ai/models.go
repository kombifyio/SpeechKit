package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

const (
	openAIBaseURL     = "https://api.openai.com/v1"
	groqBaseURL       = "https://api.groq.com/openai/v1"
	hfBaseURL         = "https://router.huggingface.co/hf-inference/v1"
	openRouterBaseURL = "https://openrouter.ai/api/v1"
	chatCompletions   = "chat/completions"
	maxRespBody       = 1 << 20 // 1 MB
)

// AICallValidation controls URL validation for OpenAI-compatible LLM calls.
// Zero value = strict (public https only). Tests relax it to allow loopback.
var AICallValidation = netsec.ValidationOptions{}

// newAIClient builds a hardened HTTP client for LLM calls (TLS 1.2+,
// redacting transport, long-running timeout).
func newAIClient() *http.Client {
	return netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 60 * time.Second})
}

// registerOpenAIModels registers OpenAI models as custom Genkit models.
func registerOpenAIModels(g *genkit.Genkit, apiKey string) {
	client := newAIClient()
	models := []string{
		"gpt-5.4-mini-2026-03-17",
		"gpt-5.4-2026-03-05",
		"gpt-4o-mini",
		"gpt-4o",
		"gpt-4-turbo",
	}

	for _, name := range models {
		registerOpenAICompatibleModel(g, "openai", name, openAIBaseURL, apiKey, client, true)
	}
}

// registerGroqModels registers Groq models as custom Genkit models.
// Groq uses an OpenAI-compatible API.
func registerGroqModels(g *genkit.Genkit, apiKey string) {
	client := newAIClient()
	models := []string{
		"llama-3.1-8b-instant",
		"llama-3.3-70b-versatile",
		"llama-3.1-70b-versatile",
		"gemma2-9b-it",
		"mixtral-8x7b-32768",
	}

	for _, name := range models {
		registerOpenAICompatibleModel(g, "groq", name, groqBaseURL, apiKey, client, true)
	}
}

// registerHFModels registers HuggingFace Inference API models as custom Genkit models.
// HF uses an OpenAI-compatible chat completions endpoint.
func registerHFModels(g *genkit.Genkit, token string) {
	client := newAIClient()
	models := []string{
		"Qwen/Qwen3.5-9B",
		"Qwen/Qwen3.5-27B",
		"Qwen/Qwen2.5-7B-Instruct",
		"Qwen/Qwen2.5-32B-Instruct",
		"meta-llama/Llama-3.1-8B-Instruct",
	}

	for _, name := range models {
		registerOpenAICompatibleModel(g, "huggingface", name, hfBaseURL, token, client, false)
	}
}

// registerOpenRouterModels registers OpenRouter models as custom Genkit models.
// OpenRouter uses an OpenAI-compatible API with a different base URL.
func registerOpenRouterModels(g *genkit.Genkit, apiKey string) {
	client := newAIClient()
	models := []string{
		"meta-llama/llama-3.1-8b-instruct",
		"google/gemini-2.5-flash",
	}

	for _, name := range models {
		registerOpenAICompatibleModel(g, "openrouter", name, openRouterBaseURL, apiKey, client, true)
	}
}

// registerOpenAICompatibleModel registers a single model that speaks the OpenAI chat completions API.
func registerOpenAICompatibleModel(g *genkit.Genkit, provider, name, baseURL, authToken string, client *http.Client, supportsTools bool) {
	genkit.DefineModel(g, provider+"/"+name,
		&ai.ModelOptions{
			Label: provider + "/" + name,
			Supports: &ai.ModelSupports{
				Multiturn:  true,
				SystemRole: true,
				Media:      false,
				Tools:      supportsTools,
			},
		},
		func(ctx context.Context, mr *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
			return callOpenAICompatible(ctx, client, baseURL, authToken, name, mr)
		},
	)
}

// OpenAI-compatible request/response types.

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
}

type oaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func callOpenAICompatible(ctx context.Context, client *http.Client, baseURL, authToken, model string, mr *ai.ModelRequest) (*ai.ModelResponse, error) {
	var messages []oaiMessage
	for _, m := range mr.Messages {
		role := string(m.Role)
		if role == "model" {
			role = "assistant"
		}
		var text string
		for _, p := range m.Content {
			if p.IsText() {
				text += p.Text
			}
		}
		messages = append(messages, oaiMessage{Role: role, Content: text})
	}

	reqBody := oaiRequest{
		Model:    model,
		Messages: messages,
	}

	if cfg, ok := mr.Config.(*ai.GenerationCommonConfig); ok && cfg != nil {
		if cfg.MaxOutputTokens > 0 {
			reqBody.MaxTokens = cfg.MaxOutputTokens
		}
		if cfg.Temperature > 0 {
			t := cfg.Temperature
			reqBody.Temperature = &t
		}
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint, err := netsec.BuildEndpoint(baseURL, chatCompletions, AICallValidation)
	if err != nil {
		return nil, fmt.Errorf("%s endpoint: %w", model, err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+authToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s request: %w", model, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s error (%d): %s", model, resp.StatusCode, string(body))
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("%s: no choices in response", model)
	}

	return &ai.ModelResponse{
		Message: &ai.Message{
			Content: []*ai.Part{ai.NewTextPart(oaiResp.Choices[0].Message.Content)},
			Role:    ai.RoleModel,
		},
		FinishReason: ai.FinishReason(oaiResp.Choices[0].FinishReason),
		Usage: &ai.GenerationUsage{
			TotalTokens: oaiResp.Usage.TotalTokens,
		},
	}, nil
}
