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
	if !strings.EqualFold(strings.TrimSpace(cfg.Server.AuthMode), "none") {
		return nil
	}
	if parseServerBoolEnv(AllowInsecureNoAuthEnv) {
		return nil
	}
	if isLoopbackListenAddr(cfg.Server.ListenAddr) {
		return nil
	}
	return fmt.Errorf("auth_mode=none is only allowed on loopback listen addresses; set %s=1 only for explicit local test runs", AllowInsecureNoAuthEnv)
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
