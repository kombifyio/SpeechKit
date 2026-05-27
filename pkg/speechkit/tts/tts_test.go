package tts

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/sdkparity"
)

type testProvider struct {
	name   string
	kind   ProviderKind
	err    error
	calls  *[]string
	called bool
}

func (p *testProvider) Synthesize(context.Context, string, SynthesizeOpts) (*Result, error) {
	p.called = true
	if p.calls != nil {
		*p.calls = append(*p.calls, p.name)
	}
	if p.err != nil {
		return nil, p.err
	}
	return &Result{Provider: p.name, Format: "pcm"}, nil
}

func (p *testProvider) Name() string                 { return p.name }
func (p *testProvider) Kind() ProviderKind           { return p.kind }
func (p *testProvider) Health(context.Context) error { return nil }

func TestRouterParity(t *testing.T) {
	sdkparity.RunTTSRouterParity(t, sdkparity.TTSRouterHarness{
		Synthesize: func(ctx context.Context, strategy sdkparity.TTSStrategy, specs []sdkparity.TTSProviderSpec) (sdkparity.TTSRouterResult, error) {
			calls := make([]string, 0, len(specs))
			providers := make([]Provider, 0, len(specs))
			for _, spec := range specs {
				providers = append(providers, &testProvider{
					name:  spec.Name,
					kind:  ProviderKind(spec.Kind),
					err:   spec.Err,
					calls: &calls,
				})
			}
			result, err := NewRouter(Strategy(strategy), providers...).Synthesize(ctx, "hello", SynthesizeOpts{})
			if err != nil {
				return sdkparity.TTSRouterResult{Calls: calls}, err
			}
			return sdkparity.TTSRouterResult{Provider: result.Provider, Calls: calls}, nil
		},
	})
}

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
