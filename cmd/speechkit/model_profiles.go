package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/firebase/genkit/go/core"
	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

var launchLocalProvider = startLocalProviderAsync
var reloadAIRuntime = rebuildAIRuntime

func applyModelProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, profile models.Profile) error {
	switch profile.Modality {
	case models.ModalitySTT:
		return applySTTProfile(ctx, cfgPath, cfg, state, sttRouter, profile)
	case models.ModalityUtility:
		return applyUtilityProfile(ctx, cfgPath, cfg, state, profile)
	case models.ModalityAssist:
		return applyAssistProfile(ctx, cfgPath, cfg, state, profile)
	case models.ModalityRealtimeVoice:
		return applyRealtimeVoiceProfile(ctx, cfgPath, cfg, state, profile)
	default:
		return fmt.Errorf("unsupported modality %q", profile.Modality)
	}
}

func applySTTProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, sttRouter *router.Router, profile models.Profile) error {
	targetRouter := sttRouter
	if targetRouter == nil && state != nil {
		targetRouter = state.sttRouter
	}

	switch profile.ExecutionMode {
	case models.ExecutionModeLocal:
		modelPath := configuredLocalSTTModelPath(cfg)
		if err := validateLocalProviderActivation(cfg, modelPath); err != nil {
			return err
		}
		cfg.Local.Enabled = true
		cfg.Routing.Strategy = "local-only"
		syncConfiguredLocalProvider(ctx, cfg, state, targetRouter)
	case models.ExecutionModeHFRouted:
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err != nil || token == "" {
			return errors.New("hugging face token not configured")
		}
		cfg.Routing.Strategy = "cloud-only"
		cfg.HuggingFace.Enabled = true
		cfg.HuggingFace.Model = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("huggingface", stt.NewHuggingFaceProvider(profile.ModelID, token))
			syncConfiguredLocalProvider(ctx, cfg, state, targetRouter)
		}
	case models.ExecutionModeOpenAI:
		apiKey := config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv)
		if apiKey == "" {
			return errors.New("openai api key not configured")
		}
		cfg.Routing.Strategy = "cloud-only"
		cfg.Providers.OpenAI.Enabled = true
		cfg.Providers.OpenAI.STTModel = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("openai", stt.NewOpenAICompatibleProvider("openai", "https://api.openai.com", apiKey, profile.ModelID))
			syncConfiguredLocalProvider(ctx, cfg, state, targetRouter)
		}
	case models.ExecutionModeGroq:
		apiKey := config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv)
		if apiKey == "" {
			return errors.New("groq api key not configured")
		}
		cfg.Routing.Strategy = "cloud-only"
		cfg.Providers.Groq.Enabled = true
		cfg.Providers.Groq.STTModel = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("groq", stt.NewOpenAICompatibleProvider("groq", "https://api.groq.com/openai", apiKey, profile.ModelID))
			syncConfiguredLocalProvider(ctx, cfg, state, targetRouter)
		}
	case models.ExecutionModeGoogle:
		apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		if apiKey == "" {
			return errors.New("google api key not configured")
		}
		cfg.Routing.Strategy = "cloud-only"
		cfg.Providers.Google.Enabled = true
		cfg.Providers.Google.STTModel = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("google", stt.NewGoogleSTTProvider(apiKey, profile.ModelID))
			syncConfiguredLocalProvider(ctx, cfg, state, targetRouter)
		}
	case models.ExecutionModeOllama:
		cfg.Routing.Strategy = "cloud-only"
		cfg.Providers.Ollama.Enabled = true
		if cfg.Providers.Ollama.BaseURL == "" {
			cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
		}
		cfg.Providers.Ollama.STTModel = profile.ModelID
		if targetRouter != nil {
			targetRouter.PreferCloud("ollama", stt.NewOllamaSTTProvider(cfg.Providers.Ollama.BaseURL, profile.ModelID))
			syncConfiguredLocalProvider(ctx, cfg, state, targetRouter)
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
		syncRuntimeProviders(ctx, state, targetRouter)
	}
	slog.Info("model profile activated", "name", profile.Name, "model", profile.ModelID)
	return nil
}

func syncConfiguredLocalProvider(ctx context.Context, cfg *config.Config, state *appState, sttRouter *router.Router) {
	targetRouter := sttRouter
	if targetRouter == nil && state != nil {
		targetRouter = state.sttRouter
	}
	if targetRouter == nil || cfg == nil {
		return
	}

	targetRouter.Strategy = router.Strategy(cfg.Routing.Strategy)
	if !cfg.Local.Enabled || cfg.Routing.Strategy == "cloud-only" {
		targetRouter.SetLocal(nil)
		return
	}

	modelPath := configuredLocalSTTModelPath(cfg)

	if existing, ok := targetRouter.Local().(*stt.LocalProvider); ok && existing != nil {
		if existing.Port == cfg.Local.Port && existing.ModelPath == modelPath && existing.GPU == cfg.Local.GPU {
			launchLocalProvider(context.WithoutCancel(ctx), state, targetRouter, existing)
			return
		}
	}

	localProvider := stt.NewLocalProvider(cfg.Local.Port, modelPath, cfg.Local.GPU)
	if previous, ok := targetRouter.Local().(*stt.LocalProvider); ok {
		previous.StopServer()
	}
	targetRouter.SetLocal(localProvider)
	// Local whisper startup must survive short-lived HTTP request contexts.
	launchLocalProvider(context.WithoutCancel(ctx), state, targetRouter, localProvider)
}

func applyUtilityProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, profile models.Profile) error {
	clearUtilityModels(cfg)
	if err := configureLLMProfile(cfg, profile, models.ModalityUtility); err != nil {
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

func applyAssistProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, profile models.Profile) error {
	clearAssistModels(cfg)
	if err := configureLLMProfile(cfg, profile, models.ModalityAssist); err != nil {
		return err
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}
	if err := rebuildAIRuntime(ctx, state, cfg); err != nil {
		return err
	}
	slog.Info("assist LLM profile activated", "name", profile.Name, "model", profile.ModelID)
	return nil
}

func clearUtilityModels(cfg *config.Config) {
	cfg.Providers.OpenAI.UtilityModel = ""
	cfg.Providers.Groq.UtilityModel = ""
	cfg.Providers.Google.UtilityModel = ""
	cfg.Providers.Ollama.UtilityModel = ""
	cfg.Providers.OpenRouter.UtilityModel = ""
	cfg.LocalLLM.UtilityModel = ""
	cfg.HuggingFace.UtilityModel = ""
}

func clearAssistModels(cfg *config.Config) {
	cfg.Providers.OpenAI.AssistModel = ""
	cfg.Providers.Groq.AssistModel = ""
	cfg.Providers.Google.AssistModel = ""
	cfg.Providers.Ollama.AssistModel = ""
	cfg.Providers.OpenRouter.AssistModel = ""
	cfg.LocalLLM.AssistModel = ""
	cfg.HuggingFace.AssistModel = ""
}

func configureLLMProfile(cfg *config.Config, profile models.Profile, modality models.Modality) error {
	switch profile.ExecutionMode {
	case models.ExecutionModeLocal:
		modelID := profile.ModelID
		if profile.ProviderKind == models.ProviderKindLocalBuiltIn && cfg.LocalLLM.Model != "" {
			modelID = cfg.LocalLLM.Model
		}
		cfg.LocalLLM.Enabled = true
		if cfg.LocalLLM.BaseURL == "" {
			cfg.LocalLLM.BaseURL = config.DefaultLocalLLMBaseURL
		}
		if cfg.LocalLLM.Port == 0 {
			cfg.LocalLLM.Port = 8082
		}
		if cfg.LocalLLM.Model == "" {
			cfg.LocalLLM.Model = modelID
		}
		if modality == models.ModalityUtility {
			cfg.LocalLLM.UtilityModel = modelID
		} else {
			cfg.LocalLLM.AssistModel = modelID
		}
	case models.ExecutionModeOpenAI:
		if config.ResolveSecret(cfg.Providers.OpenAI.APIKeyEnv) == "" {
			return errors.New("openai api key not configured")
		}
		cfg.Providers.OpenAI.Enabled = true
		if modality == models.ModalityUtility {
			cfg.Providers.OpenAI.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.OpenAI.AssistModel = profile.ModelID
		}
	case models.ExecutionModeGroq:
		if config.ResolveSecret(cfg.Providers.Groq.APIKeyEnv) == "" {
			return errors.New("groq api key not configured")
		}
		cfg.Providers.Groq.Enabled = true
		if modality == models.ModalityUtility {
			cfg.Providers.Groq.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.Groq.AssistModel = profile.ModelID
		}
	case models.ExecutionModeGoogle:
		if config.ResolveSecret(cfg.Providers.Google.APIKeyEnv) == "" {
			return errors.New("google api key not configured")
		}
		cfg.Providers.Google.Enabled = true
		if modality == models.ModalityUtility {
			cfg.Providers.Google.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.Google.AssistModel = profile.ModelID
		}
	case models.ExecutionModeHFRouted:
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err != nil || token == "" {
			return errors.New("hugging face token not configured")
		}
		cfg.HuggingFace.Enabled = true
		if modality == models.ModalityUtility {
			cfg.HuggingFace.UtilityModel = profile.ModelID
		} else {
			cfg.HuggingFace.AssistModel = profile.ModelID
		}
	case models.ExecutionModeOllama:
		cfg.Providers.Ollama.Enabled = true
		if cfg.Providers.Ollama.BaseURL == "" {
			cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
		}
		if modality == models.ModalityUtility {
			cfg.Providers.Ollama.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.Ollama.AssistModel = profile.ModelID
		}
	case models.ExecutionModeOpenRouter:
		if config.ResolveSecret(cfg.Providers.OpenRouter.APIKeyEnv) == "" {
			return errors.New("openrouter api key not configured")
		}
		cfg.Providers.OpenRouter.Enabled = true
		if modality == models.ModalityUtility {
			cfg.Providers.OpenRouter.UtilityModel = profile.ModelID
		} else {
			cfg.Providers.OpenRouter.AssistModel = profile.ModelID
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
	var assistFlow *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	if len(genkitRT.AssistModels()) > 0 {
		assistFlow = flows.DefineAssistFlow(genkitRT.G, genkitRT.AssistModels())
	}

	var assistPipeline *assist.Pipeline
	if state.ttsRouter != nil || state.assistExecutor != nil {
		assistPipeline = assist.NewPipeline(assistFlow, state.assistExecutor, state.ttsRouter, cfg.TTS.Enabled)
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

func clearAgentModels(cfg *config.Config) {
	cfg.Providers.OpenAI.AgentModel = ""
	cfg.Providers.Groq.AgentModel = ""
	cfg.Providers.Google.AgentModel = ""
	cfg.Providers.Ollama.AgentModel = ""
	cfg.Providers.OpenRouter.AgentModel = ""
	cfg.LocalLLM.AgentModel = ""
	cfg.HuggingFace.AgentModel = ""
}

// applyRealtimeVoiceProfile configures the Voice Agent for a real-time voice profile.
func applyRealtimeVoiceProfile(ctx context.Context, cfgPath string, cfg *config.Config, state *appState, profile models.Profile) error {
	clearAgentModels(cfg)
	switch profile.ExecutionMode {
	case models.ExecutionModeGoogle:
		apiKey := config.ResolveSecret(cfg.Providers.Google.APIKeyEnv)
		if apiKey == "" {
			return errors.New("google api key not configured â€” add it on the model card in Settings")
		}
		cfg.VoiceAgent.Enabled = true
		cfg.VoiceAgent.Model = profile.ModelID
		cfg.VoiceAgent.PipelineFallback = false
	case models.ExecutionModeHFRouted:
		token, _, err := config.ResolveHuggingFaceToken(cfg)
		if err != nil || token == "" {
			return errors.New("hugging face token not configured")
		}
		cfg.HuggingFace.Enabled = true
		cfg.HuggingFace.AgentModel = profile.ModelID
		cfg.VoiceAgent.Enabled = true
		cfg.VoiceAgent.Model = profile.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	case models.ExecutionModeOllama:
		cfg.Providers.Ollama.Enabled = true
		if cfg.Providers.Ollama.BaseURL == "" {
			cfg.Providers.Ollama.BaseURL = "http://localhost:11434"
		}
		cfg.Providers.Ollama.AgentModel = profile.ModelID
		cfg.VoiceAgent.Enabled = true
		cfg.VoiceAgent.Model = profile.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	case models.ExecutionModeLocal:
		cfg.LocalLLM.Enabled = true
		if cfg.LocalLLM.BaseURL == "" {
			cfg.LocalLLM.BaseURL = config.DefaultLocalLLMBaseURL
		}
		if cfg.LocalLLM.Port == 0 {
			cfg.LocalLLM.Port = 8082
		}
		cfg.LocalLLM.AgentModel = profile.ModelID
		cfg.VoiceAgent.Enabled = true
		cfg.VoiceAgent.Model = profile.ModelID
		cfg.VoiceAgent.PipelineFallback = true
	default:
		return fmt.Errorf("unsupported execution mode for realtime voice")
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := reloadAIRuntime(ctx, state, cfg); err != nil {
		return err
	}
	if state != nil {
		state.mu.Lock()
		state.activeProfiles = activeProfilesFromConfig(cfg, filteredModelCatalog())
		state.mu.Unlock()
	}
	return nil
}
