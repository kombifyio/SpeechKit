package config

import (
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/secrets"
)

const (
	GoogleAIAPIKeyEnv               = "GOOGLE_AI_API_KEY"
	GoogleSTTDefaultAPIKeyEnv       = "SPEECHKIT_GOOGLE_STT_API_KEY"
	GoogleSTTCredentialsJSONEnv     = "SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON"
	GoogleApplicationCredentialsEnv = "GOOGLE_APPLICATION_CREDENTIALS"
	GoogleCloudSTTAPIKeyEnv         = "GOOGLE_CLOUD_STT_API_KEY"
	GoogleLegacySTTAPIKeyEnv        = "GOOGLE_STT_API_KEY"
	DeepgramAPIKeyEnv               = "DEEPGRAM_API_KEY"
	AssemblyAIAPIKeyEnv             = "ASSEMBLYAI_API_KEY"
)

var (
	dopplerLookPath              = exec.LookPath
	dopplerSecretLookup          = secrets.DefaultDopplerSecretLookup
	managedHFBuildEnabled        string
	managedHFDefaultOptIn        string
	managedDopplerDefaultProject string
	managedDopplerDefaultConfig  string
	readBuildInfo                = defaultReadBuildInfo
)

type buildInfo struct {
	MainPath string
}

// ResolveSecret resolves a secret by name. Checks environment first, then Doppler CLI
// using either explicit DOPPLER_PROJECT/DOPPLER_CONFIG env vars or build-embedded
// managed Doppler defaults.
func ResolveSecret(envName string) string {
	if strings.TrimSpace(envName) == "" {
		return ""
	}
	value, _, err := secrets.ResolveNamedSecret(envName, func() string {
		return ResolveSecretFromEnvironmentOrDoppler(envName)
	})
	if err == nil && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return ResolveSecretFromEnvironmentOrDoppler(envName)
}

func ResolveSecretFromEnvironmentOrDoppler(envName string) string {
	if v := os.Getenv(envName); v != "" {
		return v
	}
	return dopplerGet(envName)
}

func HuggingFaceTokenEnvName(cfg *Config) string {
	if cfg == nil {
		return "HF_TOKEN"
	}
	if tokenEnv := strings.TrimSpace(cfg.HuggingFace.TokenEnv); tokenEnv != "" {
		return tokenEnv
	}
	return "HF_TOKEN"
}

func HuggingFaceTokenStatus(cfg *Config) (secrets.TokenStatus, error) {
	tokenEnv := HuggingFaceTokenEnvName(cfg)
	return secrets.HuggingFaceTokenStatus(func() string {
		return ResolveSecretFromEnvironmentOrDoppler(tokenEnv)
	})
}

func ResolveHuggingFaceToken(cfg *Config) (string, secrets.TokenStatus, error) {
	tokenEnv := HuggingFaceTokenEnvName(cfg)
	return secrets.ResolveHuggingFaceToken(func() string {
		return ResolveSecretFromEnvironmentOrDoppler(tokenEnv)
	})
}

func GoogleSTTAPIKeyEnvName(cfg *Config) string {
	if cfg == nil {
		return GoogleSTTDefaultAPIKeyEnv
	}
	if envName := strings.TrimSpace(cfg.Providers.Google.STTAPIKeyEnv); envName != "" {
		return envName
	}
	return GoogleSTTDefaultAPIKeyEnv
}

func GoogleSTTCredentialsJSONEnvName(cfg *Config) string {
	if cfg == nil {
		return GoogleSTTCredentialsJSONEnv
	}
	if envName := strings.TrimSpace(cfg.Providers.Google.STTCredentialsJSONEnv); envName != "" {
		return envName
	}
	return GoogleSTTCredentialsJSONEnv
}

func GoogleApplicationCredentialsEnvName(cfg *Config) string {
	if cfg == nil {
		return GoogleApplicationCredentialsEnv
	}
	if envName := strings.TrimSpace(cfg.Providers.Google.ApplicationCredentialsEnv); envName != "" {
		return envName
	}
	return GoogleApplicationCredentialsEnv
}

func ResolveGoogleSTTKey(cfg *Config) (string, string) {
	for _, envName := range googleSTTKeyEnvCandidates(cfg) {
		if key := strings.TrimSpace(ResolveSecret(envName)); key != "" {
			return key, envName
		}
	}
	return "", ""
}

func ResolveDeepgramKey(cfg *Config) (string, string) {
	envName := DeepgramAPIKeyEnv
	if cfg != nil && strings.TrimSpace(cfg.Providers.Deepgram.APIKeyEnv) != "" {
		envName = strings.TrimSpace(cfg.Providers.Deepgram.APIKeyEnv)
	}
	if key := strings.TrimSpace(ResolveSecret(envName)); key != "" {
		return key, envName
	}
	return "", envName
}

func ResolveAssemblyAIKey(cfg *Config) (string, string) {
	envName := AssemblyAIAPIKeyEnv
	if cfg != nil && strings.TrimSpace(cfg.Providers.AssemblyAI.APIKeyEnv) != "" {
		envName = strings.TrimSpace(cfg.Providers.AssemblyAI.APIKeyEnv)
	}
	if key := strings.TrimSpace(ResolveSecret(envName)); key != "" {
		return key, envName
	}
	return "", envName
}

// ResolveDeepgramThinkKey resolves the bring-your-own think-LLM credential for
// the Deepgram Voice Agent from the env var named in
// [voice_agent].deepgram_think_api_key_env. It returns ("", "") when no env
// name is configured — Deepgram's managed think LLM needs no client-supplied
// key, so the absence of a configured env var is the normal managed case, not
// an error.
func ResolveDeepgramThinkKey(cfg *Config) (string, string) {
	if cfg == nil {
		return "", ""
	}
	envName := strings.TrimSpace(cfg.VoiceAgent.DeepgramThinkAPIKeyEnv)
	if envName == "" {
		return "", ""
	}
	if key := strings.TrimSpace(ResolveSecret(envName)); key != "" {
		return key, envName
	}
	return "", envName
}

// DeepgramThinkSettings holds the resolved Deepgram Voice Agent think-LLM
// parameters for the Server- and Device-Target wiring to apply to the kernel
// provider via DeepgramLive.ConfigureThink. APIKey is already resolved from the
// configured env var and is empty in managed-LLM mode.
type DeepgramThinkSettings struct {
	Provider    string
	Model       string
	EndpointURL string
	APIKey      string
}

// DeepgramThinkConfig resolves the Deepgram Voice Agent think-LLM settings from
// config. Model precedence: an explicit deepgram_think_model wins; otherwise a
// non-Gemini [voice_agent].model is reused as the think model (preserving prior
// behavior). Two classes of [voice_agent].model ids are ignored so they can't
// pin a non-existent Deepgram think model: Gemini realtime ids, and Deepgram
// listen/speak audio ids such as the catalog composite "nova-3+aura-2". The
// bring-your-own credential is resolved only when an endpoint URL is configured
// (managed LLMs need no client-supplied key).
func (cfg *Config) DeepgramThinkConfig() DeepgramThinkSettings {
	if cfg == nil {
		return DeepgramThinkSettings{}
	}
	out := DeepgramThinkSettings{
		Provider: strings.TrimSpace(cfg.VoiceAgent.DeepgramThinkProvider),
		Model:    strings.TrimSpace(cfg.VoiceAgent.DeepgramThinkModel),
	}
	if out.Model == "" {
		if m := strings.TrimSpace(cfg.VoiceAgent.Model); m != "" &&
			!strings.Contains(strings.ToLower(m), "gemini") &&
			!isDeepgramAudioModelID(m) {
			out.Model = m
		}
	}
	if url := strings.TrimSpace(cfg.VoiceAgent.DeepgramThinkEndpointURL); url != "" {
		out.EndpointURL = url
		if key, _ := ResolveDeepgramThinkKey(cfg); key != "" {
			out.APIKey = key
		}
	}
	return out
}

// isDeepgramAudioModelID reports whether a [voice_agent].model value names
// Deepgram listen/speak audio models (Nova STT, Aura TTS, or a "listen+speak"
// composite like "nova-3+aura-2") rather than a think-capable LLM.
func isDeepgramAudioModelID(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(lower, "+") {
		return true
	}
	return strings.HasPrefix(lower, "nova") || strings.HasPrefix(lower, "aura")
}

func googleSTTKeyEnvCandidates(cfg *Config) []string {
	candidates := []string{GoogleSTTAPIKeyEnvName(cfg), GoogleCloudSTTAPIKeyEnv, GoogleLegacySTTAPIKeyEnv}
	if cfg != nil {
		envName := strings.TrimSpace(cfg.Providers.Google.APIKeyEnv)
		if envName != "" && envName != GoogleAIAPIKeyEnv {
			candidates = append(candidates, envName)
		}
	}

	out := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

// dopplerGet tries to resolve a secret via `doppler secrets get` CLI.
func dopplerGet(key string) string {
	dopplerPath := findDopplerExecutable()
	if dopplerPath == "" {
		return ""
	}

	projects := dopplerProjects()
	configs := dopplerConfigs()
	if len(projects) == 0 || len(configs) == 0 {
		return ""
	}

	for _, project := range projects {
		for _, cfg := range configs {
			v, err := dopplerSecretLookup(dopplerPath, key, project, cfg)
			if err == nil && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

func findDopplerExecutable() string {
	return secrets.FindDopplerExecutable(dopplerLookPath)
}

func dopplerProjects() []string {
	if rawProject, ok := os.LookupEnv("DOPPLER_PROJECT"); ok {
		if project := strings.TrimSpace(rawProject); project != "" {
			return []string{project}
		}
		return nil
	}
	if project := strings.TrimSpace(managedDopplerDefaultProject); project != "" {
		return []string{project}
	}
	return nil
}

func dopplerConfigs() []string {
	if rawConfig, ok := os.LookupEnv("DOPPLER_CONFIG"); ok {
		if cfg := strings.TrimSpace(rawConfig); cfg != "" {
			return []string{cfg}
		}
		return nil
	}
	if cfg := strings.TrimSpace(managedDopplerDefaultConfig); cfg != "" {
		return []string{cfg}
	}
	return nil
}

func resetDopplerHooksForTests() {
	dopplerLookPath = exec.LookPath
	dopplerSecretLookup = secrets.DefaultDopplerSecretLookup
}

func ApplyManagedIntegrationDefaults(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	if !ManagedHuggingFaceAvailableInBuild() {
		cfg.HuggingFace.Enabled = false
		return false
	}

	if !managedHFOptInEnabled() {
		return false
	}

	if cfg.HuggingFace.Enabled || cfg.VPS.Enabled || cfg.Local.Enabled {
		return false
	}

	if cfg.Routing.Strategy != "cloud-only" {
		return false
	}

	tokenEnv := HuggingFaceTokenEnvName(cfg)
	cfg.HuggingFace.TokenEnv = tokenEnv

	token, _, err := ResolveHuggingFaceToken(cfg)
	if err != nil || token == "" {
		return false
	}

	cfg.HuggingFace.Enabled = true
	if strings.TrimSpace(cfg.HuggingFace.Model) == "" {
		cfg.HuggingFace.Model = "openai/whisper-large-v3-turbo"
	}
	return true
}

func ApplyManagedDevServerDefaults(cfg *Config) bool {
	// Server targets must be explicit user/operator configuration. Local
	// Kombify development targets are injected by dev scripts only, never by
	// the Windows client at startup or by build defaults.
	return false
}

func managedHFOptInEnabled() bool {
	if raw, ok := os.LookupEnv("SPEECHKIT_ENABLE_MANAGED_HF"); ok {
		return parseManagedBool(raw)
	}
	if strings.TrimSpace(managedHFDefaultOptIn) != "" {
		return parseManagedBool(managedHFDefaultOptIn)
	}
	return defaultManagedHuggingFaceForModule()
}

func ManagedHuggingFaceAvailableInBuild() bool {
	if strings.TrimSpace(managedHFBuildEnabled) != "" {
		return parseManagedBool(managedHFBuildEnabled)
	}
	return defaultManagedHuggingFaceForModule()
}

func OverrideManagedHuggingFaceBuildForTests(value string) func() {
	previous := managedHFBuildEnabled
	managedHFBuildEnabled = value
	return func() {
		managedHFBuildEnabled = previous
	}
}

func parseManagedBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func defaultReadBuildInfo() (buildInfo, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return buildInfo{}, false
	}
	return buildInfo{MainPath: strings.TrimSpace(info.Main.Path)}, true
}

func defaultManagedHuggingFaceForModule() bool {
	return defaultManagedPrivateFeatureForModule()
}

func defaultManagedPrivateFeatureForModule() bool {
	info, ok := readBuildInfo()
	if !ok {
		return false
	}
	mainPath := strings.TrimSpace(info.MainPath)
	if mainPath == privateModulePath() {
		return true
	}
	if mainPath == "github.com/kombifyio/SpeechKit" {
		return false
	}
	return false
}

func privateModulePath() string {
	return "github.com/" + "Soulcreek" + "/kombify-SpeechKit"
}
