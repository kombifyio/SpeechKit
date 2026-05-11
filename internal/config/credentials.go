package config

import (
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/secrets"
)

const (
	GoogleAIAPIKeyEnv         = "GOOGLE_AI_API_KEY"
	GoogleSTTDefaultAPIKeyEnv = "SPEECHKIT_GOOGLE_STT_API_KEY"
	GoogleCloudSTTAPIKeyEnv   = "GOOGLE_CLOUD_STT_API_KEY"
	GoogleLegacySTTAPIKeyEnv  = "GOOGLE_STT_API_KEY"
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

func ResolveGoogleSTTKey(cfg *Config) (string, string) {
	for _, envName := range googleSTTKeyEnvCandidates(cfg) {
		if key := strings.TrimSpace(ResolveSecret(envName)); key != "" {
			return key, envName
		}
	}
	return "", ""
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
		cfg.HuggingFace.Model = "openai/whisper-large-v3"
	}
	return true
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
