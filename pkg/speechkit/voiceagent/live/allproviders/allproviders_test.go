package allproviders

import (
	"context"
	"errors"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"testing"
)

func TestNormalizeProviderIDAliases(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"gemini":                                "google",
		"realtime.google.gemini-native-audio":   "google",
		"realtime.google.gemini-live-translate": "google",
		"deepgram-agent":                        "deepgram",
		"realtime.deepgram.voice-agent":         "deepgram",
		"assembly-ai":                           "assemblyai",
		"realtime.assemblyai.voice-agent":       "assemblyai",
		"openai-realtime":                       "openai",
		"realtime.openai.gpt-realtime-2":        "openai",
	}
	for in, want := range cases {
		if got := live.NormalizeProviderID(in); got != want {
			t.Fatalf("live.NormalizeProviderID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultLiveConfigForProvider(t *testing.T) {
	t.Parallel()
	cfg, ok := live.DefaultLiveConfigForProvider("realtime.assemblyai.voice-agent")
	if !ok {
		t.Fatal("assemblyai profile should resolve")
	}
	if cfg.Provider != "assemblyai" || cfg.ProfileID != "realtime.assemblyai.voice-agent" || cfg.Model != "assemblyai-voice-agent" {
		t.Fatalf("default config = %+v", cfg)
	}
}

func TestNormalizeLiveConfigFillsProviderProfileAndModel(t *testing.T) {
	t.Parallel()
	cfg, err := NormalizeLiveConfig(live.LiveConfig{ProfileID: "realtime.deepgram.voice-agent"})
	if err != nil {
		t.Fatalf("NormalizeLiveConfig: %v", err)
	}
	if cfg.Provider != "deepgram" || cfg.ProfileID != "realtime.deepgram.voice-agent" || cfg.Model != "flux-general-multi" {
		t.Fatalf("normalized deepgram config = %+v", cfg)
	}

	cfg, err = NormalizeLiveConfig(live.LiveConfig{Model: "gemini-3.1-flash-live-preview"})
	if err != nil {
		t.Fatalf("NormalizeLiveConfig by model: %v", err)
	}
	if cfg.Provider != "google" || cfg.ProfileID != "realtime.google.gemini-native-audio" || cfg.Model != "gemini-3.1-flash-live-preview" {
		t.Fatalf("normalized gemini config = %+v", cfg)
	}

	cfg, err = NormalizeLiveConfig(live.LiveConfig{ProfileID: "realtime.google.gemini-live-translate"})
	if err != nil {
		t.Fatalf("NormalizeLiveConfig translate profile: %v", err)
	}
	if cfg.Provider != "google" || cfg.ProfileID != "realtime.google.gemini-live-translate" || cfg.Model != "gemini-3.5-live-translate-preview" {
		t.Fatalf("normalized gemini translate config = %+v", cfg)
	}
}

func TestNewProviderResolvesBuiltInsAndCustomFactories(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"gemini":                          "gemini-live",
		"deepgram":                        "deepgram-agent",
		"realtime.assemblyai.voice-agent": "assemblyai-agent",
		"realtime.openai.gpt-realtime-2":  "openai-realtime",
	}
	for selector, wantName := range cases {
		provider, err := NewProvider(selector)
		if err != nil {
			t.Fatalf("NewProvider(%q): %v", selector, err)
		}
		if provider.Name() != wantName {
			t.Fatalf("NewProvider(%q).Name() = %q, want %q", selector, provider.Name(), wantName)
		}
	}

	provider, err := NewProviderWithFactories("acme", ProviderFactoryRegistry{
		"acme": func() live.LiveProvider { return &factoryTestProvider{} },
	})
	if err != nil {
		t.Fatalf("custom NewProviderWithFactories: %v", err)
	}
	if provider.Name() != "scripted" {
		t.Fatalf("custom provider name = %q", provider.Name())
	}
}

type factoryTestProvider struct{}

func (p *factoryTestProvider) Connect(context.Context, live.LiveConfig) error { return nil }

func (p *factoryTestProvider) SendAudio([]byte) error { return nil }

func (p *factoryTestProvider) SendAudioStreamEnd() error { return nil }

func (p *factoryTestProvider) Receive(context.Context) (*live.LiveMessage, error) {
	return &live.LiveMessage{Done: true}, nil
}

func (p *factoryTestProvider) SendText(string) error { return nil }

func (p *factoryTestProvider) SendToolResponse(live.ToolResponse) error { return nil }

func (p *factoryTestProvider) Close() error { return nil }

func (p *factoryTestProvider) Name() string { return "scripted" }

func TestNewProviderForConfigNormalizesConfig(t *testing.T) {
	t.Parallel()
	provider, cfg, err := NewProviderForConfig(live.LiveConfig{Provider: "openai-realtime"})
	if err != nil {
		t.Fatalf("NewProviderForConfig: %v", err)
	}
	if provider.Name() != "openai-realtime" {
		t.Fatalf("provider = %q", provider.Name())
	}
	if cfg.Provider != "openai" || cfg.ProfileID != "realtime.openai.gpt-realtime-2" || cfg.Model != "gpt-realtime-2" {
		t.Fatalf("normalized config = %+v", cfg)
	}
}

func TestNewProviderUnknownProvider(t *testing.T) {
	t.Parallel()
	if _, err := NewProvider("missing"); !errors.Is(err, ErrUnknownLiveProvider) {
		t.Fatalf("NewProvider unknown err = %v", err)
	}
	if _, err := NormalizeLiveConfig(live.LiveConfig{}); !errors.Is(err, ErrUnknownLiveProvider) {
		t.Fatalf("NormalizeLiveConfig empty err = %v", err)
	}
}
