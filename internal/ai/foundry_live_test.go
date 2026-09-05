package ai

// Live smoke for the Foundry chat route. Off by default; run with
// SPEECHKIT_RUN_LIVE_FOUNDRY_TEST=1, SPEECHKIT_FOUNDRY_PROJECT_ENDPOINT and
// AZURE_AI_API_KEY in the environment (nothing tenant-specific in the tree).

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/firebase/genkit/go/ai"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

func TestLiveFoundryChatCompletions(t *testing.T) {
	if os.Getenv("SPEECHKIT_RUN_LIVE_FOUNDRY_TEST") != "1" {
		t.Skip("set SPEECHKIT_RUN_LIVE_FOUNDRY_TEST=1 to run the live Foundry smoke")
	}
	endpoint := strings.TrimSpace(os.Getenv("SPEECHKIT_FOUNDRY_PROJECT_ENDPOINT"))
	key := strings.TrimSpace(os.Getenv("AZURE_AI_API_KEY"))
	if endpoint == "" || key == "" {
		t.Skip("SPEECHKIT_FOUNDRY_PROJECT_ENDPOINT and AZURE_AI_API_KEY are required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		t.Fatalf("endpoint %q: %v", endpoint, err)
	}
	base := "https://" + parsed.Host + "/openai/v1"
	deployment := strings.TrimSpace(os.Getenv("SPEECHKIT_FOUNDRY_CHAT_DEPLOYMENT"))
	if deployment == "" {
		deployment = "gpt-5.6-terra"
	}
	strict := netsec.ValidationOptions{}
	client := newAIClient(&strict)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ask := func(model string, cfg *ai.GenerationCommonConfig, opts oaiCallOptions) (string, error) {
		mr := &ai.ModelRequest{
			Messages: []*ai.Message{{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Antworte mit genau einem Wort: Pong")}}},
			Config:   cfg,
		}
		resp, err := callOpenAICompatibleWithOptions(ctx, client, base, model, mr, strict, opts)
		if err != nil {
			return "", err
		}
		return resp.Message.Content[0].Text, nil
	}

	// The flows always set a token cap and a temperature; the registration
	// options must make that acceptable to whichever family the deployment is.
	reg := foundryRegistration{APIKey: key, BaseURL: base}
	_, opts, ok := foundryModelTarget(reg, deployment)
	if !ok {
		t.Fatalf("no target for %s", deployment)
	}
	text, err := ask(deployment, &ai.GenerationCommonConfig{MaxOutputTokens: 64, Temperature: 0.3}, opts)
	if err != nil {
		t.Fatalf("%s: %v", deployment, err)
	}
	t.Logf("%s answered: %q", deployment, text)

	_, err = ask("speechkit-missing-deployment", nil, oaiCallOptions{AuthToken: key})
	if err == nil || !strings.Contains(err.Error(), "deployment not found") {
		t.Fatalf("missing deployment must be named in the error, got %v", err)
	}
	t.Logf("missing deployment reported as: %v", err)
}
