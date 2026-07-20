package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	ServerAuthModeEnv          = "SPEECHKIT_SERVER_AUTH_MODE"
	ServerBearerTokenEnvName   = "SPEECHKIT_SERVER_BEARER_TOKEN_ENV"
	ServerEdgeSecretEnvName    = "SPEECHKIT_SERVER_EDGE_AUTH_SECRET_ENV"
	ServerBearerRoleEnv        = "SPEECHKIT_SERVER_BEARER_ROLE"
	ServerAdminUsernameEnv     = "SPEECHKIT_SERVER_ADMIN_USERNAME"
	ServerAdminPasswordHashEnv = "SPEECHKIT_SERVER_ADMIN_PASSWORD_HASH"

	// ServerToolBridgeURLEnv sets the voice-agent tool bridge base URL
	// (e.g. https://api.kombify.io/v1/agents/voice-tools). Setting it enables
	// the bridge and derives <base>/manifest and <base>/call so a rollout can
	// flip the bridge on without editing the baked config file.
	ServerToolBridgeURLEnv = "SPEECHKIT_TOOLBRIDGE_URL"
	// ServerToolBridgeTimeoutMsEnv overrides [server.voiceagent.tool_bridge]
	// timeout_ms.
	ServerToolBridgeTimeoutMsEnv = "SPEECHKIT_TOOLBRIDGE_TIMEOUT_MS"
	// ServerToolBridgeMaxCallsEnv overrides [server.voiceagent.tool_bridge]
	// max_calls_per_session.
	ServerToolBridgeMaxCallsEnv = "SPEECHKIT_TOOLBRIDGE_MAX_CALLS"
)

// ApplyServerDeploymentEnv applies the headless deployment contract after
// persisted server settings. This gives Compose/Kubernetes/Render env injection
// the final say over runtime credentials without persisting secret values.
func ApplyServerDeploymentEnv(cfg *Config) ([]string, error) {
	if cfg == nil {
		return nil, nil
	}

	var notes []string

	if envName := cleanSetting(os.Getenv(ServerBearerTokenEnvName)); envName != "" {
		if !validEnvName(envName) {
			return nil, fmt.Errorf("%s must be a valid environment variable name", ServerBearerTokenEnvName)
		}
		if cleanSetting(cfg.Server.BearerTokenEnv) != envName {
			cfg.Server.BearerTokenEnv = envName
			notes = append(notes, "deployment env: bearer token env set from "+ServerBearerTokenEnvName)
		}
	} else if envPresent("SPEECHKIT_SERVER_TOKEN") && cleanSetting(cfg.Server.BearerTokenEnv) != "SPEECHKIT_SERVER_TOKEN" {
		cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
		notes = append(notes, "deployment env: standard SPEECHKIT_SERVER_TOKEN overrides stored bearer token env")
	}
	if cleanSetting(cfg.Server.BearerTokenEnv) == "" {
		cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	}

	if envName := cleanSetting(os.Getenv(ServerEdgeSecretEnvName)); envName != "" {
		if !validEnvName(envName) {
			return nil, fmt.Errorf("%s must be a valid environment variable name", ServerEdgeSecretEnvName)
		}
		if cleanSetting(cfg.Server.EdgeAuthSecretEnv) != envName {
			cfg.Server.EdgeAuthSecretEnv = envName
			notes = append(notes, "deployment env: edge auth secret env set from "+ServerEdgeSecretEnvName)
		}
	}
	if cleanSetting(cfg.Server.EdgeAuthSecretEnv) == "" {
		cfg.Server.EdgeAuthSecretEnv = "EDGE_AUTH_SECRET"
	}

	explicitMode := cleanSetting(os.Getenv(ServerAuthModeEnv))
	if explicitMode != "" {
		mode, err := normalizeServerDeploymentAuthMode(explicitMode)
		if err != nil {
			return nil, err
		}
		if cleanSetting(cfg.Server.AuthMode) != mode {
			cfg.Server.AuthMode = mode
			notes = append(notes, "deployment env: auth mode set from "+ServerAuthModeEnv)
		}
	} else {
		notes = append(notes, inferServerDeploymentAuthMode(cfg)...)
	}

	if role := cleanSetting(os.Getenv(ServerBearerRoleEnv)); role != "" && cfg.Server.BearerRole != role {
		cfg.Server.BearerRole = role
		notes = append(notes, "deployment env: bearer role set from "+ServerBearerRoleEnv)
	}
	if username := cleanSetting(os.Getenv(ServerAdminUsernameEnv)); username != "" && cfg.Server.AdminUsername != username {
		cfg.Server.AdminUsername = username
		notes = append(notes, "deployment env: admin username set from "+ServerAdminUsernameEnv)
	}
	if hash := cleanSetting(os.Getenv(ServerAdminPasswordHashEnv)); hash != "" && cfg.Server.AdminPasswordHash != hash {
		cfg.Server.AdminPasswordHash = hash
		notes = append(notes, "deployment env: admin password hash set from "+ServerAdminPasswordHashEnv)
	}

	toolBridgeNotes, err := applyServerToolBridgeEnv(cfg)
	if err != nil {
		return nil, err
	}
	notes = append(notes, toolBridgeNotes...)

	return notes, nil
}

// applyServerToolBridgeEnv applies the SPEECHKIT_TOOLBRIDGE_* deployment env
// contract for [server.voiceagent.tool_bridge]. Setting the base URL both
// enables the bridge and derives the manifest/call endpoints, so a rollout can
// flip the bridge via env alone (deploy/config/server.kombify-prod.toml ships
// enabled = false). Malformed values fail startup loudly instead of silently
// running with a broken bridge.
func applyServerToolBridgeEnv(cfg *Config) ([]string, error) {
	var notes []string
	bridge := &cfg.Server.VoiceAgent.ToolBridge

	if base := cleanSetting(os.Getenv(ServerToolBridgeURLEnv)); base != "" {
		parsed, err := url.Parse(base)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("%s must be an absolute http(s) URL", ServerToolBridgeURLEnv)
		}
		base = strings.TrimRight(base, "/")
		bridge.ManifestURL = base + "/manifest"
		bridge.InvokeURL = base + "/call"
		bridge.Enabled = true
		notes = append(notes, "deployment env: voice-agent tool bridge enabled from "+ServerToolBridgeURLEnv)
	}
	if raw := cleanSetting(os.Getenv(ServerToolBridgeTimeoutMsEnv)); raw != "" {
		timeoutMs, err := strconv.Atoi(raw)
		if err != nil || timeoutMs <= 0 {
			return nil, fmt.Errorf("%s must be a positive integer (milliseconds)", ServerToolBridgeTimeoutMsEnv)
		}
		if bridge.TimeoutMs != timeoutMs {
			bridge.TimeoutMs = timeoutMs
			notes = append(notes, "deployment env: tool bridge timeout set from "+ServerToolBridgeTimeoutMsEnv)
		}
	}
	if raw := cleanSetting(os.Getenv(ServerToolBridgeMaxCallsEnv)); raw != "" {
		maxCalls, err := strconv.Atoi(raw)
		if err != nil || maxCalls <= 0 {
			return nil, fmt.Errorf("%s must be a positive integer", ServerToolBridgeMaxCallsEnv)
		}
		if bridge.MaxCallsPerSession != maxCalls {
			bridge.MaxCallsPerSession = maxCalls
			notes = append(notes, "deployment env: tool bridge per-session call cap set from "+ServerToolBridgeMaxCallsEnv)
		}
	}
	return notes, nil
}

func inferServerDeploymentAuthMode(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	mode := strings.ToLower(cleanSetting(cfg.Server.AuthMode))
	bearerEnv := firstNonEmptySetting(cfg.Server.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN")
	edgeEnv := firstNonEmptySetting(cfg.Server.EdgeAuthSecretEnv, "EDGE_AUTH_SECRET")
	bearerSet := envPresent(bearerEnv)
	edgeSet := envPresent(edgeEnv)

	switch mode {
	case "", "none":
		switch {
		case bearerSet && edgeSet:
			cfg.Server.AuthMode = "bearer_or_edge"
			return []string{"deployment env: auth mode inferred as bearer_or_edge from configured credentials"}
		case bearerSet:
			cfg.Server.AuthMode = "bearer"
			return []string{"deployment env: auth mode inferred as bearer from " + bearerEnv}
		case edgeSet:
			cfg.Server.AuthMode = "edge_hmac"
			return []string{"deployment env: auth mode inferred as edge_hmac from " + edgeEnv}
		}
	case "edge_hmac":
		if bearerSet && edgeSet {
			cfg.Server.AuthMode = "bearer_or_edge"
			return []string{"deployment env: standard bearer token enabled alongside edge_hmac"}
		}
	}
	return nil
}

func normalizeServerDeploymentAuthMode(mode string) (string, error) {
	mode = strings.ToLower(cleanSetting(mode))
	switch mode {
	case "none", "bearer", "edge_hmac", "bearer_or_edge", "oidc", "bearer_or_oidc":
		return mode, nil
	default:
		return "", fmt.Errorf("%s must be one of none, bearer, edge_hmac, bearer_or_edge, oidc, bearer_or_oidc", ServerAuthModeEnv)
	}
}

func firstNonEmptySetting(values ...string) string {
	for _, value := range values {
		if value = cleanSetting(value); value != "" {
			return value
		}
	}
	return ""
}
