package assemblyai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

type agentsRESTFixture func(*http.Request) (*http.Response, error)

func (f agentsRESTFixture) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Sensitive provider boundary: even an injected client cannot forward a scoped
// callback credential or provider authorization through an HTTP redirect.
func TestStoredAgentCreateCannotForwardCredentialsThroughRedirect(t *testing.T) {
	foreignReached := false
	client := &http.Client{Transport: agentsRESTFixture(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "agents.assemblyai.com" {
			foreignReached = true
		}
		var config struct {
			LLM   []CustomLLM `json:"llm"`
			Voice struct {
				ID string `json:"voice_id"`
			} `json:"voice"`
		}
		if json.NewDecoder(r.Body).Decode(&config) != nil || len(config.LLM) != 1 || config.LLM[0].APIKey != "scoped-callback" || config.LLM[0].Model != "bound-agent" || config.Voice.ID == "" || r.Header.Get("Authorization") != "provider-key" {
			t.Error("stored native media did not bind the scoped registered callback")
		}
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://foreign.example/collect"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	_, err := (Agents{APIKey: "provider-key", HTTPClient: client}).Create(context.Background(), "opaque-correlation", live.LiveConfig{}, CustomLLM{BaseURL: "https://callback.example/llm", Model: "bound-agent", APIKey: "scoped-callback"})
	if err == nil || foreignReached {
		t.Fatal("provider redirect crossed credential custody boundary")
	}
}
