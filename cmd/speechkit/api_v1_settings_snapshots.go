package main

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// serverConnectionSettingFromConfig copies the [server_connection]
// config into the public surface, never including the bearer-token
// value itself (only the env var name + a "is the env var set" boolean
// so the UI can render a "missing token" warning).
func serverConnectionSettingFromConfig(cfg config.ServerConnectionConfig) speechkit.ServerConnectionSetting {
	tokenSet := serverConnectionTokenSet(cfg.BearerTokenEnv)
	targets := serverConnectionTargetsFromConfig(cfg)
	return speechkit.ServerConnectionSetting{
		Enabled:           cfg.Enabled,
		ActiveTargetID:    activeServerConnectionTargetID(cfg, targets),
		URL:               cfg.URL,
		BearerTokenEnv:    cfg.BearerTokenEnv,
		AuthMode:          config.NormalizeServerConnectionAuthMode(cfg.AuthMode),
		BearerTokenSet:    tokenSet,
		FallbackToLocal:   cfg.FallbackToLocal,
		RequestTimeoutSec: cfg.RequestTimeoutSec,
		Targets:           targets,
	}
}

func serverConnectionTargetsFromConfig(cfg config.ServerConnectionConfig) []speechkit.ServerConnectionTarget {
	registered := normalizeServerConnectionTargets(cfg.Targets)
	if len(registered) == 0 && strings.TrimSpace(cfg.URL) != "" {
		registered = []config.ServerConnectionTargetConfig{serverConnectionTargetFromActiveConfig(cfg)}
	}

	targets := make([]speechkit.ServerConnectionTarget, 0, len(registered))
	for _, target := range registered {
		targets = append(targets, speechkit.ServerConnectionTarget{
			ID:                target.ID,
			Label:             target.Label,
			URL:               target.URL,
			AuthMode:          config.NormalizeServerConnectionAuthMode(target.AuthMode),
			BearerTokenEnv:    target.BearerTokenEnv,
			BearerTokenSet:    serverConnectionTokenSet(target.BearerTokenEnv),
			FallbackToLocal:   target.FallbackToLocal,
			RequestTimeoutSec: normalizedServerConnectionTimeout(target.RequestTimeoutSec),
		})
	}
	return targets
}

func serverConnectionTokenSet(envName string) bool {
	env := strings.TrimSpace(envName)
	if env == "" {
		return false
	}
	return strings.TrimSpace(config.ResolveSecret(env)) != ""
}

func serverConnectionTargetFromActiveConfig(cfg config.ServerConnectionConfig) config.ServerConnectionTargetConfig {
	id := strings.TrimSpace(cfg.ActiveTargetID)
	if id == "" {
		id = serverConnectionTargetID("configured server", cfg.URL, 0)
	}
	label := strings.TrimSpace(serverConnectionLabelFromURL(cfg.URL))
	if label == "" {
		label = "Configured server"
	}
	return config.ServerConnectionTargetConfig{
		ID:                id,
		Label:             label,
		URL:               strings.TrimSpace(cfg.URL),
		BearerTokenEnv:    strings.TrimSpace(cfg.BearerTokenEnv),
		AuthMode:          config.NormalizeServerConnectionAuthMode(cfg.AuthMode),
		FallbackToLocal:   cfg.FallbackToLocal,
		RequestTimeoutSec: normalizedServerConnectionTimeout(cfg.RequestTimeoutSec),
	}
}

func activeServerConnectionTargetID(cfg config.ServerConnectionConfig, targets []speechkit.ServerConnectionTarget) string {
	activeID := strings.TrimSpace(cfg.ActiveTargetID)
	if activeID != "" {
		for _, target := range targets {
			if target.ID == activeID {
				return activeID
			}
		}
	}
	for _, target := range targets {
		if strings.TrimRight(target.URL, "/") == strings.TrimRight(strings.TrimSpace(cfg.URL), "/") &&
			target.BearerTokenEnv == strings.TrimSpace(cfg.BearerTokenEnv) &&
			target.AuthMode == config.NormalizeServerConnectionAuthMode(cfg.AuthMode) {
			return target.ID
		}
	}
	if len(targets) > 0 {
		return targets[0].ID
	}
	return ""
}

func normalizeServerConnectionTargets(targets []config.ServerConnectionTargetConfig) []config.ServerConnectionTargetConfig {
	normalized := make([]config.ServerConnectionTargetConfig, 0, len(targets))
	seen := map[string]int{}
	for i, target := range targets {
		target.URL = strings.TrimSpace(target.URL)
		if target.URL == "" {
			continue
		}
		target.Label = strings.TrimSpace(target.Label)
		if target.Label == "" {
			target.Label = serverConnectionLabelFromURL(target.URL)
		}
		if target.Label == "" {
			target.Label = "Server"
		}
		target.ID = serverConnectionTargetID(target.ID, target.URL, i)
		if count := seen[target.ID]; count > 0 {
			seen[target.ID] = count + 1
			target.ID = target.ID + "-" + strings.TrimSpace(strings.ReplaceAll(strings.ToLower(target.Label), " ", "-"))
			target.ID = serverConnectionTargetID(target.ID, target.URL, i)
		}
		seen[target.ID]++
		target.AuthMode = config.NormalizeServerConnectionAuthMode(target.AuthMode)
		target.BearerTokenEnv = strings.TrimSpace(target.BearerTokenEnv)
		if target.BearerTokenEnv == "" {
			target.BearerTokenEnv = defaultServerConnectionTokenEnv(target.AuthMode)
		}
		target.RequestTimeoutSec = normalizedServerConnectionTimeout(target.RequestTimeoutSec)
		normalized = append(normalized, target)
	}
	return normalized
}

func serverConnectionTargetID(seed, rawURL string, index int) string {
	source := strings.TrimSpace(seed)
	if source == "" {
		source = serverConnectionLabelFromURL(rawURL)
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(source) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '.' || r == ':' || r == '/' {
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		id = "server"
	}
	if index > 0 && seed == "" {
		id = id + "-" + strconv.Itoa(index+1)
	}
	return id
}

func serverConnectionLabelFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host
}

func normalizedServerConnectionTimeout(value int) int {
	switch {
	case value < 0:
		return 0
	case value == 0:
		return 30
	case value > 3600:
		return 3600
	default:
		return value
	}
}
