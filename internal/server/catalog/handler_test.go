//go:build linux

package catalog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestProfilesRejectsUnknownMode(t *testing.T) {
	h := New(&config.Config{}, func(string) string { return "ok" }, "test")

	req := httptest.NewRequest(http.MethodGet, "/v1/catalog/profiles?mode=invalid", nil)
	rec := httptest.NewRecorder()
	h.profiles(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_mode") {
		t.Fatalf("body = %s, want invalid_mode", rec.Body.String())
	}
}

func TestProfilesRejectsEmptyMode(t *testing.T) {
	h := New(&config.Config{}, func(string) string { return "ok" }, "test")

	req := httptest.NewRequest(http.MethodGet, "/v1/catalog/profiles?mode=", nil)
	rec := httptest.NewRecorder()
	h.profiles(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_mode") {
		t.Fatalf("body = %s, want invalid_mode", rec.Body.String())
	}
}

func TestReadinessUsesConfiguredActiveProfile(t *testing.T) {
	t.Setenv("SPEECHKIT_TEST_OPENAI_KEY", "test-key")

	cfg := &config.Config{}
	cfg.Server.Modes = []string{"dictation"}
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.openai.whisper-1"
	cfg.Providers.OpenAI.Enabled = true
	cfg.Providers.OpenAI.APIKeyEnv = "SPEECHKIT_TEST_OPENAI_KEY"

	h := New(cfg, func(component string) string {
		if component == "mode.dictation" {
			return "ok"
		}
		return ""
	}, "test")

	ready := getReadiness(t, h, "/v1/catalog/profiles/stt.openai.whisper-1/readiness")
	if !ready.Active {
		t.Fatalf("Active = false, want true for configured primary profile")
	}
	if ready.Default {
		t.Fatalf("Default = true, want catalog default to remain independent from active profile")
	}
	if !ready.Configured || !ready.CredentialsReady || !ready.RuntimeReady || !ready.Ready {
		t.Fatalf("readiness = %+v, want configured credentials/runtime/ready", ready)
	}
}

func TestReadinessSeparatesModeAndProviderMissingReasons(t *testing.T) {
	t.Setenv("SPEECHKIT_TEST_OPENAI_KEY", "test-key")

	cfg := &config.Config{}
	cfg.Server.Modes = []string{"assist"}
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.openai.whisper-1"
	cfg.Providers.OpenAI.Enabled = false
	cfg.Providers.OpenAI.APIKeyEnv = "SPEECHKIT_TEST_OPENAI_KEY"

	h := New(cfg, func(component string) string {
		if component == "mode.assist" {
			return "ok"
		}
		return ""
	}, "test")

	ready := getReadiness(t, h, "/v1/catalog/profiles/stt.openai.whisper-1/readiness")
	if ready.Active || ready.Configured || ready.Ready {
		t.Fatalf("readiness = %+v, want inactive/unconfigured/not ready", ready)
	}
	if !ready.CredentialsReady {
		t.Fatalf("CredentialsReady = false, want true because env var is present")
	}
	if !slices.Contains(ready.Missing, "mode_disabled") {
		t.Fatalf("Missing = %v, want mode_disabled", ready.Missing)
	}
	if !slices.Contains(ready.Missing, "provider_disabled") {
		t.Fatalf("Missing = %v, want provider_disabled", ready.Missing)
	}
}

func TestReadinessGoogleSTTDoesNotUseGeminiKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "gemini-key")

	cfg := &config.Config{}
	cfg.Server.Modes = []string{"dictation"}
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.google.chirp-3"
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTAPIKeyEnv = "SPEECHKIT_TEST_GOOGLE_STT_KEY"

	h := New(cfg, func(component string) string {
		if component == "mode.dictation" {
			return "ok"
		}
		return ""
	}, "test")

	ready := getReadiness(t, h, "/v1/catalog/profiles/stt.google.chirp-3/readiness")
	if ready.CredentialsReady || ready.Configured || ready.Ready {
		t.Fatalf("readiness = %+v, want Google STT not ready with Gemini key only", ready)
	}
	if !slices.Contains(ready.Missing, "credentials") {
		t.Fatalf("Missing = %v, want credentials", ready.Missing)
	}
}

func TestReadinessGoogleSTTUsesDedicatedKey(t *testing.T) {
	t.Setenv("GOOGLE_AI_API_KEY", "gemini-key")
	t.Setenv("SPEECHKIT_TEST_GOOGLE_STT_KEY", "speech-key")

	cfg := &config.Config{}
	cfg.Server.Modes = []string{"dictation"}
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.google.chirp-3"
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTAPIKeyEnv = "SPEECHKIT_TEST_GOOGLE_STT_KEY"

	h := New(cfg, func(component string) string {
		if component == "mode.dictation" {
			return "ok"
		}
		return ""
	}, "test")

	ready := getReadiness(t, h, "/v1/catalog/profiles/stt.google.chirp-3/readiness")
	if !ready.CredentialsReady || !ready.Configured || !ready.Ready {
		t.Fatalf("readiness = %+v, want Google STT ready with dedicated key", ready)
	}
}

func getReadiness(t *testing.T, h *Handler, path string) framework.Readiness {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	h.profileReadiness(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var ready framework.Readiness
	if err := json.NewDecoder(rr.Body).Decode(&ready); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	return ready
}
