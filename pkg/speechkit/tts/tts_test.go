package tts

import (
	"context"
	"testing"
)

type testProvider struct {
	name   string
	kind   ProviderKind
	called bool
}

func (p *testProvider) Synthesize(context.Context, string, SynthesizeOpts) (*Result, error) {
	p.called = true
	return &Result{Provider: p.name, Format: "pcm"}, nil
}

func (p *testProvider) Name() string                 { return p.name }
func (p *testProvider) Kind() ProviderKind           { return p.kind }
func (p *testProvider) Health(context.Context) error { return nil }

func TestRouterLocalOnlyUsesProviderKind(t *testing.T) {
	cloud := &testProvider{name: "huggingface", kind: ProviderKindCloudProvider}
	local := &testProvider{name: "piper", kind: ProviderKindLocalBuiltIn}

	router := NewRouter(StrategyLocalOnly, cloud, local)
	result, err := router.Synthesize(context.Background(), "hello", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if cloud.called {
		t.Fatal("cloud provider was called in local-only mode")
	}
	if result.Provider != "piper" {
		t.Fatalf("provider = %q, want piper", result.Provider)
	}
}

func TestRouterCloudOnlySkipsLocalProviderKind(t *testing.T) {
	local := &testProvider{name: "openedai-kokoro", kind: ProviderKindLocalProvider}
	direct := &testProvider{name: "openai", kind: ProviderKindDirectProvider}

	router := NewRouter(StrategyCloudOnly, local, direct)
	result, err := router.Synthesize(context.Background(), "hello", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if local.called {
		t.Fatal("local provider was called in cloud-only mode")
	}
	if result.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", result.Provider)
	}
}
