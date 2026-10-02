package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerConnectionFromTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[model_selection.dictate]
primary_profile_id = "stt.local.whispercpp"
mode_source = "server"

[model_selection.assist]
primary_profile_id = "assist.builtin.gemma4-e4b"
mode_source = "local"

[model_selection.voice_agent]
primary_profile_id = "realtime.google.gemini-native-audio"
mode_source = "server"

[server_connection]
enabled = true
url = "https://speechkit.test"
bearer_token_env = "MY_TOKEN"
fallback_to_local = false
request_timeout_sec = 5
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ModelSelection.Dictate.ResolvedModeSource(); got != ModeSourceServer {
		t.Errorf("dictate mode_source = %q, want server", got)
	}
	if got := cfg.ModelSelection.Assist.ResolvedModeSource(); got != ModeSourceLocal {
		t.Errorf("assist mode_source = %q, want local", got)
	}
	if got := cfg.ModelSelection.VoiceAgent.ResolvedModeSource(); got != ModeSourceServer {
		t.Errorf("voice_agent mode_source = %q, want server", got)
	}
	if !cfg.ServerConnection.Enabled {
		t.Error("server_connection.enabled = false, want true")
	}
	if cfg.ServerConnection.URL != "https://speechkit.test" {
		t.Errorf("server_connection.url = %q", cfg.ServerConnection.URL)
	}
	if cfg.ServerConnection.BearerTokenEnv != "MY_TOKEN" {
		t.Errorf("server_connection.bearer_token_env = %q, want MY_TOKEN", cfg.ServerConnection.BearerTokenEnv)
	}
	if cfg.ServerConnection.AuthMode != ServerConnectionAuthModeBearer {
		t.Errorf("server_connection.auth_mode = %q, want bearer", cfg.ServerConnection.AuthMode)
	}
	if cfg.ServerConnection.FallbackToLocal {
		t.Error("server_connection.fallback_to_local = true, want false")
	}
	if cfg.ServerConnection.RequestTimeoutSec != 5 {
		t.Errorf("server_connection.request_timeout_sec = %d, want 5", cfg.ServerConnection.RequestTimeoutSec)
	}
}

func TestLoadDoesNotBackfillServerConnectionURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[server_connection]
enabled = false
url = ""
bearer_token_env = ""
fallback_to_local = true
request_timeout_sec = 0
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.ServerConnection.Enabled {
		t.Fatal("server connection should stay disabled by default")
	}
	if got, want := cfg.ServerConnection.URL, ""; got != want {
		t.Fatalf("server_connection.url = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.AuthMode, ServerConnectionAuthModeBearer; got != want {
		t.Fatalf("server_connection.auth_mode = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.BearerTokenEnv, ""; got != want {
		t.Fatalf("server_connection.bearer_token_env = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.RequestTimeoutSec, 0; got != want {
		t.Fatalf("server_connection.request_timeout_sec = %d, want %d", got, want)
	}
}
