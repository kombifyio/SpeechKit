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

func TestProviderOptionsExportsDefaultManifests(t *testing.T) {
	h := New(&config.Config{}, func(string) string { return "ok" }, "test")
	req := httptest.NewRequest(http.MethodGet, "/v1/catalog/provider-options", nil)
	rec := httptest.NewRecorder()
	h.providerOptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Schema    string `json:"schema"`
		Manifests []struct {
			Provider string `json:"provider"`
			Modality string `json:"modality"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Schema != "speechkit.provider_options.v1" {
		t.Fatalf("schema = %q", body.Schema)
	}
	if len(body.Manifests) == 0 {
		t.Fatal("expected manifests")
	}
	foundHFTTS := false
	for _, m := range body.Manifests {
		if m.Provider == "huggingface" && m.Modality == "tts" {
			foundHFTTS = true
		}
	}
	if !foundHFTTS {
		t.Fatal("expected huggingface TTS manifest in catalog export")
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
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.google.latest-long"
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTAPIKeyEnv = "SPEECHKIT_TEST_GOOGLE_STT_KEY"

	h := New(cfg, func(component string) string {
		if component == "mode.dictation" {
			return "ok"
		}
		return ""
	}, "test")

	ready := getReadiness(t, h, "/v1/catalog/profiles/stt.google.latest-long/readiness")
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
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.google.latest-long"
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.APIKeyEnv = "GOOGLE_AI_API_KEY"
	cfg.Providers.Google.STTAPIKeyEnv = "SPEECHKIT_TEST_GOOGLE_STT_KEY"

	h := New(cfg, func(component string) string {
		if component == "mode.dictation" {
			return "ok"
		}
		return ""
	}, "test")

	ready := getReadiness(t, h, "/v1/catalog/profiles/stt.google.latest-long/readiness")
	if !ready.CredentialsReady || !ready.Configured || !ready.Ready {
		t.Fatalf("readiness = %+v, want Google STT ready with dedicated key", ready)
	}
}

func TestReadinessGoogleSTTAcceptsLegacyProfileID(t *testing.T) {
	t.Setenv("SPEECHKIT_TEST_GOOGLE_STT_KEY", "speech-key")

	cfg := &config.Config{}
	cfg.Server.Modes = []string{"dictation"}
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.google.chirp-3"
	cfg.Providers.Google.Enabled = true
	cfg.Providers.Google.STTAPIKeyEnv = "SPEECHKIT_TEST_GOOGLE_STT_KEY"

	h := New(cfg, func(component string) string {
		if component == "mode.dictation" {
			return "ok"
		}
		return ""
	}, "test")

	ready := getReadiness(t, h, "/v1/catalog/profiles/stt.google.chirp-3/readiness")
	if ready.ProfileID != "stt.google.latest-long" || !ready.Ready {
		t.Fatalf("readiness = %+v, want legacy ID to resolve to latest-long ready profile", ready)
	}
}

// Voice Agent Active must follow the provider that actually serves a default
// session (cfg.VoiceAgent.Provider), not ModelSelection.VoiceAgent — serving
// never reads the latter (kombify-SpeechKit-5nt5).
func TestReadinessVoiceAgentActiveFollowsServingProviderKombifyShape(t *testing.T) {
	cfg := &config.Config{}
	// kombify-default deployment shape: kombify defaults flip the provider to
	// deepgram while the vanilla ModelSelection default still names the
	// builtin pipeline.
	cfg.VoiceAgent.Provider = "deepgram"
	cfg.Providers.Deepgram.Enabled = true
	cfg.ModelSelection.VoiceAgent.PrimaryProfileID = config.DefaultVoiceAgentPrimaryProfileID

	h := New(cfg, func(string) string { return "ok" }, "test")

	deepgram := getReadiness(t, h, "/v1/catalog/profiles/realtime.deepgram.voice-agent/readiness")
	if !deepgram.Active {
		t.Fatalf("deepgram voice-agent profile Active = false, want true (it serves default sessions): %+v", deepgram)
	}
	pipeline := getReadiness(t, h, "/v1/catalog/profiles/realtime.builtin.pipeline/readiness")
	if pipeline.Active {
		t.Fatalf("builtin pipeline Active = true although deepgram serves default sessions: %+v", pipeline)
	}
}

func TestReadinessVoiceAgentActiveFollowsServingProviderVanillaShape(t *testing.T) {
	cfg := &config.Config{}
	// Vanilla deployment shape: no provider configured serves Gemini Live,
	// while ModelSelection.VoiceAgent defaults to the builtin pipeline.
	cfg.Providers.Google.Enabled = true
	cfg.ModelSelection.VoiceAgent.PrimaryProfileID = config.DefaultVoiceAgentPrimaryProfileID

	h := New(cfg, func(string) string { return "ok" }, "test")

	gemini := getReadiness(t, h, "/v1/catalog/profiles/realtime.google.gemini-native-audio/readiness")
	if !gemini.Active {
		t.Fatalf("gemini voice-agent profile Active = false, want true (empty provider serves Gemini Live): %+v", gemini)
	}
	pipeline := getReadiness(t, h, "/v1/catalog/profiles/realtime.builtin.pipeline/readiness")
	if pipeline.Active {
		t.Fatalf("builtin pipeline Active = true although gemini serves default sessions: %+v", pipeline)
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
