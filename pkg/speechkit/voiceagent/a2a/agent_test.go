package a2a

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestAgentReadsOnlySuccessfulHostTaskAnswers(t *testing.T) {
	for _, state := range []string{"completed", "failed", "rejected", "canceled"} {
		t.Run(state, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := fmt.Sprintf(`{"jsonrpc":"2.0","result":{"kind":"task","status":{"state":%q,"message":{"parts":[{"kind":"text","text":"private failure detail"}]}},"artifacts":[{"name":"run_task","parts":[{"kind":"data","data":{"text":"Owned instance answered."}}]}],"metadata":{"io.kombify.error":{"code":"quota_exhausted"}}}}`, state)
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader("data: " + body + "\n\n")), Request: request}, nil
			})}
			agent, err := New(Config{Endpoint: "https://agents.example.test/a2a/instances/current-instance", TargetAgentID: "instance:current-instance", SessionID: "current-chat", HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			answer, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "Answer here"})
			if state == "completed" {
				if err != nil || answer.Text != "Owned instance answered." {
					t.Fatal("Host task answer was lost", err, answer.Text)
				}
				return
			}
			var coded cascaded.CodedError
			if !errors.As(err, &coded) || coded.Code() != "quota_exhausted" || answer.Text != "" || strings.Contains(err.Error(), "private failure detail") {
				t.Fatal("Failed task was spoken or lost its safe outcome", err, answer.Text)
			}
		})
	}
}

func TestAgentRejectsCumulativeStreamOverflow(t *testing.T) {
	var stream strings.Builder
	delta := strings.Repeat("x", 8<<10)
	for range 20 {
		fmt.Fprintf(&stream, "data: {\"result\":{\"parts\":[{\"kind\":\"text\",\"text\":%q}]}}\n\n", delta)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream.String())), Request: req}, nil
	})}
	agent, err := New(Config{Endpoint: "https://agents.example.test/a2a", TargetAgentID: "agent", SessionID: "session", HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	out, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "hello"})
	var coded cascaded.CodedError
	if !errors.As(err, &coded) || coded.Code() != "provider_failed" || out.Text != "" {
		t.Fatalf("overflow returned an answer: text bytes=%d error=%v", len(out.Text), err)
	}
}

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestAgentDenialNeverExposesUpstreamContent(t *testing.T) {
	const sensitive = "bearer_secret_private_transcript"
	for _, tc := range []struct {
		name                    string
		status                  int
		contentType, body, code string
	}{
		{"http quota", 429, "application/json", `{"error":{"code":"quota_exhausted","message":"` + sensitive + `"}}`, "quota_exhausted"},
		{"http auth", 403, "application/json", `{"error":{"code":"` + sensitive + `"}}`, "permission_denied"},
		{"json rpc", 200, "application/json", `{"error":{"data":{"code":"quota_exhausted"},"message":"` + sensitive + `"}}`, "quota_exhausted"},
		{"stream", 200, "text/event-stream", "data: {\"error\":{\"data\":{\"code\":\"" + sensitive + "\"}}}\n\n", "turn_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})}
			agent, err := New(Config{Endpoint: "https://agents.example.test/a2a", TargetAgentID: "agent", SessionID: "session", HTTPClient: client})
			if err != nil {
				t.Fatal(err)
			}
			out, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "hello"})
			var coded cascaded.CodedError
			if !errors.As(err, &coded) || coded.Code() != tc.code || strings.Contains(err.Error(), sensitive) || out.Text != "" {
				t.Fatalf("unsafe or misclassified denial: code=%v error=%v output=%v", coded, err, out)
			}
		})
	}
}

func TestAgentBoundsTurnWithCustomHTTPClient(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	agent, err := New(Config{Endpoint: "https://agents.example.test/a2a", TargetAgentID: "agent", SessionID: "session", HTTPClient: client, TurnTimeout: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Run(context.Background(), cascaded.AgentInput{Utterance: "hello"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded turn: %v", err)
	}
}

func TestAgentStreamsRegisteredA2ATurn(t *testing.T) {
	var method string
	var sessionHeader string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		method = request.Method
		sessionHeader = request.Header.Get("x-session-id")
		// The agent streams token deltas; the space at a delta's edge is part of the answer.
		var body strings.Builder
		for _, delta := range []string{"Your lab", " has three", " healthy nodes."} {
			fmt.Fprintf(&body, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":\"turn-1\",\"result\":{\"kind\":\"message\",\"role\":\"agent\",\"parts\":[{\"kind\":\"text\",\"text\":%q}]}}\n\n", delta)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body.String())),
			Request:    request,
		}, nil
	})}
	agent, err := New(Config{
		Endpoint:      "https://agents.example.test/a2a/agents/companion",
		TargetAgentID: "companion",
		SessionID:     "voice-session-7",
		HTTPClient:    client,
		Headers: func(context.Context, RequestContext) (http.Header, error) {
			return http.Header{"x-session-id": []string{"voice-session-7"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	output, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "How is my homelab?", Locale: "en"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if method != http.MethodPost || sessionHeader != "voice-session-7" {
		t.Fatalf("request did not preserve the configured A2A session identity")
	}
	if output.Text != "Your lab has three healthy nodes." || output.Action != "display" {
		t.Fatalf("Run() = %#v", output)
	}
}

func TestAgentFailsClosedWhenA2ADeniesTurn(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"capability_lease_denied"}}`)),
			Request:    request,
		}, nil
	})}
	agent, err := New(Config{
		Endpoint:      "https://agents.example.test/a2a/agents/companion",
		TargetAgentID: "companion",
		SessionID:     "voice-session-7",
		HTTPClient:    client,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "Turn on the lab"}); err == nil {
		t.Fatal("Run() error = nil, want fail-closed denial")
	}
}

// The agent's typed reason (here: used-up AI credits) must survive the turn
// failure so the client can show the matching notice (owner report
// 2026-10-02: every reply read "agent denied the streamed turn").
func TestAgentFailsClosedOnStreamedJSONRPCDenial(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := "data: {\"jsonrpc\":\"2.0\",\"id\":\"turn-1\",\"error\":{\"code\":-32603,\"message\":\"Companion run failed.\",\"data\":{\"code\":\"quota_exhausted\",\"reasonCode\":\"ai_credit_budget_reservation_exhausted\"}}}\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	agent, err := New(Config{Endpoint: "https://agents.example.test/a2a", TargetAgentID: "companion", SessionID: "session", HTTPClient: client})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = agent.Run(context.Background(), cascaded.AgentInput{Utterance: "hello"})
	var coded cascaded.CodedError
	if !errors.As(err, &coded) || coded.Code() != "quota_exhausted" {
		t.Fatalf("Run() error = %v, want the agent's typed reason quota_exhausted", err)
	}
}

func TestAgentRejectsEndpointWithoutHTTPS(t *testing.T) {
	_, err := New(Config{Endpoint: "http://agents.example.test/a2a", TargetAgentID: "companion", SessionID: "session"})
	if err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestAgentRequiresNonEmptyStreamAnswer(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("event: ping\ndata: {}\n\n")),
			Request:    request,
		}, nil
	})}
	agent, err := New(Config{Endpoint: "https://agents.example.test/a2a", TargetAgentID: "companion", SessionID: "session", HTTPClient: client})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "hello"}); err == nil {
		t.Fatal("Run() error = nil")
	}
}

func TestHeaderProviderErrorStopsBeforeNetwork(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		called = true
		return nil, fmt.Errorf("unexpected network call")
	})}
	agent, err := New(Config{
		Endpoint:      "https://agents.example.test/a2a",
		TargetAgentID: "companion",
		SessionID:     "session",
		HTTPClient:    client,
		Headers: func(context.Context, RequestContext) (http.Header, error) {
			return nil, fmt.Errorf("lease expired")
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := agent.Run(context.Background(), cascaded.AgentInput{Utterance: "hello"}); err == nil || called {
		t.Fatalf("Run() err = %v, network called = %v", err, called)
	}
}
