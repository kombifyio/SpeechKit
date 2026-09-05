package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/firebase/genkit/go/ai"
)

func TestFoundryModelTargetSplitsMAIFromOpenAIDeployments(t *testing.T) {
	reg := foundryRegistration{
		APIKey:     "k",
		BaseURL:    "https://example.services.ai.azure.com/openai/v1",
		MAIBaseURL: "https://example.services.ai.azure.com/mai/v1",
	}
	base, opts, ok := foundryModelTarget(reg, "gpt-5.6-terra")
	if !ok || base != reg.BaseURL || !opts.UseMaxCompletionTokens || !opts.OmitTemperature {
		t.Fatalf("gpt-5.6 deployment: base=%q ok=%v opts=%+v", base, ok, opts)
	}
	base, opts, ok = foundryModelTarget(reg, "gpt-4o")
	if !ok || base != reg.BaseURL || !opts.UseMaxCompletionTokens || opts.OmitTemperature {
		t.Fatalf("gpt-4o deployment keeps its temperature: base=%q ok=%v opts=%+v", base, ok, opts)
	}
	base, opts, ok = foundryModelTarget(reg, "MAI-Thinking-1")
	if !ok || base != reg.MAIBaseURL || !opts.UseMaxCompletionTokens || !opts.OmitTemperature {
		t.Fatalf("mai deployment: base=%q ok=%v opts=%+v", base, ok, opts)
	}
	reg.MAIBaseURL = ""
	if _, _, ok := foundryModelTarget(reg, "mai-thinking-1"); ok {
		t.Fatal("MAI deployment must be skipped without the /mai/v1 base")
	}
}

// MAI-Thinking-1 rejects max_tokens; the call must spell the cap as
// max_completion_tokens and send the minted token instead of the key.
func TestCallOpenAICompatibleWithOptions_MAIShapeAndBearer(t *testing.T) {
	var gotAuth string
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"}},
		})
	}))
	defer server.Close()

	mr := &ai.ModelRequest{
		Messages: []*ai.Message{{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("q")}}},
		Config:   &ai.GenerationCommonConfig{MaxOutputTokens: 321},
	}
	mr.Config = &ai.GenerationCommonConfig{MaxOutputTokens: 321, Temperature: 0.4}
	opts := oaiCallOptions{
		AuthToken:              "static-key",
		BearerToken:            func(ctx context.Context) (string, error) { return "minted-token", nil },
		UseMaxCompletionTokens: true,
		OmitTemperature:        true,
	}
	if _, err := callOpenAICompatibleWithOptions(context.Background(), testClient(), server.URL, "MAI-Thinking-1", mr, AICallValidation, opts); err != nil {
		t.Fatalf("call: %v", err)
	}
	if gotAuth != "Bearer minted-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if _, present := got["max_tokens"]; present {
		t.Fatalf("max_tokens must not be sent for MAI models: %v", got)
	}
	if v, _ := got["max_completion_tokens"].(float64); v != 321 {
		t.Fatalf("max_completion_tokens = %v, want 321", got["max_completion_tokens"])
	}
	if _, present := got["temperature"]; present {
		t.Fatalf("temperature must be dropped for reasoning models: %v", got)
	}
}

func TestReasoningModelFamily(t *testing.T) {
	for name, want := range map[string]bool{
		"gpt-5.6-terra": true, "GPT-5.4-mini": true, "o4-mini": true, "MAI-Thinking-1": true,
		"gpt-4o": false, "gpt-4.1": false, "gpt-realtime-2": false, "gemma": false,
	} {
		if got := reasoningModelFamily(name); got != want {
			t.Fatalf("reasoningModelFamily(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFoundryModelEnabledAcceptsTokenSourceWithoutKey(t *testing.T) {
	cfg := Config{FoundryBaseURL: "https://example.services.ai.azure.com/openai/v1"}
	if foundryModelEnabled(cfg) {
		t.Fatal("no credential must disable Foundry models")
	}
	cfg.FoundryBearerToken = func(ctx context.Context) (string, error) { return "t", nil }
	if !foundryModelEnabled(cfg) {
		t.Fatal("a token source must enable Foundry models")
	}
}
