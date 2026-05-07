//go:build linux

package catalog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"

	"github.com/kombifyio/SpeechKit/internal/config"
)

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
