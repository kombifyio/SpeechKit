package config

import (
	"log/slog"
	"strings"

	"github.com/BurntSushi/toml"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

// MigrateRetiredModels rewrites every persisted model id SpeechKit retired to
// its successor (catalog.RetiredModels) and every retired TTS profile id to
// its current profile, so an upgraded install never dials a model the vendor
// shut down or SpeechKit stopped offering. Foundry deployment names are left
// alone: they are user-owned names that only look like model ids.
func MigrateRetiredModels(cfg *Config) {
	if cfg == nil {
		return
	}
	p := &cfg.Providers
	for provider, fields := range map[string][]*string{
		"openai": {
			&p.OpenAI.STTModel, &p.OpenAI.UtilityModel, &p.OpenAI.AssistModel, &p.OpenAI.AgentModel,
			&p.OpenAI.TTSModel, &p.OpenAI.RealtimeModel, &cfg.TTS.OpenAI.Model,
		},
		"groq":        {&p.Groq.STTModel, &p.Groq.UtilityModel, &p.Groq.AssistModel, &p.Groq.AgentModel},
		"google":      {&p.Google.UtilityModel, &p.Google.AssistModel, &p.Google.AgentModel},
		"openrouter":  {&p.OpenRouter.STTModel, &p.OpenRouter.UtilityModel, &p.OpenRouter.AssistModel, &p.OpenRouter.AgentModel},
		"huggingface": {&cfg.HuggingFace.Model, &cfg.HuggingFace.UtilityModel, &cfg.HuggingFace.AssistModel, &cfg.HuggingFace.AgentModel},
		"assemblyai-llm-gateway": {
			&p.AssemblyAI.LLMGatewayUtilityModel, &p.AssemblyAI.LLMGatewayAssistModel, &p.AssemblyAI.LLMGatewayAgentModel,
		},
		"foundry-voicelive": {&p.Foundry.VoiceLiveModel},
	} {
		for _, field := range fields {
			migrateRetiredModelField(provider, field)
		}
	}
	for _, field := range []*string{&cfg.VoiceAgent.Model, &cfg.VoiceAgent.FallbackModel} {
		for _, provider := range retiredVoiceAgentModelProviders(cfg) {
			if migrateRetiredModelField(provider, field) {
				break
			}
		}
	}
	for _, id := range []*string{&cfg.ModelSelection.TTS.PrimaryProfileID, &cfg.ModelSelection.TTS.FallbackProfileID} {
		if current := framework.NormalizeProviderProfileID(*id); current != *id {
			slog.Info("config: retired profile migrated", "from", *id, "to", current)
			*id = current
		}
	}
}

// retiredVoiceAgentModelProviders names the providers whose retirements apply
// to [voice_agent].model: the configured realtime provider, or for the
// pipeline fallback (no realtime provider) the dialogue-model providers whose
// ids that field then carries.
func retiredVoiceAgentModelProviders(cfg *Config) []string {
	switch NormalizeVoiceAgentProviderName(cfg.VoiceAgent.Provider) {
	case "gemini":
		return []string{"google"}
	case "openai":
		return []string{"openai"}
	case "foundry-voicelive":
		return []string{"foundry-voicelive"}
	case "", "cascaded":
		return []string{"huggingface", "openrouter", "groq", "openai"}
	default:
		return nil
	}
}

func migrateRetiredModelField(provider string, field *string) bool {
	if field == nil {
		return false
	}
	replacement, ok := catalog.ReplacementModelID(provider, strings.TrimSpace(*field))
	if !ok {
		return false
	}
	slog.Info("config: retired model migrated", "provider", provider, "from", *field, "to", replacement)
	*field = replacement
	return true
}

// Foundry deployment names that were the defaults before the 2026-09-30
// switch to GPT-6 and gpt-realtime-2.1.
const (
	legacyFoundryUtilityDeployment  = "gpt-5.6-luna"
	legacyFoundryAssistDeployment   = "gpt-5.6-terra"
	legacyFoundryRealtimeDeployment = "gpt-realtime-2"
)

// pinLegacyFoundryDeployments keeps an existing Foundry setup on the
// deployment names it was already calling. Foundry deployments live in the
// user's own project and an empty field floats to the server default, so
// moving the defaults to GPT-6 and gpt-realtime-2.1 would point an upgraded
// install at deployments it never created (Azure upgrades a gpt-realtime-2
// deployment in place, but its name stays). Only configs saved with Foundry
// enabled are pinned; new installs take the new defaults.
func pinLegacyFoundryDeployments(meta toml.MetaData, cfg *Config) {
	if cfg == nil || !cfg.Providers.Foundry.Enabled {
		return
	}
	f := &cfg.Providers.Foundry
	for _, pin := range []struct {
		key    string
		field  *string
		legacy string
	}{
		{"utility_deployment", &f.UtilityDeployment, legacyFoundryUtilityDeployment},
		{"assist_deployment", &f.AssistDeployment, legacyFoundryAssistDeployment},
		{"agent_deployment", &f.AgentDeployment, legacyFoundryAssistDeployment},
		{"realtime_deployment", &f.RealtimeDeployment, legacyFoundryRealtimeDeployment},
	} {
		if meta.IsDefined("providers", "foundry", pin.key) && strings.TrimSpace(*pin.field) != "" {
			continue
		}
		slog.Info("config: foundry deployment pinned to the name this install already used", "field", pin.key, "deployment", pin.legacy)
		*pin.field = pin.legacy
	}
}
