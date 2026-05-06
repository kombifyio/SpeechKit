package main

import (
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// serverConnectionSettingFromConfig copies the [server_connection]
// config into the public surface, never including the bearer-token
// value itself (only the env var name + a "is the env var set" boolean
// so the UI can render a "missing token" warning).
func serverConnectionSettingFromConfig(cfg config.ServerConnectionConfig) speechkit.ServerConnectionSetting {
	tokenSet := false
	if env := strings.TrimSpace(cfg.BearerTokenEnv); env != "" {
		tokenSet = strings.TrimSpace(config.ResolveSecret(env)) != ""
	}
	return speechkit.ServerConnectionSetting{
		Enabled:           cfg.Enabled,
		URL:               cfg.URL,
		BearerTokenEnv:    cfg.BearerTokenEnv,
		BearerTokenSet:    tokenSet,
		FallbackToLocal:   cfg.FallbackToLocal,
		RequestTimeoutSec: cfg.RequestTimeoutSec,
	}
}
