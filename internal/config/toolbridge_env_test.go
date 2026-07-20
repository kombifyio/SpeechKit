package config

import (
	"strings"
	"testing"
)

func TestToolBridgeDefaultsFailClosed(t *testing.T) {
	cfg := defaults()
	tb := cfg.Server.VoiceAgent.ToolBridge
	if tb.Enabled {
		t.Fatal("tool bridge must default to disabled")
	}
	if tb.TimeoutMs != 10000 {
		t.Fatalf("TimeoutMs = %d, want 10000", tb.TimeoutMs)
	}
	if tb.MaxCallsPerSession != 20 {
		t.Fatalf("MaxCallsPerSession = %d, want 20", tb.MaxCallsPerSession)
	}
	if tb.CredentialHeader != "X-Edge-Obo-Subject-Token" {
		t.Fatalf("CredentialHeader = %q, want X-Edge-Obo-Subject-Token", tb.CredentialHeader)
	}
}

func TestApplyServerDeploymentEnvToolBridgeURLEnablesAndDerivesEndpoints(t *testing.T) {
	t.Setenv(ServerToolBridgeURLEnv, "https://api.kombify.io/v1/agents/voice-tools/")
	cfg := defaults()

	notes, err := ApplyServerDeploymentEnv(cfg)
	if err != nil {
		t.Fatalf("ApplyServerDeploymentEnv: %v", err)
	}
	tb := cfg.Server.VoiceAgent.ToolBridge
	if !tb.Enabled {
		t.Fatal("tool bridge must be enabled when SPEECHKIT_TOOLBRIDGE_URL is set")
	}
	if tb.ManifestURL != "https://api.kombify.io/v1/agents/voice-tools/manifest" {
		t.Fatalf("ManifestURL = %q", tb.ManifestURL)
	}
	if tb.InvokeURL != "https://api.kombify.io/v1/agents/voice-tools/call" {
		t.Fatalf("InvokeURL = %q", tb.InvokeURL)
	}
	found := false
	for _, note := range notes {
		if strings.Contains(note, ServerToolBridgeURLEnv) {
			found = true
		}
	}
	if !found {
		t.Fatalf("notes missing tool bridge entry: %v", notes)
	}
}

func TestApplyServerDeploymentEnvToolBridgeURLRejectsMalformed(t *testing.T) {
	for _, invalid := range []string{"not a url", "ftp://example.com/x", "/relative/path"} {
		t.Setenv(ServerToolBridgeURLEnv, invalid)
		cfg := defaults()
		if _, err := ApplyServerDeploymentEnv(cfg); err == nil {
			t.Fatalf("ApplyServerDeploymentEnv accepted %q, want error", invalid)
		}
	}
}

func TestApplyServerDeploymentEnvToolBridgeTimeoutAndCap(t *testing.T) {
	t.Setenv(ServerToolBridgeTimeoutMsEnv, "2500")
	t.Setenv(ServerToolBridgeMaxCallsEnv, "5")
	cfg := defaults()

	if _, err := ApplyServerDeploymentEnv(cfg); err != nil {
		t.Fatalf("ApplyServerDeploymentEnv: %v", err)
	}
	tb := cfg.Server.VoiceAgent.ToolBridge
	if tb.TimeoutMs != 2500 {
		t.Fatalf("TimeoutMs = %d, want 2500", tb.TimeoutMs)
	}
	if tb.MaxCallsPerSession != 5 {
		t.Fatalf("MaxCallsPerSession = %d, want 5", tb.MaxCallsPerSession)
	}
	// Timeout/cap alone must NOT enable the bridge.
	if tb.Enabled {
		t.Fatal("timeout/cap env overrides must not enable the bridge")
	}
}

func TestApplyServerDeploymentEnvToolBridgeRejectsInvalidNumbers(t *testing.T) {
	t.Setenv(ServerToolBridgeTimeoutMsEnv, "0")
	cfg := defaults()
	if _, err := ApplyServerDeploymentEnv(cfg); err == nil {
		t.Fatal("timeout 0 accepted, want error")
	}

	t.Setenv(ServerToolBridgeTimeoutMsEnv, "")
	t.Setenv(ServerToolBridgeMaxCallsEnv, "-3")
	cfg = defaults()
	if _, err := ApplyServerDeploymentEnv(cfg); err == nil {
		t.Fatal("negative max calls accepted, want error")
	}
}
