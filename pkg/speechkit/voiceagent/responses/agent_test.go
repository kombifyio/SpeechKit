package responses_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/responses"
)

// The HTTP effect must retain this own endpoint and the same local conversation
// after stopping voice, while a consumed native turn cannot dispatch twice.
func TestOwnEndpointConversationSurvivesStopWithoutReplay(t *testing.T) {
	connection := responses.Connection{Mode: "endpoint", EndpointID: "owner-endpoint", Model: "own-model"}
	original := []responses.Message{{Role: "user", Content: "Original typed question"}, {Role: "assistant", Content: "Original typed answer"}}
	var observed [][]responses.Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Connection     responses.Connection
			Messages       []responses.Message
			IdempotencyKey string
			Surface        string
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Connection != connection || body.Surface != "direct-chat" ||
			body.IdempotencyKey != r.Header.Get("Idempotency-Key") || r.Header.Get("Authorization") != "Bearer session-scoped" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		observed = append(observed, body.Messages)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"text\",\"content\":\"Own endpoint answer\"}\n\n")
		w.(http.Flusher).Flush()
		if body.IdempotencyKey == "observed-stop" {
			<-r.Context().Done()
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"type\":\"done\"}\n\n")
	}))
	defer server.Close()
	admissions := 0
	agent, err := responses.New(responses.Config{URL: server.URL, Connection: connection, Messages: original, Surface: "direct-chat",
		Headers: func(_ context.Context, body []byte) (http.Header, error) {
			var binding struct{ Connection responses.Connection }
			if json.Unmarshal(body, &binding) != nil || binding.Connection != connection {
				return nil, fmt.Errorf("binding changed")
			}
			admissions++
			return http.Header{"Authorization": {"Bearer session-scoped"}}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	output, err := agent.StreamTurn(ctx, cascaded.AgentInput{Utterance: "Voice question", SystemPrompt: "Vendor replacement model instructions"}, "observed-stop", func(string) error { cancel(); return ctx.Err() })
	if err == nil || output.Text == "" {
		t.Fatalf("stop did not preserve the partial answer: %v", err)
	}
	before := len(observed)
	if _, err := agent.StreamTurn(context.Background(), cascaded.AgentInput{Utterance: "Voice question"}, "observed-stop", nil); err == nil || len(observed) != before {
		t.Fatal("consumed turn dispatched again")
	}
	if _, err := agent.StreamTurn(context.Background(), cascaded.AgentInput{Utterance: "Continue typed context"}, "observed-next", nil); err != nil {
		t.Fatal(err)
	}
	want := append(append([]responses.Message(nil), original...), responses.Message{Role: "user", Content: "Voice question"}, responses.Message{Role: "assistant", Content: output.Text}, responses.Message{Role: "user", Content: "Continue typed context"})
	if admissions != 2 || !reflect.DeepEqual(observed[len(observed)-1], want) {
		t.Fatal("the admitted endpoint lost or substituted the stopped local conversation")
	}
}
