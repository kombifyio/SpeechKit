package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/ai/generation"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const (
	openAIBaseURL     = "https://api.openai.com/v1"
	groqBaseURL       = "https://api.groq.com/openai/v1"
	hfBaseURL         = "https://router.huggingface.co/hf-inference/v1"
	openRouterBaseURL = "https://openrouter.ai/api/v1"
	// assemblyAILLMGatewayBaseURL is the OpenAI-compatible AssemblyAI LLM
	// Gateway. Same API key as STT. EU residency uses
	// https://llm-gateway.eu.assemblyai.com/v1.
	assemblyAILLMGatewayBaseURL = "https://llm-gateway.assemblyai.com/v1"
	chatCompletions             = "chat/completions"
	maxRespBody                 = 1 << 20 // 1 MB
)

// AICallValidation controls URL validation for OpenAI-compatible LLM calls.
// Zero value = strict (public https only). Tests relax it to allow loopback.
var AICallValidation = netsec.ValidationOptions{}

var localLLMCallValidation = netsec.ValidationOptions{AllowLoopback: true, AllowPrivate: true, AllowHTTP: true}

// newAIClient builds a hardened HTTP client for LLM calls (TLS 1.2+,
// redacting transport, resolve-time IP validation, long-running timeout).
// The 180s ceiling accommodates first-token latency of CPU-bound local
// llama-server on large prompts; cloud providers normally respond in seconds
// and the larger ceiling only kicks in on genuine slowness.
func newAIClient(validation *netsec.ValidationOptions) *http.Client {
	return netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 180 * time.Second, DialValidation: validation})
}

// newLocalAIClient is the same hardened client with a far longer ceiling,
// because the bundled model runs on the CPU and is slow in a way no cloud
// provider is: measured at single-digit tokens per second, a long answer — a
// meeting write-up, most of all — takes minutes of generation, and the 180s
// ceiling cut those requests off mid-answer. The real bound is the caller's
// context deadline, which every long-running caller sets; this ceiling only
// stops a wedged local server from being held open forever.
func newLocalAIClient(validation *netsec.ValidationOptions) *http.Client {
	return netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Minute, DialValidation: validation})
}

func newModel(cfg Config, provider, name string) (Model, error) {
	switch strings.TrimSpace(provider) {
	case "openai":
		opts := oaiCallOptions{AuthToken: cfg.OpenAIAPIKey}
		if reasoningModelFamily(name) {
			opts.UseMaxCompletionTokens = true
			opts.OmitTemperature = true
		}
		return newOpenAICompatibleModel("openai", name, openAIBaseURL, newAIClient(&AICallValidation), AICallValidation, opts), nil
	case "groq":
		return newOpenAICompatibleModel("groq", name, groqBaseURL, newAIClient(&AICallValidation), AICallValidation, oaiCallOptions{AuthToken: cfg.GroqAPIKey}), nil
	case "huggingface":
		return newOpenAICompatibleModel("huggingface", name, hfBaseURL, newAIClient(&AICallValidation), AICallValidation, oaiCallOptions{AuthToken: cfg.HuggingFaceToken}), nil
	case "openrouter":
		return newOpenAICompatibleModel("openrouter", name, openRouterBaseURL, newAIClient(&AICallValidation), AICallValidation, oaiCallOptions{AuthToken: cfg.OpenRouterAPIKey}), nil
	case "assemblyai":
		baseURL := strings.TrimSpace(cfg.AssemblyAILLMGatewayBaseURL)
		if baseURL == "" {
			baseURL = assemblyAILLMGatewayBaseURL
		}
		return newOpenAICompatibleModel("assemblyai", name, baseURL, newAIClient(&AICallValidation), AICallValidation, oaiCallOptions{AuthToken: cfg.AssemblyAIAPIKey}), nil
	case "cloudflare":
		if strings.TrimSpace(cfg.CloudflareAPIKey) == "" || strings.TrimSpace(cfg.CloudflareAccountID) == "" {
			return nil, errors.New("Cloudflare native credentials are unavailable")
		}
		gatewayID := strings.TrimSpace(cfg.CloudflareGatewayID)
		if gatewayID == "" {
			gatewayID = "default"
		}
		baseURL := "https://api.cloudflare.com/client/v4/accounts/" + cfg.CloudflareAccountID + "/ai/v1"
		return newOpenAICompatibleModel("cloudflare", name, baseURL, newAIClient(&AICallValidation), AICallValidation, oaiCallOptions{
			AuthToken:    cfg.CloudflareAPIKey,
			ExtraHeaders: map[string]string{"cf-aig-gateway-id": gatewayID},
		}), nil
	case "foundry":
		if !foundryModelEnabled(cfg) {
			return nil, errors.New("Foundry credentials are unavailable")
		}
		baseURL, opts, ok := foundryModelTarget(foundryRegistration{
			APIKey:      cfg.FoundryAPIKey,
			BearerToken: cfg.FoundryBearerToken,
			BaseURL:     cfg.FoundryBaseURL,
			MAIBaseURL:  cfg.FoundryMAIBaseURL,
		}, name)
		if !ok {
			return nil, fmt.Errorf("Foundry deployment %q cannot be served with the configured bases", name)
		}
		return newOpenAICompatibleModel("foundry", name, baseURL, newAIClient(&AICallValidation), AICallValidation, opts), nil
	case "local":
		client := newLocalAIClient(&localLLMCallValidation)
		if cfg.LocalLLMTransport != nil {
			next := client.Transport
			if next == nil {
				next = http.DefaultTransport
			}
			client.Transport = cfg.LocalLLMTransport(next)
		}
		return newOpenAICompatibleModel("local", name, cfg.LocalLLMBaseURL, client, localLLMCallValidation, oaiCallOptions{}), nil
	case "ollama":
		return &ollamaModel{baseURL: cfg.OllamaBaseURL, name: name, client: newLocalAIClient(&localLLMCallValidation)}, nil
	default:
		return nil, fmt.Errorf("unsupported model provider %q", provider)
	}
}

// foundryRegistration is what Foundry model construction needs from the host:
// one credential (the resource key or a token source), the two inference bases
// and the deployment names the tiers point at.
type foundryRegistration struct {
	APIKey      string
	BearerToken speechkit.BearerTokenFunc
	// BaseURL is https://<host>/openai/v1 for OpenAI-publisher deployments.
	BaseURL string
	// MAIBaseURL is https://<host>/mai/v1 for Microsoft-publisher
	// deployments (MAI-Thinking-1). Empty means those cannot be served.
	MAIBaseURL string
}

// isFoundryMAIDeployment mirrors config.IsMAIThinkingModel without importing
// the config package: Microsoft-publisher chat deployments are served on
// /mai/v1 and take max_completion_tokens instead of max_tokens.
func isFoundryMAIDeployment(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "mai-thinking")
}

// foundryModelTarget picks the base URL and request shape for one deployment.
// ok is false when the deployment cannot be served with the given bases.
func foundryModelTarget(reg foundryRegistration, name string) (baseURL string, opts oaiCallOptions, ok bool) {
	// Every current Azure OpenAI chat deployment accepts max_completion_tokens
	// (verified for gpt-4o, gpt-4.1 and gpt-5.6), while the GPT-5 family
	// rejects max_tokens outright, so the newer spelling is used throughout.
	opts = oaiCallOptions{
		AuthToken:              reg.APIKey,
		BearerToken:            reg.BearerToken,
		UseMaxCompletionTokens: true,
		OmitTemperature:        reasoningModelFamily(name),
	}
	if isFoundryMAIDeployment(name) {
		if strings.TrimSpace(reg.MAIBaseURL) == "" {
			return "", opts, false
		}
		return reg.MAIBaseURL, opts, true
	}
	if strings.TrimSpace(reg.BaseURL) == "" {
		return "", opts, false
	}
	return reg.BaseURL, opts, true
}

type openAICompatibleModel struct {
	provider   string
	name       string
	baseURL    string
	client     *http.Client
	validation netsec.ValidationOptions
	opts       oaiCallOptions
}

func newOpenAICompatibleModel(provider, name, baseURL string, client *http.Client, validation netsec.ValidationOptions, opts oaiCallOptions) *openAICompatibleModel {
	return &openAICompatibleModel{
		provider:   provider,
		name:       name,
		baseURL:    baseURL,
		client:     client,
		validation: validation,
		opts:       opts,
	}
}

func (m *openAICompatibleModel) ID() string { return m.provider + "/" + m.name }

func (m *openAICompatibleModel) Generate(ctx context.Context, input Request) (Response, error) {
	return callOpenAICompatibleWithOptions(ctx, m.client, m.baseURL, m.name, input, m.validation, m.opts)
}

// OpenAI-compatible request/response types.

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model    string       `json:"model"`
	Messages []oaiMessage `json:"messages"`
	// MaxTokens is the classic cap; MaxCompletionTokens is what reasoning
	// models such as MAI-Thinking-1 require instead (they reject max_tokens).
	MaxTokens           int      `json:"max_tokens,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	Temperature         *float64 `json:"temperature,omitempty"`
}

// oaiCallOptions varies one OpenAI-compatible call beyond its base URL and
// model: which credential to send and how to spell the output cap.
type oaiCallOptions struct {
	// AuthToken is the static credential sent as "Authorization: Bearer".
	AuthToken string
	// BearerToken, when set, wins over AuthToken and is minted per call.
	BearerToken speechkit.BearerTokenFunc
	// UseMaxCompletionTokens sends max_completion_tokens instead of max_tokens.
	UseMaxCompletionTokens bool
	// OmitTemperature drops the temperature: reasoning models (GPT-5 family,
	// o-series, MAI-Thinking) accept only their default and reject the
	// request otherwise (verified live on Foundry, 2026-09-05).
	OmitTemperature bool
	ExtraHeaders    map[string]string
}

// reasoningModelFamily reports whether a deployment or model name belongs to
// a family that rejects max_tokens and non-default temperature.
func reasoningModelFamily(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, prefix := range []string{"gpt-5", "o1", "o3", "o4", "mai-thinking"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
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

func callOpenAICompatible(ctx context.Context, client *http.Client, baseURL, authToken, model string, input Request) (Response, error) {
	return callOpenAICompatibleWithOptions(ctx, client, baseURL, model, input, AICallValidation, oaiCallOptions{AuthToken: authToken})
}

func callOpenAICompatibleWithOptions(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	model string,
	input Request,
	validation netsec.ValidationOptions,
	opts oaiCallOptions,
) (Response, error) {
	reqBody := oaiRequest{
		Model:    model,
		Messages: promptMessages(input),
	}
	if input.MaxTokens > 0 {
		if opts.UseMaxCompletionTokens {
			reqBody.MaxCompletionTokens = input.MaxTokens
		} else {
			reqBody.MaxTokens = input.MaxTokens
		}
	}
	if input.Temperature != nil && *input.Temperature > 0 && !opts.OmitTemperature {
		reqBody.Temperature = input.Temperature
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	endpoint, err := netsec.BuildEndpoint(baseURL, chatCompletions, validation)
	if err != nil {
		return Response{}, fmt.Errorf("%s endpoint: %w", model, err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	authToken := opts.AuthToken
	if opts.BearerToken != nil {
		minted, err := opts.BearerToken(ctx)
		if err != nil {
			return Response{}, fmt.Errorf("%s: bearer token: %w", model, err)
		}
		authToken = minted
	}
	if token := strings.TrimSpace(authToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range opts.ExtraHeaders {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	if client == nil {
		client = newAIClient(&validation)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("%s request: %w", model, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return Response{}, netsec.ProviderStatusError(model, resp.StatusCode, body)
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return Response{}, fmt.Errorf("parse response: %w", err)
	}

	if len(oaiResp.Choices) == 0 {
		var wrapped struct {
			Result oaiResponse `json:"result"`
		}
		if err := json.Unmarshal(body, &wrapped); err == nil {
			oaiResp = wrapped.Result
		}
	}
	if len(oaiResp.Choices) == 0 {
		return Response{}, fmt.Errorf("%s: no choices in response", model)
	}

	out := Response{
		Text:         oaiResp.Choices[0].Message.Content,
		FinishReason: oaiResp.Choices[0].FinishReason,
	}
	if oaiResp.Usage.TotalTokens > 0 {
		out.Usage = &generation.Usage{TotalTokens: oaiResp.Usage.TotalTokens}
	}
	return out, nil
}

func promptMessages(input Request) []oaiMessage {
	messages := make([]oaiMessage, 0, 2+len(input.Messages))
	if system := strings.TrimSpace(input.System); system != "" {
		messages = append(messages, oaiMessage{Role: "system", Content: system})
	}
	for _, message := range input.Messages {
		role := message.Role
		if role == "model" {
			role = "assistant"
		}
		if strings.TrimSpace(message.Content) == "" || role == "system" {
			continue
		}
		messages = append(messages, oaiMessage{Role: role, Content: message.Content})
	}
	if prompt := strings.TrimSpace(input.Prompt); prompt != "" {
		messages = append(messages, oaiMessage{Role: "user", Content: prompt})
	}
	return messages
}

type ollamaModel struct {
	baseURL string
	name    string
	client  *http.Client
}

func (m *ollamaModel) ID() string { return "ollama/" + m.name }

func (m *ollamaModel) Generate(ctx context.Context, input Request) (Response, error) {
	body, err := json.Marshal(struct {
		Model    string       `json:"model"`
		Messages []oaiMessage `json:"messages"`
		Stream   bool         `json:"stream"`
	}{Model: m.name, Messages: promptMessages(input), Stream: false})
	if err != nil {
		return Response{}, fmt.Errorf("marshal %s request: %w", m.ID(), err)
	}
	endpoint, err := netsec.BuildEndpoint(m.baseURL, "api/chat", localLLMCallValidation)
	if err != nil {
		return Response{}, fmt.Errorf("%s endpoint: %w", m.ID(), err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("create %s request: %w", m.ID(), err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := m.client
	if client == nil {
		client = newLocalAIClient(&localLLMCallValidation)
	}
	response, err := client.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("%s request: %w", m.ID(), err)
	}
	defer response.Body.Close() //nolint:errcheck
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxRespBody))
	if err != nil {
		return Response{}, fmt.Errorf("read %s response: %w", m.ID(), err)
	}
	if response.StatusCode != http.StatusOK {
		return Response{}, netsec.ProviderStatusError(m.ID(), response.StatusCode, payload)
	}
	var decoded struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return Response{}, fmt.Errorf("parse %s response: %w", m.ID(), err)
	}
	return Response{Text: decoded.Message.Content}, nil
}
