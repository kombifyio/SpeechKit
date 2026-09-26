package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func TestVoiceAgentSessionLimitsPreferVoiceAgentSection(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			MaxVoiceAgentSessions: 10,
			MaxSessionsPerUser:    2,
		},
		VoiceAgent: VoiceAgentConfig{
			Limits: VoiceAgentLimitsConfig{
				MaxGlobalSessions:      20,
				MaxPerIdentitySessions: 4,
			},
		},
	}

	got := cfg.VoiceAgentSessionLimits()
	if got.MaxGlobalSessions != 20 {
		t.Fatalf("MaxGlobalSessions = %d, want 20", got.MaxGlobalSessions)
	}
	if got.MaxPerIdentitySessions != 4 {
		t.Fatalf("MaxPerIdentitySessions = %d, want 4", got.MaxPerIdentitySessions)
	}
}

func TestVoiceAgentSessionLimitsFallbackToLegacyServerFields(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{
			MaxVoiceAgentSessions: 10,
			MaxSessionsPerUser:    2,
		},
	}

	got := cfg.VoiceAgentSessionLimits()
	if got.MaxGlobalSessions != 10 {
		t.Fatalf("MaxGlobalSessions = %d, want 10", got.MaxGlobalSessions)
	}
	if got.MaxPerIdentitySessions != 2 {
		t.Fatalf("MaxPerIdentitySessions = %d, want 2", got.MaxPerIdentitySessions)
	}
}

func TestLoadVoiceAgentLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(`
[server]
max_voiceagent_sessions = 10
max_sessions_per_user = 2

[voice_agent.limits]
max_global_sessions = 25
max_per_identity_sessions = 5
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.VoiceAgentSessionLimits()
	if got.MaxGlobalSessions != 25 {
		t.Fatalf("MaxGlobalSessions = %d, want 25", got.MaxGlobalSessions)
	}
	if got.MaxPerIdentitySessions != 5 {
		t.Fatalf("MaxPerIdentitySessions = %d, want 5", got.MaxPerIdentitySessions)
	}
}

func TestLoadVoiceAgentAgentProfileID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[voice_agent]
agent_profile_id = "humor_companion"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.VoiceAgent.AgentProfileID, voiceagentprofile.HumorCompanionID; got != want {
		t.Fatalf("voice_agent.agent_profile_id = %q, want %q", got, want)
	}
}

func TestLoadVoiceAgentAgentProfileIDFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[voice_agent]
agent_profile_id = "unknown"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.VoiceAgent.AgentProfileID, voiceagentprofile.DefaultID; got != want {
		t.Fatalf("voice_agent.agent_profile_id = %q, want %q", got, want)
	}
}

func TestLoadVoiceAgentAgentSequenceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[voice_agent]
agent_sequence_id = "  custom_discovery_flow  "
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := cfg.VoiceAgent.AgentSequenceID, "custom_discovery_flow"; got != want {
		t.Fatalf("voice_agent.agent_sequence_id = %q, want %q", got, want)
	}
}
