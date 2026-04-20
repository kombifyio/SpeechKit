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
	"strings"
	"time"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/runtimepath"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// runtimeConfigPath returns the active config file path.
func runtimeConfigPath() string {
	return runtimepath.ConfigFilePath()
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

	if provider := configuredOllamaSTTProvider(cfg); provider != nil {
		r.AddCloud(provider)
		msgs = append(msgs, fmt.Sprintf("STT: Ollama provider registered (%s)", cfg.Providers.Ollama.STTModel))
	}

	if selectedLocalBuiltInSTT(cfg) {
		cfg.Local.Enabled = true
		cfg.Routing.Strategy = "local-only"
		r.Strategy = router.StrategyLocalOnly
	}

	if cfg.Local.Enabled {
		modelPath := configuredLocalSTTModelPath(cfg)
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

func selectedLocalBuiltInSTT(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	profile, ok := effectiveSelectedProfile(cfg, filteredModelCatalog(), modeDictate)
	return ok &&
		profile.Modality == models.ModalitySTT &&
		profile.ExecutionMode == models.ExecutionModeLocal &&
		profile.ProviderKind == models.ProviderKindLocalBuiltIn
}

// buildGenkitConfig maps provider config into the appai.Config structure.
func buildGenkitConfig(cfg *config.Config) appai.Config {
	var aiCfg appai.Config
	catalog := filteredModelCatalog()
	assistSelections, explicitAssistSelections := selectedModelSpecsForMode(cfg, catalog, modeAssist)
	voiceSelections, explicitVoiceSelections := selectedModelSpecsForMode(cfg, catalog, modeVoiceAgent)
	selectedProvider := func(provider string) bool {
		return orderedSelectionsContainProvider(assistSelections, provider) ||
			orderedSelectionsContainProvider(voiceSelections, provider)
	}

	if cfg.Providers.Google.Enabled {
		aiCfg.GoogleAPIKey = config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		aiCfg.GoogleUtilityModel = cfg.Providers.Google.UtilityModel
		aiCfg.GoogleAssistModel = cfg.Providers.Google.AssistModel
		aiCfg.GoogleAgentModel = cfg.Providers.Google.AgentModel
	}

	if cfg.Providers.OpenAI.Enabled {
		aiCfg.OpenAIAPIKey = config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		aiCfg.OpenAIUtilityModel = cfg.Providers.OpenAI.UtilityModel
		aiCfg.OpenAIAssistModel = cfg.Providers.OpenAI.AssistModel
		aiCfg.OpenAIAgentModel = cfg.Providers.OpenAI.AgentModel
	}

	if cfg.Providers.Groq.Enabled {
		aiCfg.GroqAPIKey = config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
		aiCfg.GroqUtilityModel = cfg.Providers.Groq.UtilityModel
		aiCfg.GroqAssistModel = cfg.Providers.Groq.AssistModel
		aiCfg.GroqAgentModel = cfg.Providers.Groq.AgentModel
	}

	if cfg.HuggingFace.Enabled {
		token, _, _ := config.ResolveHuggingFaceToken(cfg)
		aiCfg.HuggingFaceToken = token
		aiCfg.HFUtilityModel = cfg.HuggingFace.UtilityModel
		aiCfg.HFAssistModel = cfg.HuggingFace.AssistModel
		aiCfg.HFAgentModel = cfg.HuggingFace.AgentModel
	}

	if cfg.LocalLLM.Enabled || selectedProvider("local") {
		aiCfg.LocalLLMBaseURL = cfg.LocalLLM.BaseURL
		if aiCfg.LocalLLMBaseURL == "" {
			aiCfg.LocalLLMBaseURL = config.DefaultLocalLLMBaseURL
		}
		aiCfg.LocalLLMUtilityModel = cfg.LocalLLM.UtilityModel
		aiCfg.LocalLLMAssistModel = cfg.LocalLLM.AssistModel
		aiCfg.LocalLLMAgentModel = cfg.LocalLLM.AgentModel
	}

	if cfg.Providers.Ollama.Enabled || selectedProvider("ollama") {
		aiCfg.OllamaBaseURL = cfg.Providers.Ollama.BaseURL
		if aiCfg.OllamaBaseURL == "" {
			aiCfg.OllamaBaseURL = "http://localhost:11434"
		}
		aiCfg.OllamaUtilityModel = cfg.Providers.Ollama.UtilityModel
		aiCfg.OllamaAssistModel = cfg.Providers.Ollama.AssistModel
		aiCfg.OllamaAgentModel = cfg.Providers.Ollama.AgentModel
	}

	if cfg.Providers.OpenRouter.Enabled {
		aiCfg.OpenRouterAPIKey = config.ResolveSecret(cfg.Providers.OpenRouter.APIKeyEnv)
		aiCfg.OpenRouterUtilityModel = cfg.Providers.OpenRouter.UtilityModel
		aiCfg.OpenRouterAssistModel = cfg.Providers.OpenRouter.AssistModel
		aiCfg.OpenRouterAgentModel = cfg.Providers.OpenRouter.AgentModel
	}

	if explicitAssistSelections {
		aiCfg.OrderedAssistModels = assistSelections
		aiCfg.UseOrderedAssistModels = true
	}

	if explicitVoiceSelections {
		aiCfg.OrderedAgentModels = voiceSelections
		aiCfg.UseOrderedAgentModels = true
	}

	return aiCfg
}

func orderedSelectionsContainProvider(selections []appai.OrderedModelSelection, provider string) bool {
	for _, selection := range selections {
		if selection.Provider == provider {
			return true
		}
	}
	return false
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

func buildShortcutResolver(cfg *config.Config) *shortcuts.Resolver {
	registry := shortcuts.DefaultRegistry()
	if cfg == nil {
		return shortcuts.NewResolver(registry)
	}

	for locale, localeCfg := range cfg.Shortcuts.Locale {
		registry.RegisterLeadingFillers(locale, localeCfg.LeadingFillers...)
		registerShortcutAliases(registry, locale, shortcuts.IntentCopyLast, localeCfg.CopyLast)
		registerShortcutAliases(registry, locale, shortcuts.IntentInsertLast, localeCfg.InsertLast)
		registerShortcutAliases(registry, locale, shortcuts.IntentSummarize, localeCfg.Summarize)
		registerShortcutAliases(registry, locale, shortcuts.IntentQuickNote, localeCfg.QuickNote)
	}

	return shortcuts.NewResolver(registry)
}

func registerShortcutAliases(registry *shortcuts.Registry, locale string, intent shortcuts.Intent, aliases []string) {
	if registry == nil || len(aliases) == 0 {
		return
	}

	phrases := make([]shortcuts.Phrase, 0, len(aliases))
	for _, alias := range aliases {
		phrases = append(phrases, shortcuts.Phrase{
			Value:    alias,
			Prefix:   true,
			Priority: 100,
		})
	}

	registry.RegisterLexicon(shortcuts.IntentLexicon{
		Intent:  intent,
		Locale:  locale,
		Phrases: phrases,
	})
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
func defaultLocalModelPath(exeDir, localAppData, modelName string) string {
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

func configuredLocalSTTModelPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if modelPath := strings.TrimSpace(cfg.Local.ModelPath); modelPath != "" {
		return modelPath
	}
	modelName := strings.TrimSpace(cfg.Local.Model)
	if modelName == "" {
		return ""
	}
	if downloadDir := strings.TrimSpace(cfg.General.ModelDownloadDir); downloadDir != "" {
		return filepath.Join(downloadDir, modelName)
	}
	return defaultLocalModelPath(executableDir(), os.Getenv("LOCALAPPDATA"), modelName)
}

// escapeJS returns s as a safe JavaScript string literal (without surrounding quotes).
// Uses json.Marshal to handle all special characters including backticks and
// unicode line/paragraph separators (U+2028, U+2029).
func escapeJS(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	// json.Marshal returns a "quoted string" â€” strip the surrounding quotes.
	return string(b[1 : len(b)-1])
}
