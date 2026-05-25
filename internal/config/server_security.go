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
	// Audit S-7: when AdminAuthEnabled is true the operator is
	// shipping the admin-session cookie surface (set-cookie reachable
	// via POST /v1/admin/session). Wildcard CORS combined with cookie
	// auth lets any origin issue authenticated requests once the
	// admin browser tab visits a hostile page in the same session.
	// Reject even when auth_mode=none, because the admin cookie is
	// independent of the request-auth mode.
	if cfg.Server.AdminAuthEnabled && serverCORSAllowsWildcard(cfg.Server.CORSAllowedOrigins) {
		return fmt.Errorf("cors_allowed_origins=* is not allowed when admin_auth_enabled=true; the admin session cookie surface requires an explicit origin allow-list")
	}
	if authMode != "none" {
		return validateServerAuthCredentials(cfg, authMode)
	}
	if parseServerBoolEnv(AllowInsecureNoAuthEnv) {
		return nil
	}
	if IsLoopbackListenAddr(cfg.Server.ListenAddr) {
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

// IsLoopbackListenAddr reports whether the given HTTP listen address binds
// only to a loopback interface (127.0.0.1, ::1, localhost). Used by both
// startup validation (server_security.go) and the auth middleware to decide
// whether AuthModeNone is acceptable as a runtime fallback.
//
// Returns false for wildcard binds (":8080", "0.0.0.0:8080", "[::]:8080")
// and for any address that fails to parse — fail-closed: an unknown bind is
// treated as public.
func IsLoopbackListenAddr(raw string) bool {
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
