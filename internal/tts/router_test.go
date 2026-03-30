package tts

import (
	"context"
	"fmt"
	"testing"
)

// mockProvider is a test double for Provider.
type mockProvider struct {
	name    string
	err     error
	result  *Result
	called  bool
}

func (m *mockProvider) Synthesize(_ context.Context, text string, _ SynthesizeOpts) (*Result, error) {
	m.called = true
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &Result{
		Audio:    []byte("audio-from-" + m.name),
		Format:   "mp3",
		Provider: m.name,
	}, nil
}

func (m *mockProvider) Name() string               { return m.name }
func (m *mockProvider) Health(_ context.Context) error { return m.err }

func TestRouterFallback(t *testing.T) {
	failing := &mockProvider{name: "openai", err: fmt.Errorf("rate limited")}
	working := &mockProvider{name: "google"}

	r := NewRouter(StrategyCloudFirst, failing, working)

	result, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !failing.called {
		t.Error("expected failing provider to be called")
	}
	if !working.called {
		t.Error("expected working provider to be called")
	}
	if result.Provider != "google" {
		t.Errorf("expected google, got %s", result.Provider)
	}
}

func TestRouterNoProviders(t *testing.T) {
	r := NewRouter(StrategyCloudFirst)
	_, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error with no providers")
	}
}

func TestRouterAllFail(t *testing.T) {
	p1 := &mockProvider{name: "openai", err: fmt.Errorf("fail1")}
	p2 := &mockProvider{name: "google", err: fmt.Errorf("fail2")}

	r := NewRouter(StrategyCloudFirst, p1, p2)
	_, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err == nil {
		t.Fatal("expected error when all providers fail")
	}
}

func TestRouterCloudOnly(t *testing.T) {
	local := &mockProvider{name: "kokoro"}
	cloud := &mockProvider{name: "openai"}

	r := NewRouter(StrategyCloudOnly, local, cloud)
	result, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if local.called {
		t.Error("local provider should be skipped in cloud-only mode")
	}
	if result.Provider != "openai" {
		t.Errorf("expected openai, got %s", result.Provider)
	}
}

func TestRouterLocalOnly(t *testing.T) {
	local := &mockProvider{name: "kokoro"}
	cloud := &mockProvider{name: "openai"}

	r := NewRouter(StrategyLocalOnly, local, cloud)
	result, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cloud.called {
		t.Error("cloud provider should be skipped in local-only mode")
	}
	if result.Provider != "kokoro" {
		t.Errorf("expected kokoro, got %s", result.Provider)
	}
}

func TestRouterHealthCheck(t *testing.T) {
	healthy := &mockProvider{name: "openai"}
	unhealthy := &mockProvider{name: "google", err: fmt.Errorf("no key")}

	r := NewRouter(StrategyCloudFirst, healthy, unhealthy)
	results := r.HealthCheck(context.Background())

	if results["openai"] != nil {
		t.Errorf("openai should be healthy")
	}
	if results["google"] == nil {
		t.Errorf("google should be unhealthy")
	}
}
