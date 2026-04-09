package main

// app_init.go contains builder and helper functions extracted from main.go.
// These are stateless initialization helpers that construct subsystems from
// config, making them independently testable without a full Wails app instance.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// runtimeConfigPath returns the path to config.toml, co-located with the binary.
func runtimeConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(filepath.Dir(exe), "config.toml")
}

// buildRouter constructs the STT router from config and returns it together
// with a slice of human-readable status messages for the startup log.
func buildRouter(cfg *config.Config) (*router.Router, []string) {
	var msgs []string
	r := &router.Router{
		Strategy:             router.Strategy(cfg.Routing.Strategy),
		PreferLocalUnderSecs: cfg.Routing.PreferLocalUnderSeconds,
		ParallelCloud:        cfg.Routing.ParallelCloud,
		ReplaceOnBetter:      cfg.Routing.ReplaceOnBetter,
	}

	if config.ManagedHuggingFaceAvailableInBuild() && cfg.HuggingFace.Enabled {
		hfToken, tokenStatus, err := config.ResolveHuggingFaceToken(cfg)
		if err != nil || hfToken == "" {
			tokenEnv := config.HuggingFaceTokenEnvName(cfg)
			if tokenEnv == "" {
				tokenEnv = "HF_TOKEN"
			}
			msgs = append(msgs, fmt.Sprintf("WARN: %s not found in host secret store, env or Doppler", tokenEnv))
		} else {
			r.SetHuggingFace(newHuggingFaceProvider(cfg.HuggingFace.Model, hfToken))
			msgs = append(msgs, fmt.Sprintf("HuggingFace: %s (source: %s)", cfg.HuggingFace.Model, tokenStatus.ActiveSource))
		}
	}

	if cfg.VPS.Enabled && cfg.VPS.URL != "" {
		apiKey := config.ResolveSecret(cfg.VPS.APIKeyEnv)
		r.SetVPS(stt.NewVPSProvider(cfg.VPS.URL, apiKey))
		msgs = append(msgs, fmt.Sprintf("VPS: %s", cfg.VPS.URL))
	}

	if cfg.Local.Enabled {
		modelPath := cfg.Local.ModelPath
		if modelPath == "" {
			modelPath = defaultLocalModelPath(executableDir(), os.Getenv("LOCALAPPDATA"), cfg.Local.Model)
		}
		r.SetLocal(stt.NewLocalProvider(cfg.Local.Port, modelPath, cfg.Local.GPU))
		msgs = append(msgs, fmt.Sprintf("Local: %s (not started)", cfg.Local.Model))
	}

	if cfg.Providers.Groq.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
		if apiKey != "" {
			r.AddCloud(stt.NewGroqSTTProvider(apiKey))
			msgs = append(msgs, "STT: Groq provider registered")
		}
	}

	if cfg.Providers.OpenAI.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		if apiKey != "" {
			r.AddCloud(stt.NewOpenAISTTProvider(apiKey))
			msgs = append(msgs, "STT: OpenAI provider registered")
		}
	}

	if cfg.Providers.Google.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		if apiKey != "" {
			r.AddCloud(stt.NewGoogleSTTProvider(apiKey, cfg.Providers.Google.STTModel))
			msgs = append(msgs, "STT: Google provider registered")
		}
	}

	return r, msgs
}

// buildGenkitConfig maps provider config into the appai.Config structure.
func buildGenkitConfig(cfg *config.Config) appai.Config {
	var aiCfg appai.Config

	if cfg.Providers.Google.Enabled {
		aiCfg.GoogleAPIKey = config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		aiCfg.GoogleUtilityModel = cfg.Providers.Google.UtilityModel
		aiCfg.GoogleAgentModel = cfg.Providers.Google.AgentModel
	}

	if cfg.Providers.OpenAI.Enabled {
		aiCfg.OpenAIAPIKey = config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		aiCfg.OpenAIUtilityModel = cfg.Providers.OpenAI.UtilityModel
		aiCfg.OpenAIAgentModel = cfg.Providers.OpenAI.AgentModel
	}

	if cfg.Providers.Groq.Enabled {
		aiCfg.GroqAPIKey = config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
		aiCfg.GroqUtilityModel = cfg.Providers.Groq.UtilityModel
		aiCfg.GroqAgentModel = cfg.Providers.Groq.AgentModel
	}

	if cfg.HuggingFace.Enabled {
		token, _, _ := config.ResolveHuggingFaceToken(cfg)
		aiCfg.HuggingFaceToken = token
		aiCfg.HFUtilityModel = cfg.HuggingFace.UtilityModel
		aiCfg.HFAgentModel = cfg.HuggingFace.AgentModel
	}

	if cfg.Providers.Ollama.Enabled {
		aiCfg.OllamaBaseURL = cfg.Providers.Ollama.BaseURL
		aiCfg.OllamaUtilityModel = cfg.Providers.Ollama.UtilityModel
		aiCfg.OllamaAgentModel = cfg.Providers.Ollama.AgentModel
	}

	return aiCfg
}

// buildTTSRouter constructs the TTS router from config. Returns nil if TTS is disabled
// or no providers are configured.
func buildTTSRouter(cfg *config.Config) *tts.Router {
	if !cfg.TTS.Enabled {
		return nil
	}

	var providers []tts.Provider

	if cfg.TTS.OpenAI.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		if apiKey != "" {
			model := cfg.TTS.OpenAI.Model
			if model == "" {
				model = cfg.Providers.OpenAI.TTSModel
			}
			voice := cfg.TTS.OpenAI.Voice
			if voice == "" {
				voice = cfg.Providers.OpenAI.TTSVoice
			}
			providers = append(providers, tts.NewOpenAI(tts.OpenAIOpts{
				APIKey: apiKey,
				Model:  model,
				Voice:  voice,
			}))
		}
	}

	if cfg.TTS.Google.Enabled {
		apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		if apiKey != "" {
			providers = append(providers, tts.NewGoogle(tts.GoogleOpts{
				APIKey: apiKey,
				Voice:  cfg.TTS.Google.Voice,
			}))
		}
	}

	if cfg.TTS.HuggingFace.Enabled {
		token := config.ResolveSecret(cfg.HuggingFace.TokenEnv)
		if token != "" {
			providers = append(providers, tts.NewHuggingFace(tts.HuggingFaceOpts{
				Token: token,
				Model: cfg.TTS.HuggingFace.Model,
			}))
		}
	}

	if len(providers) == 0 {
		return nil
	}

	return tts.NewRouter(tts.Strategy(cfg.TTS.Strategy), providers...)
}

// validateCloudProviders runs a quick health check on all configured cloud
// providers and returns human-readable status messages.
func validateCloudProviders(ctx context.Context, r *router.Router) []string {
	var msgs []string

	if vps := r.VPS(); vps != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := vps.Health(checkCtx)
		cancel()
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("VPS unavailable: %v", err))
		} else {
			msgs = append(msgs, "VPS ready")
		}
	}

	if hf := r.HuggingFace(); hf != nil {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := hf.Health(checkCtx)
		cancel()
		if err != nil {
			msgs = append(msgs, fmt.Sprintf("HuggingFace unavailable: %v", err))
		} else {
			msgs = append(msgs, "HuggingFace ready")
		}
	}

	return msgs
}

// missingProviderHint returns a user-visible hint when no STT provider is
// available, describing what the user needs to configure. Returns "" if a
// provider is expected to be available.
func missingProviderHint(cfg *config.Config) string {
	hfAvailable := config.ManagedHuggingFaceAvailableInBuild()
	hfEnabled := hfAvailable && cfg.HuggingFace.Enabled

	if cfg.Routing.Strategy == "cloud-only" && !hfEnabled && !cfg.VPS.Enabled {
		if hfAvailable {
			return "Cloud-only routing is active, but no cloud provider is enabled. Enable Hugging Face Inference or configure VPS."
		}
		return "Cloud-only routing is active, but no cloud provider is enabled. Configure VPS or switch to a local-capable routing mode."
	}

	if hfEnabled && cfg.VPS.Enabled {
		return ""
	}

	if hfEnabled {
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err == nil && token != "" {
			return ""
		}
		tokenEnv := config.HuggingFaceTokenEnvName(cfg)
		if tokenEnv == "" {
			tokenEnv = "HF_TOKEN"
		}
		return fmt.Sprintf("Hugging Face Inference is enabled, but no token could be resolved from settings, install bootstrap, %s, env or Doppler.", tokenEnv)
	}
	if cfg.VPS.Enabled && cfg.VPS.URL == "" {
		return "VPS provider is enabled, but no VPS URL is configured."
	}

	return ""
}

// executableDir returns the directory of the running binary.
func executableDir() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return ""
	}
	return filepath.Dir(exe)
}

// defaultLocalModelPath resolves where the local Whisper model file lives,
// preferring bundle-adjacent paths and falling back to AppData.
func defaultLocalModelPath(exeDir string, localAppData string, modelName string) string {
	if exeDir != "" && modelName != "" {
		bundlePath := filepath.Join(exeDir, "models", modelName)
		if _, err := os.Stat(bundlePath); err == nil {
			return bundlePath
		}
	}
	if localAppData != "" && modelName != "" {
		return filepath.Join(localAppData, "SpeechKit", "models", modelName)
	}
	if exeDir != "" && modelName != "" {
		return filepath.Join(exeDir, "models", modelName)
	}
	return ""
}

// escapeJS returns s as a safe JavaScript string literal (without surrounding quotes).
// Uses json.Marshal to handle all special characters including backticks and
// unicode line/paragraph separators (U+2028, U+2029).
func escapeJS(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	// json.Marshal returns a "quoted string" — strip the surrounding quotes.
	return string(b[1 : len(b)-1])
}
