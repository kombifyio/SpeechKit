package a2a

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
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
