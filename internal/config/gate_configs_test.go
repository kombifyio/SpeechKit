package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProviderGateConfigsLoadDeterministically verifies the per-provider
// functional-gate reference configs load and enable only the target provider
// surface for each gate. The provider-live-gate workflow relies on this
// determinism so `sk-e2e --expect-provider <name>` proves real routing rather
// than a silent fallback to another configured provider.
func TestProviderGateConfigsLoadDeterministically(t *testing.T) {
	t.Run("deepgram", func(t *testing.T) {
		cfg := loadPrivateGateConfig(t, filepath.Join("..", "..", "deploy", "config", "server.deepgram-gate.toml"))
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
		cfg := loadPrivateGateConfig(t, filepath.Join("..", "..", "deploy", "config", "server.assemblyai-gate.toml"))
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
		if cfg.VoiceAgent.Provider != "assemblyai" {
			t.Errorf("voice_agent.provider = %q, want assemblyai", cfg.VoiceAgent.Provider)
		}
		if cfg.VoiceAgent.Model != "assemblyai-voice-agent" {
			t.Errorf("voice_agent.model = %q, want assemblyai-voice-agent", cfg.VoiceAgent.Model)
		}
	})

	t.Run("openai", func(t *testing.T) {
		cfg := loadPrivateGateConfig(t, filepath.Join("..", "..", "deploy", "config", "server.openai-gate.toml"))
		if !cfg.Providers.OpenAI.Enabled {
			t.Error("openai provider must be enabled")
		}
		assertProvidersDisabled(t, "openai gate", map[string]bool{
			"deepgram":    cfg.Providers.Deepgram.Enabled,
			"assemblyai":  cfg.Providers.AssemblyAI.Enabled,
			"google":      cfg.Providers.Google.Enabled,
			"groq":        cfg.Providers.Groq.Enabled,
			"huggingface": cfg.HuggingFace.Enabled,
		})
		if cfg.Routing.Strategy != "cloud-only" {
			t.Errorf("routing.strategy = %q, want cloud-only", cfg.Routing.Strategy)
		}
		if cfg.VoiceAgent.Provider != "openai" {
			t.Errorf("voice_agent.provider = %q, want openai", cfg.VoiceAgent.Provider)
		}
		if cfg.Providers.OpenAI.RealtimeModel != "gpt-realtime-2" {
			t.Errorf("providers.openai.realtime_model = %q, want gpt-realtime-2", cfg.Providers.OpenAI.RealtimeModel)
		}
	})

	t.Run("gemini", func(t *testing.T) {
		cfg := loadPrivateGateConfig(t, filepath.Join("..", "..", "deploy", "config", "server.gemini-gate.toml"))
		if !cfg.Providers.Google.Enabled {
			t.Error("google provider must be enabled for Gemini Live")
		}
		assertProvidersDisabled(t, "gemini gate", map[string]bool{
			"deepgram":    cfg.Providers.Deepgram.Enabled,
			"assemblyai":  cfg.Providers.AssemblyAI.Enabled,
			"openai":      cfg.Providers.OpenAI.Enabled,
			"groq":        cfg.Providers.Groq.Enabled,
			"huggingface": cfg.HuggingFace.Enabled,
		})
		if cfg.Routing.Strategy != "cloud-only" {
			t.Errorf("routing.strategy = %q, want cloud-only", cfg.Routing.Strategy)
		}
		if cfg.VoiceAgent.Provider != "gemini" {
			t.Errorf("voice_agent.provider = %q, want gemini", cfg.VoiceAgent.Provider)
		}
		if cfg.VoiceAgent.Model != "gemini-3.1-flash-live-preview" {
			t.Errorf("voice_agent.model = %q, want gemini-3.1-flash-live-preview", cfg.VoiceAgent.Model)
		}
		assertModes(t, cfg.Server.Modes, map[string]bool{"voiceagent": true})
	})
}

// TestStagingConfigLoads verifies the Render staging config loads and turns on
// the v0.43 providers it is meant to exercise (Deepgram STT/TTS/Voice-Agent +
// AssemblyAI STT + Google Assist LLM). It is baked into the staging image via
// the Dockerfile CONFIG_FILE build-arg.
func TestStagingConfigLoads(t *testing.T) {
	cfg := loadPrivateGateConfig(t, filepath.Join("..", "..", "deploy", "config", "server.staging.toml"))
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

func assertProvidersDisabled(t *testing.T, gate string, providers map[string]bool) {
	t.Helper()
	for name, enabled := range providers {
		if enabled {
			t.Errorf("%s: %s must be disabled for deterministic single-provider routing", gate, name)
		}
	}
}

func assertModes(t *testing.T, got []string, want map[string]bool) {
	t.Helper()
	remaining := map[string]bool{}
	for mode, expected := range want {
		if expected {
			remaining[mode] = true
		}
	}
	for _, mode := range got {
		if !want[mode] {
			t.Errorf("unexpected server mode %q", mode)
			continue
		}
		delete(remaining, mode)
	}
	for mode := range remaining {
		t.Errorf("missing server mode %q", mode)
	}
}
