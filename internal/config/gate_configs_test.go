package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKombifyProdConfigDoesNotSelectGoogle is Kombify's own deployment
// policy: Google is an opt-in BYOK provider for SpeechKit users, but the
// Kombify production image never enables it or routes the Voice Agent to
// Gemini Live (Kombify does not run on Google AI / GCP).
func TestKombifyProdConfigDoesNotSelectGoogle(t *testing.T) {
	cfg := loadPrivateGateConfig(t, filepath.Join("..", "..", "deploy", "config", "server.kombify-prod.toml"))
	if cfg.Providers.Google.Enabled {
		t.Error("Kombify production must not enable the Google provider")
	}
	if cfg.TTS.Google.Enabled {
		t.Error("Kombify production must not enable Google TTS")
	}
	if got := EffectiveVoiceAgentProvider(cfg); got == "gemini" {
		t.Fatalf("Kombify production voice agent provider resolved to Gemini Live from %q", cfg.VoiceAgent.Provider)
	}
	if cfg.VoiceAgent.Provider != "deepgram" {
		t.Errorf("production voice_agent.provider = %q, want deepgram", cfg.VoiceAgent.Provider)
	}
	if !cfg.Providers.Deepgram.Enabled || !cfg.Providers.AssemblyAI.Enabled {
		t.Errorf("production must keep deepgram+assemblyai: dg=%v aai=%v",
			cfg.Providers.Deepgram.Enabled, cfg.Providers.AssemblyAI.Enabled)
	}
}

func loadPrivateGateConfig(t *testing.T, path string) *Config {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("%s is private deployment testdata and is not part of the OSS export", path)
		}
		t.Fatalf("stat %s: %v", path, err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return cfg
}
