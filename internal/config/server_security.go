package config

import (
	"fmt"
	"net"
	"os"
	"strings"
)

const AllowInsecureNoAuthEnv = "SPEECHKIT_ALLOW_INSECURE_NO_AUTH"

// ValidateServerProductionAuth rejects accidental public no-auth server binds.
// auth_mode=none remains available for local development and explicit tests.
func ValidateServerProductionAuth(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	authMode := strings.ToLower(strings.TrimSpace(cfg.Server.AuthMode))
	if authMode == "" {
		authMode = "none"
	}
	if authMode != "none" && serverCORSAllowsWildcard(cfg.Server.CORSAllowedOrigins) {
		return fmt.Errorf("cors_allowed_origins=* is not allowed with authenticated server auth_mode=%s", authMode)
	}
	if authMode != "none" {
		return validateServerAuthCredentials(cfg, authMode)
	}
	if parseServerBoolEnv(AllowInsecureNoAuthEnv) {
		return nil
	}
	if isLoopbackListenAddr(cfg.Server.ListenAddr) {
		return nil
	}
	return fmt.Errorf("auth_mode=none is only allowed on loopback listen addresses; set %s=1 only for explicit local test runs", AllowInsecureNoAuthEnv)
}

func validateServerAuthCredentials(cfg *Config, authMode string) error {
	bearerEnv := strings.TrimSpace(cfg.Server.BearerTokenEnv)
	if bearerEnv == "" {
		bearerEnv = "SPEECHKIT_SERVER_TOKEN"
	}
	edgeEnv := strings.TrimSpace(cfg.Server.EdgeAuthSecretEnv)
	if edgeEnv == "" {
		edgeEnv = "EDGE_AUTH_SECRET"
	}
	bearerSet := strings.TrimSpace(os.Getenv(bearerEnv)) != ""
	edgeSet := strings.TrimSpace(os.Getenv(edgeEnv)) != ""
	switch authMode {
	case "bearer":
		if !bearerSet {
			return fmt.Errorf("auth_mode=bearer requires %s to be set", bearerEnv)
		}
	case "edge_hmac":
		if !edgeSet {
			return fmt.Errorf("auth_mode=edge_hmac requires %s to be set", edgeEnv)
		}
	case "bearer_or_edge":
		if !bearerSet && !edgeSet {
			return fmt.Errorf("auth_mode=bearer_or_edge requires %s or %s to be set", bearerEnv, edgeEnv)
		}
	}
	return nil
}

func serverCORSAllowsWildcard(origins []string) bool {
	for _, origin := range origins {
		if strings.TrimSpace(origin) == "*" {
			return true
		}
	}
	return false
}

func parseServerBoolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isLoopbackListenAddr(raw string) bool {
	addr := strings.TrimSpace(raw)
	if addr == "" {
		addr = ":8080"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			host = ""
		} else {
			return false
		}
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
