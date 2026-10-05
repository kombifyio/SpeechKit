package security

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/app/internal/config"
)

func TestApplyServerDeploymentEnvStandardTokenOverridesStoredBearerEnv(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "compose-token")
	t.Setenv("PERSISTED_SERVER_TOKEN", "")

	cfg := &config.Config{}
	cfg.Server.AuthMode = "bearer"
	cfg.Server.BearerTokenEnv = "PERSISTED_SERVER_TOKEN"

	notes, err := config.ApplyServerDeploymentEnv(cfg)
	if err != nil {
		t.Fatalf("config.ApplyServerDeploymentEnv: %v", err)
	}
	if got := cfg.Server.BearerTokenEnv; got != "SPEECHKIT_SERVER_TOKEN" {
		t.Fatalf("BearerTokenEnv = %q, want SPEECHKIT_SERVER_TOKEN", got)
	}
	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth: %v", err)
	}
	if !notesContain(notes, "standard SPEECHKIT_SERVER_TOKEN overrides") {
		t.Fatalf("notes = %#v, want standard token override note", notes)
	}
}

func TestApplyServerDeploymentEnvCustomTokenEnvTakesPrecedence(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "standard-token")
	t.Setenv("DEPLOYMENT_SERVER_TOKEN", "custom-token")
	t.Setenv(config.ServerBearerTokenEnvName, "DEPLOYMENT_SERVER_TOKEN")

	cfg := &config.Config{}
	cfg.Server.AuthMode = "bearer"
	cfg.Server.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"

	if _, err := config.ApplyServerDeploymentEnv(cfg); err != nil {
		t.Fatalf("config.ApplyServerDeploymentEnv: %v", err)
	}
	if got := cfg.Server.BearerTokenEnv; got != "DEPLOYMENT_SERVER_TOKEN" {
		t.Fatalf("BearerTokenEnv = %q, want DEPLOYMENT_SERVER_TOKEN", got)
	}
	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth: %v", err)
	}
}

func TestApplyServerDeploymentEnvInfersAuthModeFromHeadlessToken(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "compose-token")

	cfg := &config.Config{}
	cfg.Server.AuthMode = "none"

	notes, err := config.ApplyServerDeploymentEnv(cfg)
	if err != nil {
		t.Fatalf("config.ApplyServerDeploymentEnv: %v", err)
	}
	if got := cfg.Server.AuthMode; got != "bearer" {
		t.Fatalf("AuthMode = %q, want bearer", got)
	}
	if !notesContain(notes, "auth mode inferred as bearer") {
		t.Fatalf("notes = %#v, want inferred bearer note", notes)
	}
}

func TestApplyServerDeploymentEnvRejectsInvalidTokenEnvName(t *testing.T) {
	t.Setenv(config.ServerBearerTokenEnvName, "not valid")

	cfg := &config.Config{}
	_, err := config.ApplyServerDeploymentEnv(cfg)
	if err == nil {
		t.Fatal("config.ApplyServerDeploymentEnv error = nil, want invalid env name error")
	}
	if !strings.Contains(err.Error(), config.ServerBearerTokenEnvName) {
		t.Fatalf("error = %q, want %s context", err.Error(), config.ServerBearerTokenEnvName)
	}
}

func TestApplyServerDeploymentEnvExplicitAuthMode(t *testing.T) {
	t.Setenv("EDGE_AUTH_SECRET", "edge-secret")
	t.Setenv(config.ServerAuthModeEnv, "edge_hmac")

	cfg := &config.Config{}
	cfg.Server.AuthMode = "bearer"

	if _, err := config.ApplyServerDeploymentEnv(cfg); err != nil {
		t.Fatalf("config.ApplyServerDeploymentEnv: %v", err)
	}
	if got := cfg.Server.AuthMode; got != "edge_hmac" {
		t.Fatalf("AuthMode = %q, want edge_hmac", got)
	}
	if err := ValidateServerProductionAuth(cfg); err != nil {
		t.Fatalf("ValidateServerProductionAuth: %v", err)
	}
}

func notesContain(notes []string, needle string) bool {
	for _, note := range notes {
		if strings.Contains(note, needle) {
			return true
		}
	}
	return false
}
