package config

import (
	"net/url"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/phonon"
)

const (
	ServerSelfHostedDefaultsEnv = "SPEECHKIT_SELFHOSTED_DEFAULTS"

	defaultServerSTTURL     = "http://speechkit-whisper:8080"
	defaultServerSTTModel   = "whisper-1"
	defaultServerLLMBaseURL = "http://speechkit-llm:8080/v1"
	defaultServerLLMModel   = DefaultLocalLLMModel
	defaultServerSQLitePath = "/var/lib/speechkit/data/speechkit.db"
	// bundledServerSTTModelDir is where deploy/docker/Dockerfile.server puts
	// Phonon-2.5, outside the models volume so a mounted volume cannot hide it.
	bundledServerSTTModelDir = "/usr/local/share/speechkit/models/phonon-2.5"

	serverPublicURLEnv         = "SPEECHKIT_PUBLIC_URL"
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

	// The built-in "local-only" strategy is the desktop's, which embeds a
	// local engine. A server config that sets no strategy and no [local]
	// model reaches its providers over the network, so its absent-key default
	// is cloud-only; the self-hosted default below may still choose the
	// bundled local model. A strategy the file sets is never rewritten.
	if !cfg.keyDefined("routing.strategy") && strings.TrimSpace(cfg.Local.ModelPath) == "" &&
		strings.EqualFold(strings.TrimSpace(cfg.Routing.Strategy), "local-only") {
		cfg.Routing.Strategy = "cloud-only"
	}

	if publicURL, envName := firstPresentEnv(serverPublicURLEnv); publicURL != "" && strings.TrimSpace(cfg.Server.PublicURL) == "" {
		cfg.Server.PublicURL = strings.TrimRight(publicURL, "/")
		notes = append(notes, "server public URL default from "+envName+": "+LogSafeURL(cfg.Server.PublicURL))
	}
	if strings.TrimSpace(os.Getenv("SPEECHKIT_SERVER_PUBLIC_URL")) != "" {
		notes = append(notes, "SPEECHKIT_SERVER_PUBLIC_URL was removed; use SPEECHKIT_PUBLIC_URL")
	}

	// Same-origin browser smoke + Voice Agent WebSocket: if PublicURL is set
	// but CORSAllowedOrigins doesn't already include it, fold it in. The
	// alternative (operators noticing 403 only when the WS handshake fails
	// in a browser) is exactly what shipped broken in v0.35.4 and required
	// a hotfix. Auto-deriving here keeps OSS deployments behind a public
	// origin working without operator intervention.
	// A browser Origin is scheme://host[:port] and never carries a path, so a
	// PublicURL such as https://api.kombify.io/v1/speechkit contributes only
	// its origin.
	if origin := originFromURL(cfg.Server.PublicURL); origin != "" {
		if added := appendUniqueOrigin(&cfg.Server.CORSAllowedOrigins, origin); added {
			notes = append(notes, "server CORS allow-list extended with PublicURL: "+origin)
		}
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

	if bundled, note := bundledServerSTTDefault(cfg); bundled {
		notes = append(notes, note)
	} else {
		if note != "" {
			notes = append(notes, note)
		}
		notes = append(notes, applyServerVPSSTTDefaults(cfg)...)
	}

	llmBaseURL := envOrDefault("SPEECHKIT_SELFHOSTED_LLM_BASE_URL", defaultServerLLMBaseURL)
	if !cfg.LocalLLM.Enabled {
		cfg.LocalLLM.Enabled = true
	}
	if localLLMBaseURLNeedsDefault(cfg.LocalLLM.BaseURL) {
		cfg.LocalLLM.BaseURL = llmBaseURL
		notes = append(notes, "self-hosted LLM default: "+LogSafeURL(llmBaseURL))
	}
	applyServerLocalLLMModelDefaults(&cfg.LocalLLM)

	if strings.TrimSpace(cfg.Store.Backend) == "" {
		cfg.Store.Backend = "sqlite"
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Store.Backend), "sqlite") && strings.TrimSpace(cfg.Store.SQLitePath) == "" {
		cfg.Store.SQLitePath = defaultServerSQLitePath
	}

	// A self-hosted OpenAI-compatible TTS sidecar (Kokoro-FastAPI, Speaches)
	// gives the keyless install spoken output.
	if ttsURL := strings.TrimSpace(os.Getenv("SPEECHKIT_SELFHOSTED_TTS_URL")); ttsURL != "" && strings.TrimSpace(cfg.TTS.Local.URL) == "" {
		cfg.TTS.Enabled = true
		cfg.TTS.Local.Enabled = true
		cfg.TTS.Local.URL = ttsURL
		if voice := strings.TrimSpace(os.Getenv("SPEECHKIT_SELFHOSTED_TTS_VOICE")); voice != "" && strings.TrimSpace(cfg.TTS.Local.Voice) == "" {
			cfg.TTS.Local.Voice = voice
		}
		notes = append(notes, "self-hosted TTS default: "+LogSafeURL(ttsURL))
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

// bundledServerSTTDefault makes the server image's bundled Phonon-2.5 the
// self-hosted STT when nothing else is configured: no
// SPEECHKIT_SELFHOSTED_STT_URL, no [vps] url, no [local] model and no cloud
// credential in the environment. The server then runs speechkit-stt-sherpa on
// it (core.startLocalSTT), so a fresh install dictates with no STT container
// and no key. An explicit operator choice always wins: a config file that sets
// [local] enabled or model_path, or a routing strategy the local model cannot
// serve (cloud-only), keeps the whisper-server default, and a strategy the file
// sets is never rewritten. It reports false, with the reason in note, when the
// image has no bundled model, the processor cannot run it (Phonon needs
// x86-64-v3 or ARM dot product) or the full_local data profile is bound (it
// admits whisper.cpp only).
func bundledServerSTTDefault(cfg *Config) (bool, string) {
	if envPresent("SPEECHKIT_SELFHOSTED_STT_URL") || strings.TrimSpace(cfg.VPS.URL) != "" {
		return false, ""
	}
	if cfg.keyDefined("local.enabled") || cfg.keyDefined("local.model_path") || strings.TrimSpace(cfg.Local.ModelPath) != "" {
		return false, ""
	}
	strategy := strings.TrimSpace(strings.ToLower(cfg.Routing.Strategy))
	if cfg.keyDefined("routing.strategy") && strategy != "local-only" && strategy != "dynamic" {
		return false, "self-hosted STT: bundled Phonon-2.5 not used because config.toml sets [routing] strategy = \"" + cfg.Routing.Strategy + "\""
	}
	for _, name := range CloudCredentialEnvNames(cfg) {
		if envPresent(name) {
			return false, ""
		}
	}
	dir := envOrDefault("SPEECHKIT_SELFHOSTED_STT_MODEL_DIR", bundledServerSTTModelDir)
	if !phonon.IsModelDir(dir) {
		return false, ""
	}
	if netsec.FullLocal() {
		return false, "self-hosted STT: bundled Phonon-2.5 not used under the full_local data profile, which admits whisper.cpp only; using the whisper-server default (SPEECHKIT_SELFHOSTED_STT_URL)"
	}
	if !phonon.CPUSupported() {
		return false, "self-hosted STT: bundled Phonon-2.5 skipped (this processor lacks AVX2/x86-64-v3); using the whisper-server default (SPEECHKIT_SELFHOSTED_STT_URL)"
	}
	cfg.Local.Enabled = true
	cfg.Local.ModelPath = dir
	cfg.Local.Port = 0
	if !cfg.keyDefined("routing.strategy") {
		cfg.Routing.Strategy = "local-only"
	}
	return true, "self-hosted STT default: bundled Phonon-2.5 (" + dir + "), strategy=" + cfg.Routing.Strategy
}

// applyServerVPSSTTDefaults points the self-hosted STT at an
// OpenAI-compatible whisper-server (SPEECHKIT_SELFHOSTED_STT_URL, default the
// compose sidecar) when the image's bundled model is not the default.
func applyServerVPSSTTDefaults(cfg *Config) []string {
	var notes []string
	sttURL := envOrDefault("SPEECHKIT_SELFHOSTED_STT_URL", defaultServerSTTURL)
	if strings.TrimSpace(cfg.VPS.URL) == "" {
		cfg.VPS.URL = sttURL
		notes = append(notes, "self-hosted STT default: "+LogSafeURL(sttURL))
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
	// A strategy the config file sets explicitly is the operator's choice
	// and stays.
	switch strings.TrimSpace(strings.ToLower(cfg.Routing.Strategy)) {
	case "", "local-only":
		if cfg.keyDefined("routing.strategy") {
			break
		}
		cfg.Routing.Strategy = "cloud-only"
		notes = append(notes, "self-hosted routing: forced strategy=cloud-only (server reaches whisper.cpp via VPS endpoint)")
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

// shouldUseCascadedVoiceAgent reports whether a self-hosted deployment must
// fall back to the cascaded pipeline (local STT + LLM + TTS) because the
// configured realtime provider is a hosted one whose credential is missing.
// Without it a fresh local-only install would hand every Voice Agent session
// to a cloud provider that can only fail at upgrade.
func shouldUseCascadedVoiceAgent(cfg *Config) bool {
	credential := ""
	switch NormalizeVoiceAgentProviderName(cfg.VoiceAgent.Provider) {
	case "", "gemini":
		credential = "google"
	case "openai", "gpt-live":
		credential = "openai"
	case "deepgram":
		credential = "deepgram"
	case "assemblyai":
		credential = "assemblyai"
	case "xai":
		credential = "xai"
	default:
		// cascaded and kombify-agent need no cloud key; foundry-gpt-live
		// signs in through Entra, which an env check cannot see.
		return false
	}
	return !ProviderCredentialAvailable(cfg, credential)
}

func noConfiguredTTSSecret(cfg *Config) bool {
	if cfg == nil || !cfg.TTS.Enabled {
		return false
	}
	if cfg.TTS.OpenAI.Enabled && ProviderCredentialAvailable(cfg, "openai") {
		return false
	}
	if cfg.TTS.Google.Enabled && ProviderCredentialAvailable(cfg, "google") {
		return false
	}
	if cfg.TTS.HuggingFace.Enabled && ProviderCredentialAvailable(cfg, "huggingface") {
		return false
	}
	// xAI counts only once the deployment selected Grok speech output and
	// enabled xAI; a stored key alone must not keep TTS on for it.
	if cfg.Providers.XAI.Enabled && cfg.ModelSelection.TTS.NamesProvider("xai") && ProviderCredentialAvailable(cfg, "xai") {
		return false
	}
	// Local engines need no credential.
	if cfg.TTS.Local.Enabled && strings.TrimSpace(cfg.TTS.Local.URL) != "" {
		return false
	}
	if cfg.TTS.Piper.Enabled && strings.TrimSpace(cfg.TTS.Piper.VoiceDir) != "" {
		return false
	}
	if cfg.TTS.Sherpa.Enabled || strings.TrimSpace(cfg.ModelSelection.TTS.PrimaryProfileID) == DefaultTTSPrimaryProfileID {
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

// appendUniqueOrigin folds origin into *origins iff it is not already
// present (case-sensitive, exact match — origins are URL-shaped).
// Returns true when the slice was extended. A wildcard ("*") entry
// short-circuits: nothing further needs to be added.
func appendUniqueOrigin(origins *[]string, origin string) bool {
	if origins == nil {
		return false
	}
	target := strings.TrimSpace(origin)
	if target == "" {
		return false
	}
	for _, existing := range *origins {
		trimmed := strings.TrimSpace(existing)
		if trimmed == "*" || trimmed == target {
			return false
		}
	}
	*origins = append(*origins, target)
	return true
}

// originFromURL returns scheme://host[:port] for an absolute http(s) URL, or
// "" when the value is not one.
func originFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
