package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

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

// registerOpenAIModels registers OpenAI models as custom Genkit models.
func registerOpenAIModels(g *genkit.Genkit, apiKey string) {
	client := newAIClient(&AICallValidation)
	models := []string{
		"gpt-5.4-mini-2026-03-17",
		"gpt-5.4-2026-03-05",
		"gpt-4o-mini",
		"gpt-4o",
		"gpt-4-turbo",
	}

	for _, name := range models {
		// The GPT-5 family on OpenAI has the same request rules as on Foundry:
		// max_completion_tokens only, default temperature only.
		if reasoningModelFamily(name) {
			registerOpenAICompatibleModelWithOptions(g, "openai", name, openAIBaseURL, client, true, AICallValidation, oaiCallOptions{
				AuthToken:              apiKey,
				UseMaxCompletionTokens: true,
				OmitTemperature:        true,
			})
			continue
		}
		registerOpenAICompatibleModel(g, "openai", name, openAIBaseURL, apiKey, client, true)
	}
}

// registerGroqModels registers Groq models as custom Genkit models.
// Groq uses an OpenAI-compatible API.
func registerGroqModels(g *genkit.Genkit, apiKey string) {
	client := newAIClient(&AICallValidation)
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
	client := newAIClient(&AICallValidation)
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

// registerAssemblyAILLMModels registers AssemblyAI LLM Gateway models.
// Chat completions are OpenAI-compatible; Qwen 3.5 4B Fast has no tool calling.
func registerAssemblyAILLMModels(g *genkit.Genkit, apiKey, baseURL string, extra []string) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = assemblyAILLMGatewayBaseURL
	}
	client := newAIClient(&AICallValidation)
	names := []string{
		"qwen3.5-4b-32k-fast",
		"qwen3-32B",
		"gemini-2.5-flash",
	}
	names = append(names, extra...)
	seen := map[string]bool{}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		supportsTools := name != "qwen3.5-4b-32k-fast"
		registerOpenAICompatibleModel(g, "assemblyai", name, baseURL, apiKey, client, supportsTools)
	}
}

func registerCloudflareAIGatewayModels(g *genkit.Genkit, apiKey, accountID, gatewayID string, extra []string) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" || strings.TrimSpace(apiKey) == "" {
		return
	}
	if strings.TrimSpace(gatewayID) == "" {
		gatewayID = "default"
	}
	baseURL := "https://api.cloudflare.com/client/v4/accounts/" + accountID + "/ai/v1"
	client := newAIClient(&AICallValidation)
	headers := map[string]string{"cf-aig-gateway-id": gatewayID}
	names := []string{
		"@cf/meta/llama-3.2-3b-instruct",
		"@cf/meta/llama-3.1-8b-instruct-fast",
	}
	names = append(names, extra...)
	seen := map[string]bool{}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		registerOpenAICompatibleModelWithHeaders(g, "cloudflare", name, baseURL, apiKey, client, false, AICallValidation, headers)
	}
}

// registerOpenRouterModels registers OpenRouter models as custom Genkit models.
// OpenRouter uses an OpenAI-compatible API with a different base URL.
func registerOpenRouterModels(g *genkit.Genkit, apiKey string) {
	client := newAIClient(&AICallValidation)
	models := []string{
		"meta-llama/llama-3.1-8b-instruct",
		"google/gemini-2.5-flash",
	}

	for _, name := range models {
		registerOpenAICompatibleModel(g, "openrouter", name, openRouterBaseURL, apiKey, client, true)
	}
}

// foundryRegistration is what registerFoundryModels needs from the host: one
// credential (the resource key or a token source), the two inference bases
// and the deployment names the tiers point at.
type foundryRegistration struct {
	APIKey      string
	BearerToken speechkit.BearerTokenFunc
	// BaseURL is https://<host>/openai/v1 for OpenAI-publisher deployments.
	BaseURL string
	// MAIBaseURL is https://<host>/mai/v1 for Microsoft-publisher
	// deployments (MAI-Thinking-1). Empty means those cannot be served.
	MAIBaseURL  string
	Deployments []string
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

// registerFoundryModels registers the configured Microsoft Foundry deployments
// as custom Genkit models. OpenAI-publisher deployments speak the
// OpenAI-compatible v1 surface; Microsoft-publisher ones (MAI-Thinking-1)
// live on /mai/v1 with the same chat-completions shape. Both accept the
// resource key or an Entra bearer token.
func registerFoundryModels(g *genkit.Genkit, reg foundryRegistration) {
	if strings.TrimSpace(reg.APIKey) == "" && reg.BearerToken == nil {
		return
	}
	client := newAIClient(&AICallValidation)
	seen := map[string]bool{}
	for _, raw := range reg.Deployments {
		name := strings.TrimSpace(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		baseURL, opts, ok := foundryModelTarget(reg, name)
		if !ok {
			continue
		}
		registerOpenAICompatibleModelWithOptions(g, "foundry", name, baseURL, client, true, AICallValidation, opts)
	}
}

// registerLocalLLMModels registers SpeechKit-managed local LLM models.
// The runtime speaks the OpenAI-compatible chat completions API on loopback.
func registerLocalLLMModels(g *genkit.Genkit, baseURL string, modelNames []string, wrapTransport func(http.RoundTripper) http.RoundTripper) {
	client := newLocalAIClient(&localLLMCallValidation)
	if wrapTransport != nil {
		next := client.Transport
		if next == nil {
			next = http.DefaultTransport
		}
		client.Transport = wrapTransport(next)
	}
	seen := map[string]bool{}
	for _, rawName := range modelNames {
		name := strings.TrimSpace(rawName)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		registerOpenAICompatibleModelWithValidation(g, "local", name, baseURL, "", client, false, localLLMCallValidation)
	}
}

// registerOpenAICompatibleModel registers a single model that speaks the OpenAI chat completions API.
func registerOpenAICompatibleModel(g *genkit.Genkit, provider, name, baseURL, authToken string, client *http.Client, supportsTools bool) {
	registerOpenAICompatibleModelWithValidation(g, provider, name, baseURL, authToken, client, supportsTools, AICallValidation)
}

func registerOpenAICompatibleModelWithValidation(
	g *genkit.Genkit,
	provider string,
	name string,
	baseURL string,
	authToken string,
	client *http.Client,
	supportsTools bool,
	validation netsec.ValidationOptions,
) {
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
			return callOpenAICompatibleWithValidation(ctx, client, baseURL, authToken, name, mr, validation, nil)
		},
	)
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

func registerOpenAICompatibleModelWithOptions(
	g *genkit.Genkit,
	provider, name, baseURL string,
	client *http.Client,
	supportsTools bool,
	validation netsec.ValidationOptions,
	opts oaiCallOptions,
) {
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
			return callOpenAICompatibleWithOptions(ctx, client, baseURL, name, mr, validation, opts)
		},
	)
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

func registerOpenAICompatibleModelWithHeaders(
	g *genkit.Genkit,
	provider, name, baseURL, authToken string,
	client *http.Client,
	supportsTools bool,
	validation netsec.ValidationOptions,
	extraHeaders map[string]string,
) {
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
			return callOpenAICompatibleWithValidation(ctx, client, baseURL, authToken, name, mr, validation, extraHeaders)
		},
	)
}

func callOpenAICompatible(ctx context.Context, client *http.Client, baseURL, authToken, model string, mr *ai.ModelRequest) (*ai.ModelResponse, error) {
	return callOpenAICompatibleWithValidation(ctx, client, baseURL, authToken, model, mr, AICallValidation, nil)
}

func callOpenAICompatibleWithValidation(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	authToken string,
	model string,
	mr *ai.ModelRequest,
	validation netsec.ValidationOptions,
	extraHeaders map[string]string,
) (*ai.ModelResponse, error) {
	return callOpenAICompatibleWithOptions(ctx, client, baseURL, model, mr, validation, oaiCallOptions{
		AuthToken:    authToken,
		ExtraHeaders: extraHeaders,
	})
}

func callOpenAICompatibleWithOptions(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	model string,
	mr *ai.ModelRequest,
	validation netsec.ValidationOptions,
	opts oaiCallOptions,
) (*ai.ModelResponse, error) {
	extraHeaders := opts.ExtraHeaders
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
			if opts.UseMaxCompletionTokens {
				reqBody.MaxCompletionTokens = cfg.MaxOutputTokens
			} else {
				reqBody.MaxTokens = cfg.MaxOutputTokens
			}
		}
		if cfg.Temperature > 0 && !opts.OmitTemperature {
			t := cfg.Temperature
			reqBody.Temperature = &t
		}
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint, err := netsec.BuildEndpoint(baseURL, chatCompletions, validation)
	if err != nil {
		return nil, fmt.Errorf("%s endpoint: %w", model, err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	authToken := opts.AuthToken
	if opts.BearerToken != nil {
		minted, err := opts.BearerToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: bearer token: %w", model, err)
		}
		authToken = minted
	}
	if token := strings.TrimSpace(authToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range extraHeaders {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

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
		return nil, netsec.ProviderStatusError(model, resp.StatusCode, body)
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
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
