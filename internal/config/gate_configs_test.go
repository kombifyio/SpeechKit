package config

import (
	"path/filepath"
	"testing"
)

// TestProviderGateConfigsEnableExactlyOneSTTProvider verifies the per-provider
// functional-gate reference configs load and enable only their target STT
// provider. The provider-live-gate workflow relies on this single-provider
// determinism so `sk-e2e --expect-provider <name>` proves real routing rather
// than a silent fallback to another configured provider.
func TestProviderGateConfigsEnableExactlyOneSTTProvider(t *testing.T) {
	t.Run("deepgram", func(t *testing.T) {
		cfg, err := Load(filepath.Join("..", "..", "deploy", "config", "server.deepgram-gate.toml"))
		if err != nil {
			t.Fatalf("load deepgram gate config: %v", err)
		}
		if !cfg.Providers.Deepgram.Enabled {
			t.Error("deepgram provider must be enabled")
		}
		assertProvidersDisabled(t, "deepgram gate", map[string]bool{
			"assemblyai":  cfg.Providers.AssemblyAI.Enabled,
			"openai":      cfg.Providers.OpenAI.Enabled,
			"google":      cfg.Providers.Google.Enabled,
			"groq":        cfg.Providers.Groq.Enabled,
			"huggingface": cfg.HuggingFace.Enabled,
		})
		if cfg.Routing.Strategy != "cloud-only" {
			t.Errorf("routing.strategy = %q, want cloud-only", cfg.Routing.Strategy)
		}
		if cfg.VoiceAgent.Provider != "deepgram" {
			t.Errorf("voice_agent.provider = %q, want deepgram", cfg.VoiceAgent.Provider)
		}
		if !cfg.TTS.Deepgram.Enabled {
			t.Error("deepgram TTS must be enabled for the Aura WAV/Voice-Agent path")
		}
	})

	t.Run("assemblyai", func(t *testing.T) {
		cfg, err := Load(filepath.Join("..", "..", "deploy", "config", "server.assemblyai-gate.toml"))
		if err != nil {
			t.Fatalf("load assemblyai gate config: %v", err)
		}
		if !cfg.Providers.AssemblyAI.Enabled {
			t.Error("assemblyai provider must be enabled")
		}
		assertProvidersDisabled(t, "assemblyai gate", map[string]bool{
			"deepgram":    cfg.Providers.Deepgram.Enabled,
			"openai":      cfg.Providers.OpenAI.Enabled,
			"google":      cfg.Providers.Google.Enabled,
			"groq":        cfg.Providers.Groq.Enabled,
			"huggingface": cfg.HuggingFace.Enabled,
		})
		if cfg.Routing.Strategy != "cloud-only" {
			t.Errorf("routing.strategy = %q, want cloud-only", cfg.Routing.Strategy)
		}
	})
}

// TestStagingConfigLoads verifies the Render staging config loads and turns on
// the v0.43 providers it is meant to exercise (Deepgram STT/TTS/Voice-Agent +
// AssemblyAI STT + Google Assist LLM). It is baked into the staging image via
// the Dockerfile CONFIG_FILE build-arg.
func TestStagingConfigLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "deploy", "config", "server.staging.toml"))
	if err != nil {
		t.Fatalf("load staging config: %v", err)
	}
	if !cfg.Providers.Deepgram.Enabled || !cfg.Providers.AssemblyAI.Enabled || !cfg.Providers.Google.Enabled {
		t.Errorf("staging must enable deepgram+assemblyai+google: dg=%v aai=%v google=%v",
			cfg.Providers.Deepgram.Enabled, cfg.Providers.AssemblyAI.Enabled, cfg.Providers.Google.Enabled)
	}
	if !cfg.TTS.Deepgram.Enabled {
		t.Error("staging must enable Deepgram Aura TTS")
	}
	if cfg.VoiceAgent.Provider != "deepgram" {
		t.Errorf("staging voice_agent.provider = %q, want deepgram", cfg.VoiceAgent.Provider)
	}
	wantModes := map[string]bool{"dictation": true, "assist": true, "voiceagent": true}
	for _, m := range cfg.Server.Modes {
		delete(wantModes, m)
	}
	if len(wantModes) != 0 {
		t.Errorf("staging must enable all three modes; missing %v", wantModes)
	}
}

func assertProvidersDisabled(t *testing.T, gate string, providers map[string]bool) {
	t.Helper()
	for name, enabled := range providers {
		if enabled {
			t.Errorf("%s: %s must be disabled for deterministic single-provider routing", gate, name)
		}
	}
}
