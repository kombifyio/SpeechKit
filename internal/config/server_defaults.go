package config

import (
	"os"
	"strings"
)

const (
	ServerSelfHostedDefaultsEnv = "SPEECHKIT_SELFHOSTED_DEFAULTS"

	defaultServerSTTURL     = "http://speechkit-whisper:8080"
	defaultServerSTTModel   = "whisper-1"
	defaultServerLLMBaseURL = "http://speechkit-llm:8080/v1"
	defaultServerLLMModel   = DefaultLocalLLMModel
	defaultServerSQLitePath = "/var/lib/speechkit/data/speechkit.db"

	serverPublicURLEnv         = "SPEECHKIT_PUBLIC_URL"
	serverPublicURLAliasEnv    = "SPEECHKIT_SERVER_PUBLIC_URL"
	serverPostgresDSNEnv       = "POSTGRES_DSN"
	serverSpeechKitPostgresEnv = "SPEECHKIT_POSTGRES_DSN"
	serverLiveKitURLEnv        = "LIVEKIT_URL"
)

// ApplyServerRuntimeDefaults turns the standalone Linux Server-Target into a
// working self-hosted deployment when SPEECHKIT_SELFHOSTED_DEFAULTS is set.
// Desktop code never calls this; it is intentionally opt-in for server
// containers that ship local STT/LLM sidecars.
func ApplyServerRuntimeDefaults(cfg *Config) []string {
	if cfg == nil {
		return nil
	}

	var notes []string

	if publicURL, envName := firstPresentEnv(serverPublicURLEnv, serverPublicURLAliasEnv); publicURL != "" && strings.TrimSpace(cfg.Server.PublicURL) == "" {
		cfg.Server.PublicURL = strings.TrimRight(publicURL, "/")
		notes = append(notes, "server public URL default from "+envName+": "+cfg.Server.PublicURL)
	}

	if postgresDSN, envName := firstPresentEnv(serverSpeechKitPostgresEnv, serverPostgresDSNEnv); postgresDSN != "" {
		if strings.TrimSpace(cfg.Store.PostgresDSN) == "" {
			cfg.Store.PostgresDSN = postgresDSN
			notes = append(notes, "server store postgres DSN default from "+envName)
		}
		backend := strings.ToLower(strings.TrimSpace(cfg.Store.Backend))
		if backend == "" || backend == "sqlite" || backend == "postgres" {
			cfg.Store.Backend = "postgres"
			cfg.Store.SQLitePath = ""
		}
	}

	if strings.TrimSpace(cfg.Server.LiveKit.APIKeyEnv) == "" {
		cfg.Server.LiveKit.APIKeyEnv = "LIVEKIT_API_KEY"
	}
	if strings.TrimSpace(cfg.Server.LiveKit.APISecretEnv) == "" {
		cfg.Server.LiveKit.APISecretEnv = "LIVEKIT_API_SECRET"
	}
	if cfg.Server.LiveKit.TokenTTLSec <= 0 {
		cfg.Server.LiveKit.TokenTTLSec = 600
	}
	if strings.TrimSpace(cfg.Server.LiveKit.RoomPrefix) == "" {
		cfg.Server.LiveKit.RoomPrefix = "speechkit-va"
	}
	if liveKitURL := strings.TrimRight(strings.TrimSpace(os.Getenv(serverLiveKitURLEnv)), "/"); liveKitURL != "" && strings.TrimSpace(cfg.Server.LiveKit.URL) == "" {
		cfg.Server.LiveKit.URL = liveKitURL
		notes = append(notes, "server LiveKit URL default from "+serverLiveKitURLEnv)
	}
	if strings.TrimSpace(cfg.Server.LiveKit.URL) != "" &&
		envPresent(cfg.Server.LiveKit.APIKeyEnv) &&
		envPresent(cfg.Server.LiveKit.APISecretEnv) &&
		!cfg.Server.LiveKit.Enabled {
		cfg.Server.LiveKit.Enabled = true
		notes = append(notes, "server LiveKit token minting enabled from runtime env")
	}

	if !parseManagedBool(os.Getenv(ServerSelfHostedDefaultsEnv)) {
		return notes
	}

	sttURL := envOrDefault("SPEECHKIT_SELFHOSTED_STT_URL", defaultServerSTTURL)
	if strings.TrimSpace(cfg.VPS.URL) == "" {
		cfg.VPS.URL = sttURL
		notes = append(notes, "self-hosted STT default: "+sttURL)
	}
	if !cfg.VPS.Enabled {
		cfg.VPS.Enabled = true
	}
	if strings.TrimSpace(cfg.VPS.Model) == "" {
		cfg.VPS.Model = envOrDefault("SPEECHKIT_SELFHOSTED_STT_MODEL", defaultServerSTTModel)
	}
	if strings.TrimSpace(cfg.VPS.APIKeyEnv) == "" || strings.TrimSpace(cfg.VPS.APIKeyEnv) == "VPS_API_KEY" {
		cfg.VPS.APIKeyEnv = "SPEECHKIT_SELFHOSTED_STT_API_KEY"
	}
	// The kernel's default config Strategy is "local-only" because the
	// Device-Target embeds an in-process whisper.cpp Local provider. The
	// Server-Target has no such Local provider — it reaches whisper.cpp
	// over the network via the VPS-shaped client at SPEECHKIT_SELFHOSTED_
	// STT_URL. Force Strategy to cloud-only here (not just default-when-
	// empty) so a fresh self-hosted install routes through the VPS
	// provider instead of failing with "local provider not configured".
	switch strings.TrimSpace(strings.ToLower(cfg.Routing.Strategy)) {
	case "", "local-only":
		cfg.Routing.Strategy = "cloud-only"
		notes = append(notes, "self-hosted routing: forced strategy=cloud-only (server reaches whisper.cpp via VPS endpoint)")
	}

	llmBaseURL := envOrDefault("SPEECHKIT_SELFHOSTED_LLM_BASE_URL", defaultServerLLMBaseURL)
	if !cfg.LocalLLM.Enabled {
		cfg.LocalLLM.Enabled = true
	}
	if localLLMBaseURLNeedsDefault(cfg.LocalLLM.BaseURL) {
		cfg.LocalLLM.BaseURL = llmBaseURL
		notes = append(notes, "self-hosted LLM default: "+llmBaseURL)
	}
	applyServerLocalLLMModelDefaults(&cfg.LocalLLM)

	if strings.TrimSpace(cfg.Store.Backend) == "" {
		cfg.Store.Backend = "sqlite"
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "sqlite") && strings.TrimSpace(cfg.Store.SQLitePath) == "" {
		cfg.Store.SQLitePath = defaultServerSQLitePath
	}

	if noConfiguredTTSSecret(cfg) {
		cfg.TTS.Enabled = false
		notes = append(notes, "TTS disabled until a speech provider credential is configured")
	}

	if shouldUseCascadedVoiceAgent(cfg) {
		cfg.VoiceAgent.Provider = "cascaded"
		notes = append(notes, "Voice Agent default: cascaded self-hosted provider")
	}

	if len(cfg.Personas) == 0 {
		cfg.Personas = append(cfg.Personas, PersonaConfig{
			ID:          "default",
			DisplayName: "Default Assistant",
			Description: "Concise general-purpose assistant for server smoke tests and first-run setup.",
			Voice:       "Kore",
			Locale:      "en",
			DefaultRole: "default-role",
			Tags:        []string{"default", "server"},
		})
	}
	if len(cfg.Roles) == 0 {
		cfg.Roles = append(cfg.Roles, RoleConfig{
			ID:                         "default-role",
			DisplayName:                "Default Behavior",
			SystemPrompt:               "You are a helpful voice assistant. Be concise and answer in the user's language.",
			Locale:                     "en",
			Temperature:                0.6,
			AutomaticActivityDetection: true,
			VADStartSensitivity:        "medium",
			VADEndSensitivity:          "medium",
		})
	}

	return notes
}

func applyServerLocalLLMModelDefaults(cfg *LocalLLMConfig) {
	if cfg == nil {
		return
	}
	model := normalizeServerLLMModel(envOrDefault("SPEECHKIT_SELFHOSTED_LLM_MODEL", defaultServerLLMModel))
	if localLLMModelNeedsDefault(cfg.Model) {
		cfg.Model = model
	}
	if localLLMModelNeedsDefault(cfg.UtilityModel) {
		cfg.UtilityModel = model
	}
	if localLLMModelNeedsDefault(cfg.AssistModel) {
		cfg.AssistModel = model
	}
	if localLLMModelNeedsDefault(cfg.AgentModel) {
		cfg.AgentModel = model
	}
}

func localLLMBaseURLNeedsDefault(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == DefaultLocalLLMBaseURL
}

func localLLMModelNeedsDefault(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "gemma4:e4b" || value == "speechkit-gemma4"
}

func normalizeServerLLMModel(value string) string {
	if localLLMModelNeedsDefault(value) {
		return defaultServerLLMModel
	}
	return strings.TrimSpace(value)
}

func shouldUseCascadedVoiceAgent(cfg *Config) bool {
	provider := strings.ToLower(strings.TrimSpace(cfg.VoiceAgent.Provider))
	if provider != "" && provider != "gemini" {
		return false
	}
	googleEnv := strings.TrimSpace(cfg.Providers.Google.APIKeyEnv)
	if googleEnv == "" {
		googleEnv = GoogleAIAPIKeyEnv
	}
	return strings.TrimSpace(os.Getenv(googleEnv)) == ""
}

func noConfiguredTTSSecret(cfg *Config) bool {
	if cfg == nil || !cfg.TTS.Enabled {
		return false
	}
	if cfg.TTS.OpenAI.Enabled && envPresent(cfg.Providers.OpenAI.APIKeyEnv) {
		return false
	}
	if cfg.TTS.Google.Enabled && envPresent(cfg.Providers.Google.APIKeyEnv) {
		return false
	}
	if cfg.TTS.HuggingFace.Enabled && envPresent(HuggingFaceTokenEnvName(cfg)) {
		return false
	}
	return true
}

func envPresent(name string) bool {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(name))) != ""
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func firstPresentEnv(names ...string) (string, string) {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, name
		}
	}
	return "", ""
}
