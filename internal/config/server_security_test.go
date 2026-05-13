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

func TestValidateServerProductionAuthRejectsPublicEmptyAuthMode(t *testing.T) {
	cfg := &Config{}
	cfg.Server.ListenAddr = ":8080"

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

func TestValidateServerProductionAuthRejectsWildcardCORSWithAuthenticatedModes(t *testing.T) {
	for _, mode := range []string{"bearer", "edge_hmac", "bearer_or_edge"} {
		cfg := &Config{}
		cfg.Server.ListenAddr = "127.0.0.1:8080"
		cfg.Server.AuthMode = mode
		cfg.Server.CORSAllowedOrigins = []string{"https://app.example.com", " * "}

		err := ValidateServerProductionAuth(cfg)
		if err == nil {
			t.Fatalf("ValidateServerProductionAuth(%q) error = nil, want wildcard CORS rejection", mode)
		}
		if !strings.Contains(err.Error(), "cors_allowed_origins=*") {
			t.Fatalf("ValidateServerProductionAuth(%q) error = %q, want CORS context", mode, err.Error())
		}
	}
}

func TestValidateServerProductionAuthAllowsWildcardCORSForExplicitLocalNoAuth(t *testing.T) {
	cfg := &Config{}
	cfg.Server.ListenAddr = "127.0.0.1:8080"
	cfg.Server.AuthMode = "none"
	cfg.Server.CORSAllowedOrigins = []string{"*"}

	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth local no-auth wildcard CORS: %v", err)
	}
}

func TestValidateServerProductionAuthRejectsMissingBearerToken(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "")
	cfg := &Config{}
	cfg.Server.ListenAddr = ":8080"
	cfg.Server.AuthMode = "bearer"
	cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"

	err := ValidateServerProductionAuth(cfg)
	if err == nil {
		t.Fatal("ValidateServerProductionAuth error = nil, want missing token rejection")
	}
	if !strings.Contains(err.Error(), "SPEECHKIT_SERVER_TOKEN") {
		t.Fatalf("ValidateServerProductionAuth error = %q, want token env context", err.Error())
	}
}

func TestValidateServerProductionAuthAllowsConfiguredBearerToken(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "compose-token")
	cfg := &Config{}
	cfg.Server.ListenAddr = ":8080"
	cfg.Server.AuthMode = "bearer"
	cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"

	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth: %v", err)
	}
}

func TestValidateServerProductionAuthRejectsBearerOrEdgeWithoutCredentials(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "")
	t.Setenv("EDGE_AUTH_SECRET", "")
	cfg := &Config{}
	cfg.Server.ListenAddr = ":8080"
	cfg.Server.AuthMode = "bearer_or_edge"
	cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	cfg.Server.EdgeAuthSecretEnv = "EDGE_AUTH_SECRET"

	err := ValidateServerProductionAuth(cfg)
	if err == nil {
		t.Fatal("ValidateServerProductionAuth error = nil, want missing credentials rejection")
	}
	if !strings.Contains(err.Error(), "SPEECHKIT_SERVER_TOKEN") || !strings.Contains(err.Error(), "EDGE_AUTH_SECRET") {
		t.Fatalf("ValidateServerProductionAuth error = %q, want both env names", err.Error())
	}
}
