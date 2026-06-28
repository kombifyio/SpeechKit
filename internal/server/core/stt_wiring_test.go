//go:build linux

package core

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

func TestBuildSTTRouterDoesNotUseGoogleAIKeyForCloudSTT(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "gemini-key")

	cfg := &config.Config{}
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTModel = "latest_long"

	_, providers, notes := buildSTTRouter(cfg)

	if hasProvider(providers, "stt.google") {
		t.Fatal("Google STT should not register with GOOGLE_AI_API_KEY alone")
	}
	if !hasNote(notes, "Google STT disabled") {
		t.Fatalf("notes = %v, want disabled note", notes)
	}
}

func TestBuildSTTRouterUsesDedicatedGoogleSTTKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "gemini-key")
	t.Setenv("SPEECHKIT_GOOGLE_STT_API_KEY", "speech-key")

	cfg := &config.Config{}
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTModel = "latest_long"

	_, providers, notes := buildSTTRouter(cfg)

	if !hasProvider(providers, "stt.google") {
		t.Fatal("Google STT should register when a dedicated STT key is set")
	}
	if !hasNote(notes, "source=SPEECHKIT_GOOGLE_STT_API_KEY") {
		t.Fatalf("notes = %v, want dedicated key source", notes)
	}
}

func TestBuildSTTRouterUsesConfiguredGoogleSTTKeyEnv(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "gemini-key")
	t.Setenv("CUSTOM_GOOGLE_STT_KEY", "speech-key")

	cfg := &config.Config{}
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTAPIKeyEnv = "CUSTOM_GOOGLE_STT_KEY"
	cfg.Providers.Google.STTModel = "latest_long"

	_, providers, notes := buildSTTRouter(cfg)

	if !hasProvider(providers, "stt.google") {
		t.Fatal("Google STT should register when the configured STT key env is set")
	}
	if !hasNote(notes, "source=CUSTOM_GOOGLE_STT_KEY") {
		t.Fatalf("notes = %v, want configured STT key source", notes)
	}
}

func TestBuildSTTRouterRegistersDeepgramWhenKeyIsSet(t *testing.T) {
	t.Setenv("DEEPGRAM_API_KEY", "deepgram-key")

	cfg := &config.Config{}
	cfg.Providers.Deepgram.Enabled = true
	cfg.Providers.Deepgram.APIKeyEnv = "DEEPGRAM_API_KEY"
	cfg.Providers.Deepgram.STTModel = "nova-3"
	cfg.Providers.Deepgram.DiarizationModel = "latest"

	_, providers, notes := buildSTTRouter(cfg)

	if !hasProvider(providers, "stt.deepgram") {
		t.Fatal("Deepgram STT should register when the key is set")
	}
	if !hasNote(notes, "source=DEEPGRAM_API_KEY") {
		t.Fatalf("notes = %v, want Deepgram key source", notes)
	}
}

func TestBuildSTTRouterRegistersAssemblyAIWhenKeyIsSet(t *testing.T) {
	t.Setenv("ASSEMBLYAI_API_KEY", "assembly-key")

	cfg := &config.Config{}
	cfg.Providers.AssemblyAI.Enabled = true
	cfg.Providers.AssemblyAI.APIKeyEnv = "ASSEMBLYAI_API_KEY"
	cfg.Providers.AssemblyAI.STTModels = "universal-3-pro,universal-2"
	cfg.Providers.AssemblyAI.StreamingModel = "universal-3-5-pro"
	cfg.Providers.AssemblyAI.StreamingBaseURL = "wss://eu.streaming.assemblyai.com"

	_, providers, notes := buildSTTRouter(cfg)

	if !hasProvider(providers, "stt.assemblyai") {
		t.Fatal("AssemblyAI STT should register when the key is set")
	}
	if !hasNote(notes, "source=ASSEMBLYAI_API_KEY") {
		t.Fatalf("notes = %v, want AssemblyAI key source", notes)
	}
	provider := findProvider[stt.AssemblyAIProvider](providers, "stt.assemblyai")
	if provider == nil {
		t.Fatalf("providers = %+v, want AssemblyAIProvider", providers)
	}
	if provider.StreamingModel != "universal-3-5-pro" || provider.StreamingBaseURL != "wss://eu.streaming.assemblyai.com" {
		t.Fatalf("AssemblyAI streaming config = %q/%q", provider.StreamingModel, provider.StreamingBaseURL)
	}
}

func TestBuildSTTRouterUsesConfiguredOpenAIModel(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "openai-key")

	cfg := &config.Config{}
	cfg.Providers.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "OPENAI_API_KEY"
	cfg.Providers.OpenAI.STTModel = "gpt-4o-transcribe"

	_, providers, notes := buildSTTRouter(cfg)

	provider := findProvider[stt.OpenAICompatibleProvider](providers, "stt.openai")
	if provider == nil {
		t.Fatalf("providers = %+v, want OpenAICompatibleProvider", providers)
	}
	if provider.Model != "gpt-4o-transcribe" {
		t.Fatalf("OpenAI model = %q, want gpt-4o-transcribe", provider.Model)
	}
	if !hasNote(notes, "OpenAI STT registered (model=gpt-4o-transcribe") {
		t.Fatalf("notes = %v, want OpenAI model note", notes)
	}
}

func TestBuildSTTRouterUsesConfiguredGroqModel(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "groq-key")

	cfg := &config.Config{}
	cfg.Providers.Groq.Enabled = true
	cfg.Providers.Groq.APIKeyEnv = "GROQ_API_KEY"
	cfg.Providers.Groq.STTModel = "whisper-large-v3"

	_, providers, notes := buildSTTRouter(cfg)

	provider := findProvider[stt.OpenAICompatibleProvider](providers, "stt.groq")
	if provider == nil {
		t.Fatalf("providers = %+v, want OpenAICompatibleProvider", providers)
	}
	if provider.Model != "whisper-large-v3" {
		t.Fatalf("Groq model = %q, want whisper-large-v3", provider.Model)
	}
	if !hasNote(notes, "Groq STT registered (model=whisper-large-v3") {
		t.Fatalf("notes = %v, want Groq model note", notes)
	}
}

func TestUpdateSTTAggregateIgnoresDisabledProviders(t *testing.T) {
	app := &App{Health: NewHealthRegistry()}
	app.Health.SetReadyWithOptions("stt.disabled", StatusDisabled, "configured off", sttProviderOptions(namedProvider{name: "stt.disabled"}))
	app.Health.SetReadyWithOptions(sttAggregateComponent, StatusStarting, "probing STT providers", sttAggregateOptions())

	updateSTTAggregate(app)

	_, components, _ := app.Health.Snapshot()
	entry := components[sttAggregateComponent]
	if entry.Status != StatusUnavailable {
		t.Fatalf("aggregate status = %q, want unavailable when every provider is disabled", entry.Status)
	}
	if entry.Detail != "no STT providers configured" {
		t.Fatalf("aggregate detail = %q, want no providers detail", entry.Detail)
	}
}

func TestUpdateSTTAggregateStartsWhenAnyEnabledProviderStillProbes(t *testing.T) {
	app := &App{Health: NewHealthRegistry()}
	app.Health.SetReadyWithOptions("stt.disabled", StatusDisabled, "configured off", sttProviderOptions(namedProvider{name: "stt.disabled"}))
	app.Health.SetReadyWithOptions("stt.openai", StatusStarting, "probing", sttProviderOptions(namedProvider{name: "stt.openai"}))

	updateSTTAggregate(app)

	_, components, _ := app.Health.Snapshot()
	if got := components[sttAggregateComponent].Status; got != StatusStarting {
		t.Fatalf("aggregate status = %q, want starting", got)
	}
}

func hasProvider(providers []namedProvider, name string) bool {
	for _, provider := range providers {
		if provider.name == name {
			return true
		}
	}
	return false
}

func findProvider[T any](providers []namedProvider, name string) *T {
	for _, provider := range providers {
		if provider.name != name {
			continue
		}
		typed, ok := any(provider.provider).(*T)
		if ok {
			return typed
		}
	}
	return nil
}

func hasNote(notes []string, needle string) bool {
	for _, note := range notes {
		if strings.Contains(note, needle) {
			return true
		}
	}
	return false
}
