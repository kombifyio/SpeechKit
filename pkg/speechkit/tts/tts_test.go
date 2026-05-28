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
	opts   *[]SynthesizeOpts
	called bool
}

func (p *testProvider) Synthesize(_ context.Context, _ string, opts SynthesizeOpts) (*Result, error) {
	p.called = true
	if p.calls != nil {
		*p.calls = append(*p.calls, p.name)
	}
	if p.opts != nil {
		*p.opts = append(*p.opts, opts)
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

func TestServiceMergesDefaultAndRequestOptions(t *testing.T) {
	var got []SynthesizeOpts
	provider := &testProvider{name: "piper", kind: ProviderKindLocalBuiltIn, opts: &got}
	service, err := NewService(
		NewRouter(StrategyLocalOnly, provider),
		WithDefaultOpts(SynthesizeOpts{
			Locale: "en-US",
			Voice:  "default",
			Speed:  1.1,
			Format: "wav",
		}),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.Synthesize(context.Background(), "hello", SynthesizeOpts{Voice: "custom"}); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(got))
	}
	if got[0].Locale != "en-US" || got[0].Voice != "custom" || got[0].Speed != 1.1 || got[0].Format != "wav" {
		t.Fatalf("merged opts = %+v", got[0])
	}
	if service.Router() == nil {
		t.Fatal("Router should expose the configured router")
	}
}

func TestPreferredProviderForProfileID(t *testing.T) {
	tests := map[string]string{
		"tts.openai.gpt-4o-mini-tts": "openai",
		"tts.google.chirp":           "google",
		"tts.huggingface.bark":       "huggingface",
		"tts.openedai.kokoro":        "kokoro",
		"tts.local.piper":            "piper",
		"tts.local.piper-de":         "piper",
		"tts.local.supertonic":       "supertonic_local",
		"unknown":                    "",
		"":                           "",
	}
	for profileID, want := range tests {
		t.Run(profileID, func(t *testing.T) {
			if got := PreferredProviderForProfileID(profileID); got != want {
				t.Fatalf("PreferredProviderForProfileID(%q) = %q, want %q", profileID, got, want)
			}
		})
	}
}
