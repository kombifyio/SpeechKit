package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestVoiceAgentApplyProfile_OnPrem(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := defaultTestConfig()

	handler := handleVoiceAgentApplyProfile(cfgPath, cfg, nil)
	body, _ := json.Marshal(map[string]string{"profile": "on-prem"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice-agent/apply-profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp voiceAgentApplyProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Applied {
		t.Fatal("applied = false, want true")
	}
	if resp.Profile != "on-prem" {
		t.Fatalf("profile = %q, want \"on-prem\"", resp.Profile)
	}

	// on-prem preset sets routing.strategy = "local-only"
	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("routing.strategy = %q, want \"local-only\"", cfg.Routing.Strategy)
	}
	// on-prem preset sets update.enabled = false
	if cfg.Update.Enabled {
		t.Fatal("update.enabled = true after on-prem preset, want false")
	}
	// on-prem preset sets voice_agent.provider = "local-cascaded"
	if cfg.VoiceAgent.Provider != "local-cascaded" {
		t.Fatalf("voice_agent.provider = %q, want \"local-cascaded\"", cfg.VoiceAgent.Provider)
	}
	// on-prem preset sets audit.enabled = true
	if !cfg.Audit.Enabled {
		t.Fatal("audit.enabled = false after on-prem preset, want true")
	}
}

func TestVoiceAgentApplyProfile_CloudByok(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := defaultTestConfig()

	handler := handleVoiceAgentApplyProfile(cfgPath, cfg, nil)
	body, _ := json.Marshal(map[string]string{"profile": "cloud-byok"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice-agent/apply-profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var resp voiceAgentApplyProfileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Profile != "cloud-byok" {
		t.Fatalf("profile = %q, want \"cloud-byok\"", resp.Profile)
	}

	// cloud-byok preset sets voice_agent.provider = "gemini"
	if cfg.VoiceAgent.Provider != "gemini" {
		t.Fatalf("voice_agent.provider = %q, want \"gemini\"", cfg.VoiceAgent.Provider)
	}
	// cloud-byok preset sets routing.strategy = "dynamic"
	if cfg.Routing.Strategy != "dynamic" {
		t.Fatalf("routing.strategy = %q, want \"dynamic\"", cfg.Routing.Strategy)
	}
	// cloud-byok preset sets update.enabled = true
	if !cfg.Update.Enabled {
		t.Fatal("update.enabled = false after cloud-byok preset, want true")
	}
}

func TestVoiceAgentApplyProfile_UnknownProfile(t *testing.T) {
	cfg := defaultTestConfig()
	handler := handleVoiceAgentApplyProfile(filepath.Join(t.TempDir(), "config.toml"), cfg, nil)

	body, _ := json.Marshal(map[string]string{"profile": "unknown"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice-agent/apply-profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown profile") {
		t.Fatalf("body = %q, want to contain \"unknown profile\"", rec.Body.String())
	}
}

func TestVoiceAgentApplyProfile_MethodNotAllowed(t *testing.T) {
	cfg := defaultTestConfig()
	handler := handleVoiceAgentApplyProfile(filepath.Join(t.TempDir(), "config.toml"), cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/voice-agent/apply-profile", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestVoiceAgentApplyProfile_NilConfigReturns503(t *testing.T) {
	handler := handleVoiceAgentApplyProfile(filepath.Join(t.TempDir(), "config.toml"), nil, nil)

	body, _ := json.Marshal(map[string]string{"profile": "on-prem"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice-agent/apply-profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestVoiceAgentApplyProfile_PreservesExistingFields(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := defaultTestConfig()
	// Set customer-specific fields that the preset must not overwrite.
	cfg.General.DictateHotkey = "Ctrl+F9"
	cfg.Providers.Google.Region = "us-central1" // customer-set region; preset must preserve it
	cfg.Providers.Google.AgentModel = "gemini-custom-model"

	handler := handleVoiceAgentApplyProfile(cfgPath, cfg, nil)
	body, _ := json.Marshal(map[string]string{"profile": "cloud-byok"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice-agent/apply-profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if cfg.General.DictateHotkey != "Ctrl+F9" {
		t.Fatalf("DictateHotkey = %q, preset must not overwrite customer hotkeys", cfg.General.DictateHotkey)
	}
	if cfg.Providers.Google.Region != "us-central1" {
		t.Fatalf("Google.Region = %q, want customer region to be preserved", cfg.Providers.Google.Region)
	}
	if cfg.Providers.Google.AgentModel != "gemini-custom-model" {
		t.Fatalf("Google.AgentModel = %q, preset must not overwrite customer model choice", cfg.Providers.Google.AgentModel)
	}
}
