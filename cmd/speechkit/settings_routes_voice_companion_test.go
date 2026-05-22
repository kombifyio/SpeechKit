package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

// TestPiperVoicesEndpoint_ListsFilesInDir verifies the /api/tts/piper/voices
// route returns the live directory scan and respects the voice_dir query
// override.
func TestPiperVoicesEndpoint_ListsFilesInDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "en_US-amy-medium.onnx"), []byte("model"), 0o644); err != nil {
		t.Fatalf("write voice file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "de_DE-thorsten-medium.onnx"), []byte("model"), 0o644); err != nil {
		t.Fatalf("write voice file: %v", err)
	}

	cfg := &config.Config{}
	cfg.TTS.Piper.VoiceDir = dir
	cfg.TTS.Piper.DefaultVoices = map[string]string{"en": "en_US-amy-medium.onnx"}

	mux := http.NewServeMux()
	registerVoiceCompanionRoutes(mux, cfg)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tts/piper/voices")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var result piperVoiceListResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.VoiceDir != dir {
		t.Errorf("VoiceDir = %q, want %q", result.VoiceDir, dir)
	}
	if len(result.Voices) != 2 {
		t.Fatalf("Voices count = %d, want 2", len(result.Voices))
	}
	if result.Voices[0].Filename != "de_DE-thorsten-medium.onnx" {
		t.Errorf("first voice = %q, want de_DE-thorsten-medium.onnx (sorted)", result.Voices[0].Filename)
	}
	if result.Defaults["en"] != "en_US-amy-medium.onnx" {
		t.Errorf("defaults[en] = %q", result.Defaults["en"])
	}
}

// TestPiperVoicesEndpoint_OverrideQuery verifies the voice_dir query
// parameter takes precedence over cfg's VoiceDir.
func TestPiperVoicesEndpoint_OverrideQuery(t *testing.T) {
	queryDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(queryDir, "voice-test.onnx"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := &config.Config{}
	cfg.TTS.Piper.VoiceDir = t.TempDir() // different, empty

	mux := http.NewServeMux()
	registerVoiceCompanionRoutes(mux, cfg)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/tts/piper/voices?voice_dir=" + url.QueryEscape(queryDir))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	var result piperVoiceListResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.VoiceDir != queryDir {
		t.Errorf("VoiceDir = %q, want override %q", result.VoiceDir, queryDir)
	}
	if len(result.Voices) != 1 {
		t.Errorf("Voices count = %d, want 1 from override dir", len(result.Voices))
	}
}

// TestHomeAssistantProbeEndpoint_MissingFields confirms the endpoint
// surfaces clear error messages when URL or token env are missing.
func TestHomeAssistantProbeEndpoint_MissingFields(t *testing.T) {
	cfg := &config.Config{} // empty
	mux := http.NewServeMux()
	registerVoiceCompanionRoutes(mux, cfg)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := url.Values{}
	resp, err := http.Post(srv.URL+"/settings/homeassistant/test", "application/x-www-form-urlencoded", strings.NewReader(body.Encode()))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	var result homeAssistantProbeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.OK {
		t.Error("OK = true on empty config, want false")
	}
	if !strings.Contains(result.Error, "url") {
		t.Errorf("Error = %q, want mention of url", result.Error)
	}
}

// TestHomeAssistantProbeEndpoint_HitsFakeServer wires a fake HA server
// to the probe and asserts the result reflects /api/'s status.
func TestHomeAssistantProbeEndpoint_HitsFakeServer(t *testing.T) {
	haCalls := 0
	fakeHA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		haCalls++
		if r.URL.Path != "/api/" {
			t.Errorf("unexpected HA path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer probe-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer fakeHA.Close()

	t.Setenv("HA_LL_TOKEN_PROBE", "probe-token")
	cfg := &config.Config{}
	cfg.Assist.HomeAssistant.URL = fakeHA.URL
	cfg.Assist.HomeAssistant.TokenEnv = "HA_LL_TOKEN_PROBE"

	mux := http.NewServeMux()
	registerVoiceCompanionRoutes(mux, cfg)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/settings/homeassistant/test", "application/x-www-form-urlencoded", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	var result homeAssistantProbeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.OK {
		t.Errorf("OK = false, error = %q", result.Error)
	}
	if haCalls != 1 {
		t.Errorf("HA called %d times, want 1", haCalls)
	}
}
