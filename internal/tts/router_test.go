package tts

import (
	"context"
	"fmt"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/models"
)

func TestOrderByPreferredProvider_MovesMatchToFront(t *testing.T) {
	openai := &mockProvider{name: "openai"}
	google := &mockProvider{name: "google"}
	hf := &mockProvider{name: "huggingface"}

	ordered := OrderByPreferredProvider([]Provider{openai, google, hf}, "google")
	if len(ordered) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(ordered))
	}
	if ordered[0].Name() != "google" {
		t.Errorf("ordered[0] = %q, want google", ordered[0].Name())
	}
	// Original relative order of the remaining providers is preserved.
	if ordered[1].Name() != "openai" || ordered[2].Name() != "huggingface" {
		t.Errorf("relative order broken: %v", []string{ordered[1].Name(), ordered[2].Name()})
	}
}

func TestOrderByPreferredProvider_EmptyPreferredIsNoOp(t *testing.T) {
	openai := &mockProvider{name: "openai"}
	google := &mockProvider{name: "google"}
	in := []Provider{openai, google}
	out := OrderByPreferredProvider(in, "")
	if len(out) != 2 || out[0].Name() != "openai" {
		t.Errorf("empty preferred should be no-op, got %v", out)
	}
}

func TestOrderByPreferredProvider_NoMatchReturnsInputUnchanged(t *testing.T) {
	openai := &mockProvider{name: "openai"}
	in := []Provider{openai}
	out := OrderByPreferredProvider(in, "kokoro")
	if len(out) != 1 || out[0].Name() != "openai" {
		t.Errorf("no-match should return input, got %v", out)
	}
}

func TestPreferredProviderForProfileID_MapsCatalogEntries(t *testing.T) {
	cases := map[string]string{
		"":                                      "",
		"tts.openai.tts-1-hd":                   "openai",
		"tts.google.studio-o-de":                "google",
		"tts.google.studio-o-de.variant.studio": "google", // prefix match
		"tts.huggingface.parler-multilingual":   "huggingface",
		"tts.openedai.kokoro":                   "kokoro",
		"tts.kokoro.82m":                        "kokoro",
		// v0.37.3 Local Built-in profiles — distinct provider Name()s
		// per runtime adapter (kokoro_local vs supertonic_local vs
		// chatterbox_local vs piper subprocess).
		"tts.local.kokoro-82m":              "kokoro_local",
		"tts.local.kokoro-v1":               "kokoro_local",
		"tts.local.supertonic-3":            "supertonic_local",
		"tts.local.supertonic-3.variant.de": "supertonic_local",
		"tts.local.chatterbox-multilingual": "chatterbox_local",
		"tts.local.chatterbox-clone":        "chatterbox_local",
		"tts.local.piper":                   "piper",
		"tts.local.piper-de":                "piper",
		"tts.local.piper-en":                "piper",
		"stt.openai.whisper-1":              "", // wrong mode prefix
		"random-string":                     "",
	}
	for profile, want := range cases {
		got := PreferredProviderForProfileID(profile)
		if got != want {
			t.Errorf("PreferredProviderForProfileID(%q) = %q, want %q", profile, got, want)
		}
	}
}

// mockProvider is a test double for Provider.
type mockProvider struct {
	name   string
	kind   models.ProviderKind
	err    error
	result *Result
	called bool
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

func (m *mockProvider) Name() string                   { return m.name }
func (m *mockProvider) Kind() models.ProviderKind      { return m.kind }
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
	local := &mockProvider{name: "piper", kind: models.ProviderKindLocalBuiltIn}
	cloud := &mockProvider{name: "openai", kind: models.ProviderKindDirectProvider}

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
	local := &mockProvider{name: "piper", kind: models.ProviderKindLocalBuiltIn}
	cloud := &mockProvider{name: "openai", kind: models.ProviderKindDirectProvider}

	r := NewRouter(StrategyLocalOnly, local, cloud)
	result, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cloud.called {
		t.Error("cloud provider should be skipped in local-only mode")
	}
	if result.Provider != "piper" {
		t.Errorf("expected piper, got %s", result.Provider)
	}
}

func TestRouterLocalOnlyAllowsLocalProviderKind(t *testing.T) {
	local := &mockProvider{name: "openedai-kokoro", kind: models.ProviderKindLocalProvider}
	cloud := &mockProvider{name: "huggingface", kind: models.ProviderKindCloudProvider}

	r := NewRouter(StrategyLocalOnly, cloud, local)
	result, err := r.Synthesize(context.Background(), "test", SynthesizeOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cloud.called {
		t.Error("cloud provider should be skipped in local-only mode")
	}
	if result.Provider != "openedai-kokoro" {
		t.Errorf("expected openedai-kokoro, got %s", result.Provider)
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
