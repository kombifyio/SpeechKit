package config

import (
	"os"
	"strings"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
)

func cleanSetting(value string) string {
	return strings.TrimSpace(value)
}

func normalizeServerMultiline(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func normalizeServerStringList(values []string) []string {
	if values == nil {
		return nil
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func modeSettingDefined(mode ServerModeSetting) bool {
	return mode.Enabled != nil ||
		cleanSetting(mode.ProviderKind) != "" ||
		cleanSetting(mode.ProfileID) != "" ||
		cleanSetting(mode.Model) != ""
}

func normalizedProviderKind(mode ServerModeSetting) string {
	if kind := strings.ToLower(cleanSetting(mode.ProviderKind)); kind != "" {
		return kind
	}
	return providerKindForProfileID(mode.ProfileID)
}

func providerKindForProfileID(profileID string) string {
	profileID = framework.NormalizeProviderProfileID(cleanSetting(profileID))
	if profileID == "" {
		return ""
	}
	for _, profile := range catalog.DefaultProviderProfiles() {
		if profile.ID == profileID {
			return string(profile.ProviderKind)
		}
	}
	switch providerForProfileID(profileID) {
	case "local":
		return "local_built_in"
	case "ollama", "openedai", "selfhosted":
		return "local_provider"
	case "huggingface", "openrouter":
		return "cloud_provider"
	case "":
		return ""
	default:
		return "direct_provider"
	}
}

func validServerProviderKind(value string) bool {
	switch strings.ToLower(cleanSetting(value)) {
	case "", "local_built_in", "local_provider", "cloud_provider", "direct_provider":
		return true
	default:
		return false
	}
}

func validEnvName(value string) bool {
	value = cleanSetting(value)
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validServerToolID(value string) bool {
	value = cleanSetting(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func setCredentialEnv(envName, fallback, value string) {
	envName = cleanSetting(envName)
	if envName == "" {
		envName = fallback
	}
	_ = os.Setenv(envName, value)
}
