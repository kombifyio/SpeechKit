package config

import (
	"strings"
	"testing"
)

func TestValidateServerProductionAuthRejectsPublicNoAuth(t *testing.T) {
	cfg := &Config{}
	cfg.Server.ListenAddr = ":8080"
	cfg.Server.AuthMode = "none"

	err := ValidateServerProductionAuth(cfg)
	if err == nil {
		t.Fatal("ValidateServerProductionAuth error = nil, want public no-auth rejection")
	}
	if !strings.Contains(err.Error(), "auth_mode=none") {
		t.Fatalf("ValidateServerProductionAuth error = %q, want auth_mode=none context", err.Error())
	}
}

func TestValidateServerProductionAuthAllowsLoopbackNoAuth(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		cfg := &Config{}
		cfg.Server.ListenAddr = addr
		cfg.Server.AuthMode = "none"

		if err := ValidateServerProductionAuth(cfg); err != nil {
			t.Fatalf("ValidateServerProductionAuth(%q): %v", addr, err)
		}
	}
}

func TestValidateServerProductionAuthAllowsExplicitInsecureTestOverride(t *testing.T) {
	t.Setenv(AllowInsecureNoAuthEnv, "1")
	cfg := &Config{}
	cfg.Server.ListenAddr = ":8080"
	cfg.Server.AuthMode = "none"

	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth with override: %v", err)
	}
}
