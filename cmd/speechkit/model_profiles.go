package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

var launchLocalProvider = startLocalProviderAsync

func applyModelProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, profile models.Profile) error {
	switch profile.Modality {
	case models.ModalitySTT:
		return applySTTProfile(ctx, cfgPath, cfg, state, sttRouter, profile)
	case models.ModalityUtility:
		return applyUtilityProfile(ctx, cfgPath, cfg, state, profile)
	case models.ModalityAgent:
		return applyAgentProfile(ctx, cfgPath, cfg, state, profile)
	case models.ModalityRealtimeVoice:
		return applyRealtimeVoiceProfile(cfgPath, cfg, state, profile)
	default:
		return fmt.Errorf("unsupported modality %q", profile.Modality)
	}
}

func applySTTProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, profile models.Profile) error {
	if ctx == nil {
		ctx = context.Background()
	}

	targetRouter := sttRouter
	if targetRouter == nil && state != nil {
		targetRouter = state.sttRouter
	}

	switch profile.ExecutionMode {
	case models.ExecutionModeLocal:
		cfg.Local.Enabled = true
		if cfg.Routing.Strategy == "" || cfg.Routing.Strategy == "cloud-only" {
			cfg.Routing.Strategy = "dynamic"
		}
		var localProvider localProviderStarter
		if targetRouter != nil {
			if previous, ok := targetRouter.Local().(*stt.LocalProvider); ok {
				previous.StopServer()
			}
			modelPath := cfg.Local.ModelPath
			if modelPath == "" {
				modelPath = defaultLocalModelPath(executableDir(), os.Getenv("LOCALAPPDATA"), cfg.Local.Model)
			}
			localProvider = stt.NewLocalProvider(cfg.Local.Port, modelPath, cfg.Local.GPU)
			targetRouter.SetLocal(localProvider)
			targetRouter.Strategy = router.Strategy(cfg.Routing.Strategy)
		}
		if localProvider != nil {
			launchLocalProvider(ctx, state, targetRouter, localProvider)
		}
	case models.ExecutionModeHFRouted:
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err != nil || token == "" {
			return errors.New("hugging face token not configured")
		}
		cfg.HuggingFace.Enabled = true
		cfg.HuggingFace.Model = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("huggingface", stt.NewHuggingFaceProvider(profile.ModelID, token))
		}
	case models.ExecutionModeOpenAI:
		apiKey := config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		if apiKey == "" {
			return errors.New("openai api key not configured")
		}
		cfg.Providers.OpenAI.Enabled = true
		cfg.Providers.OpenAI.STTModel = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("openai", stt.NewOpenAICompatibleProvider("openai", "https://api.openai.com", apiKey, profile.ModelID))
		}
	case models.ExecutionModeGroq:
		apiKey := config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
		if apiKey == "" {
			return errors.New("groq api key not configured")
		}
		cfg.Providers.Groq.Enabled = true
		cfg.Providers.Groq.STTModel = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("groq", stt.NewOpenAICompatibleProvider("groq", "https://api.groq.com/openai", apiKey, profile.ModelID))
		}
	case models.ExecutionModeGoogle:
		apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		if apiKey == "" {
			return errors.New("google api key not configured")
		}
		cfg.Providers.Google.Enabled = true
		cfg.Providers.Google.STTModel = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("google", stt.NewGoogleSTTProvider(apiKey, profile.ModelID))
		}
	default:
		return fmt.Errorf("unsupported execution mode for STT")
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	if state != nil {
		state.mu.Lock()
		state.activeProfiles = activeProfilesFromConfig(cfg, filteredModelCatalog())
		state.mu.Unlock()
		syncRuntimeProviders(state, targetRouter)
	}
	slog.Info("model profile activated", "name", profile.Name, "model", profile.ModelID)
	return nil
}

func applyUtilityProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, profile models.Profile) error {
	clearUtilityModels(cfg)
	if err := configureLLMProfile(cfg, profile, true); err != nil {
		return err
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	if err := rebuildAIRuntime(ctx, state, cfg); err != nil {
		return err
	}
	slog.Info("utility LLM profile activated", "name", profile.Name, "model", profile.ModelID)
	return nil
}

func applyAgentProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, profile models.Profile) error {
	clearAgentModels(cfg)
	if err := configureLLMProfile(cfg, profile, false); err != nil {
		return err
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	if err := rebuildAIRuntime(ctx, state, cfg); err != nil {
		return err
	}
	slog.Info("agent LLM profile activated", "name", profile.Name, "model", profile.ModelID)
	return nil
}

func clearUtilityModels(cfg *config.Config) {
	cfg.Providers.OpenAI.UtilityModel = ""
	cfg.Providers.Groq.UtilityModel = ""
	cfg.Providers.Google.UtilityModel = ""
	cfg.Providers.Ollama.UtilityModel = ""
	cfg.Providers.OpenRouter.UtilityModel = ""
	cfg.HuggingFace.UtilityModel = ""
}

func clearAgentModels(cfg *config.Config) {
	cfg.Providers.OpenAI.AgentModel = ""
	cfg.Providers.Groq.AgentModel = ""
	cfg.Providers.Google.AgentModel = ""
	cfg.Providers.Ollama.AgentModel = ""
	cfg.Providers.OpenRouter.AgentModel = ""
	cfg.HuggingFace.AgentModel = ""
}

func configureLLMProfile(cfg *config.Config, profile models.Profile, utility bool) error {
	switch profile.ExecutionMode {
	case models.ExecutionModeOpenAI:
		if config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv) == "" {
			return errors.New("openai api key not configured")
		}
		cfg.Providers.OpenAI.Enabled = true
		if utility {
			cfg.Providers.OpenAI.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.OpenAI.AgentModel = profile.ModelID
		}
	case models.ExecutionModeGroq:
		if config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv) == "" {
			return errors.New("groq api key not configured")
		}
		cfg.Providers.Groq.Enabled = true
		if utility {
			cfg.Providers.Groq.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.Groq.AgentModel = profile.ModelID
		}
	case models.ExecutionModeGoogle:
		if config.ResolveSecret(cfg.Providers.Google.APIKeyEnv) == "" {
			return errors.New("google api key not configured")
		}
		cfg.Providers.Google.Enabled = true
		if utility {
			cfg.Providers.Google.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.Google.AgentModel = profile.ModelID
		}
	case models.ExecutionModeHFRouted:
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err != nil || token == "" {
			return errors.New("hugging face token not configured")
		}
		cfg.HuggingFace.Enabled = true
		if utility {
			cfg.HuggingFace.UtilityModel = profile.ModelID
		} else {
			cfg.HuggingFace.AgentModel = profile.ModelID
		}
	case models.ExecutionModeOllama:
		cfg.Providers.Ollama.Enabled = true
		if cfg.Providers.Ollama.BaseURL == "" {
			cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
		}
		if utility {
			cfg.Providers.Ollama.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.Ollama.AgentModel = profile.ModelID
		}
	case models.ExecutionModeOpenRouter:
		if config.ResolveSecret(cfg.Providers.OpenRouter.APIKeyEnv) == "" {
			return errors.New("openrouter api key not configured")
		}
		cfg.Providers.OpenRouter.Enabled = true
		if utility {
			cfg.Providers.OpenRouter.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.OpenRouter.AgentModel = profile.ModelID
		}
	default:
		return fmt.Errorf("unsupported execution mode %q", profile.ExecutionMode)
	}

	return nil
}

func rebuildAIRuntime(ctx context.Context, state *appState, cfg *config.Config) error {
	if state == nil {
		return nil
	}

	genkitRT, err := appai.Init(ctx, buildGenkitConfig(cfg))
	if err != nil {
		return fmt.Errorf("reload genkit: %w", err)
	}

	summarizeFlow := flows.DefineSummarizeFlow(genkitRT.G, genkitRT.UtilityModels())
	agentFlow := flows.DefineAgentFlow(genkitRT.G, genkitRT.AgentModels())
	assistFlow := flows.DefineAssistFlow(genkitRT.G, genkitRT.UtilityModels())

	var assistPipeline *assist.Pipeline
	if state.ttsRouter != nil {
		assistPipeline = assist.NewPipeline(assistFlow, state.ttsRouter, cfg.TTS.Enabled)
	}

	state.mu.Lock()
	state.genkitRT = genkitRT
	state.summarizeFlow = summarizeFlow
	state.agentFlow = agentFlow
	state.assistFlow = assistFlow
	state.assistPipeline = assistPipeline
	state.activeProfiles = activeProfilesFromConfig(cfg, filteredModelCatalog())
	state.mu.Unlock()

	return nil
}

// applyRealtimeVoiceProfile configures the Voice Agent for a real-time voice profile.
func applyRealtimeVoiceProfile(cfgPath string, cfg *config.Config, state *appState, profile models.Profile) error {
	apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
	if apiKey == "" {
		return errors.New("google api key not configured â€” set it in the API Keys section below")
	}
	cfg.VoiceAgent.Enabled = true
	cfg.VoiceAgent.Model = profile.ModelID
	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if state != nil {
		state.mu.Lock()
		state.activeProfiles = activeProfilesFromConfig(cfg, filteredModelCatalog())
		state.mu.Unlock()
	}
	return nil
}
