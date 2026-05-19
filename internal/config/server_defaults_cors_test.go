package config

import (
	"testing"
)

func TestApplyServerRuntimeDefaultsAddsPublicURLToCORS(t *testing.T) {
	t.Setenv("SPEECHKIT_PUBLIC_URL", "https://speechkit.kombify.io")
	cfg := &Config{}
	_ = ApplyServerRuntimeDefaults(cfg)
	found := false
	for _, o := range cfg.Server.CORSAllowedOrigins {
		if o == "https://speechkit.kombify.io" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected CORSAllowedOrigins to include public URL, got %#v", cfg.Server.CORSAllowedOrigins)
	}
}

func TestApplyServerRuntimeDefaultsRespectsExplicitWildcard(t *testing.T) {
	t.Setenv("SPEECHKIT_PUBLIC_URL", "https://speechkit.kombify.io")
	cfg := &Config{}
	cfg.Server.CORSAllowedOrigins = []string{"*"}
	_ = ApplyServerRuntimeDefaults(cfg)
	if len(cfg.Server.CORSAllowedOrigins) != 1 || cfg.Server.CORSAllowedOrigins[0] != "*" {
		t.Fatalf("wildcard must short-circuit, got %#v", cfg.Server.CORSAllowedOrigins)
	}
}

func TestApplyServerRuntimeDefaultsDeduplicatesPublicURL(t *testing.T) {
	t.Setenv("SPEECHKIT_PUBLIC_URL", "https://speechkit.kombify.io")
	cfg := &Config{}
	cfg.Server.CORSAllowedOrigins = []string{"https://speechkit.kombify.io"}
	_ = ApplyServerRuntimeDefaults(cfg)
	if len(cfg.Server.CORSAllowedOrigins) != 1 {
		t.Fatalf("duplicate entry must be skipped, got %#v", cfg.Server.CORSAllowedOrigins)
	}
}
