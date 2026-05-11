//go:build linux

package core

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestBuildSTTRouterDoesNotUseGoogleAIKeyForCloudSTT(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "gemini-key")

	cfg := &config.Config{}
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTModel = "chirp_3"

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
	cfg.Providers.Google.STTModel = "chirp_3"

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
	cfg.Providers.Google.STTModel = "chirp_3"

	_, providers, notes := buildSTTRouter(cfg)

	if !hasProvider(providers, "stt.google") {
		t.Fatal("Google STT should register when the configured STT key env is set")
	}
	if !hasNote(notes, "source=CUSTOM_GOOGLE_STT_KEY") {
		t.Fatalf("notes = %v, want configured STT key source", notes)
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

func hasNote(notes []string, needle string) bool {
	for _, note := range notes {
		if strings.Contains(note, needle) {
			return true
		}
	}
	return false
}
