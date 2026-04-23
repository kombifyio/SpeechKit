package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestAPIV1ModesReturnsContractsAndSettings(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modes", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		Contracts []speechkit.ModeContract `json:"contracts"`
		Settings  speechkit.ModeSettings   `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Contracts) != 3 {
		t.Fatalf("contracts = %d, want 3", len(payload.Contracts))
	}
	if payload.Settings.Dictation.PrimaryProfileID == "" {
		t.Fatal("dictation primary profile missing")
	}
	if payload.Settings.VoiceAgent.SessionSummary != cfg.VoiceAgent.EnableSessionSummary {
		t.Fatal("voice agent session summary setting did not reflect config")
	}
}

func TestAPIV1ProfilesExposeProviderGroups(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/profiles", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload struct {
		Groups map[string][]string `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, key := range []string{
		"dictate:local_built_in",
		"dictate:local_provider",
		"dictate:cloud_provider",
		"dictate:direct_provider",
		"assist:local_built_in",
		"assist:local_provider",
		"assist:cloud_provider",
		"assist:direct_provider",
		"voice_agent:local_built_in",
		"voice_agent:local_provider",
		"voice_agent:cloud_provider",
		"voice_agent:direct_provider",
	} {
		if len(payload.Groups[key]) == 0 {
			t.Fatalf("provider group %q missing profiles", key)
		}
	}
}

func TestAPIV1ReadinessReportsEveryModeProviderProfile(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/readiness", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var readiness []speechkit.Readiness
	if err := json.Unmarshal(rec.Body.Bytes(), &readiness); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(readiness) < 12 {
		t.Fatalf("readiness entries = %d, want at least 12", len(readiness))
	}
	seen := map[speechkit.Mode]bool{}
	for _, item := range readiness {
		seen[item.Mode] = true
		if item.ProfileID == "" {
			t.Fatal("readiness entry missing profile id")
		}
		if item.SchemaVersion != speechkit.ReadinessSchemaVersion {
			t.Fatalf("readiness schema version = %q, want %q", item.SchemaVersion, speechkit.ReadinessSchemaVersion)
		}
		if len(item.Requirements) == 0 {
			t.Fatalf("readiness entry %q missing structured requirements", item.ProfileID)
		}
	}
	for _, mode := range []speechkit.Mode{speechkit.ModeDictation, speechkit.ModeAssist, speechkit.ModeVoiceAgent} {
		if !seen[mode] {
			t.Fatalf("readiness missing mode %q", mode)
		}
	}
}

func TestAPIV1ReadinessExposesLocalBuiltInArtifacts(t *testing.T) {
	cfg := defaultTestConfig()
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/readiness", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var readiness []speechkit.Readiness
	if err := json.Unmarshal(rec.Body.Bytes(), &readiness); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	foundVoiceBuiltIn := false
	for _, item := range readiness {
		if item.ProfileID != "realtime.builtin.pipeline" {
			continue
		}
		foundVoiceBuiltIn = true
		if len(item.Artifacts) < 2 {
			t.Fatalf("voice local built-in artifacts = %d, want at least 2", len(item.Artifacts))
		}
		if item.Artifacts[0].ID != "llamacpp.gemma-3-4b-it-q4-k-m-voice" {
			t.Fatalf("first voice local artifact = %q", item.Artifacts[0].ID)
		}
	}
	if !foundVoiceBuiltIn {
		t.Fatal("readiness missing realtime.builtin.pipeline")
	}
}

func TestAPIV1ProviderArtifactsEndpointReturnsCatalogAndJobs(t *testing.T) {
	cfg := defaultTestConfig()
	state := &appState{downloads: downloads.NewManager()}
	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/artifacts", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var payload apiV1ProviderArtifactsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	seenWhisper := false
	seenVoice := false
	for _, artifact := range payload.Artifacts {
		switch artifact.ID {
		case "whisper.ggml-large-v3-turbo":
			seenWhisper = true
		case "llamacpp.gemma-3-4b-it-q4-k-m-voice":
			seenVoice = true
		}
	}
	if !seenWhisper {
		t.Fatal("artifact catalog missing Whisper local built-in model")
	}
	if !seenVoice {
		t.Fatal("artifact catalog missing Voice Agent local built-in model")
	}
	if payload.Jobs == nil {
		t.Fatal("artifact payload jobs should be an empty list, not nil")
	}
}

func TestAPIV1PatchModeSettingsUpdatesConfig(t *testing.T) {
	cfg := defaultTestConfig()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{activeProfiles: map[string]string{}}
	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	previousReload := reloadAIRuntime
	reloadAIRuntime = func(ctx context.Context, state *appState, cfg *config.Config) error {
		return nil
	}
	defer func() { reloadAIRuntime = previousReload }()

	body := `{"enabled":false,"hotkey":"ctrl+win+j","hotkeyBehavior":"toggle","ttsEnabled":false}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/modes/assist/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if cfg.General.AssistEnabled {
		t.Fatal("assist enabled = true, want false")
	}
	if got := cfg.General.AssistHotkey; got != "ctrl+win+j" {
		t.Fatalf("assist hotkey = %q, want ctrl+win+j", got)
	}
	if got := cfg.General.AssistHotkeyBehavior; got != config.HotkeyBehaviorToggle {
		t.Fatalf("assist hotkey behavior = %q, want toggle", got)
	}
	if cfg.TTS.Enabled {
		t.Fatal("tts enabled = true, want false")
	}
}

func TestAPIV1DictionaryExportsUsageCountsAndImportsEntries(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.Language = "de"
	cfg.Vocabulary.Dictionary = "kombi fire => Kombify"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	feedbackStore, err := store.NewSQLiteStore(store.StoreConfig{
		SQLitePath: filepath.Join(t.TempDir(), "feedback.db"),
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer feedbackStore.Close()
	if err := syncVocabularyDictionaryStore(context.Background(), feedbackStore, "de", cfg.Vocabulary.Dictionary); err != nil {
		t.Fatalf("sync dictionary: %v", err)
	}
	if err := feedbackStore.RecordUserDictionaryUsage(context.Background(), "Kombify", "de"); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	handler := assetHandler(cfg, cfgPath, &appState{}, &router.Router{}, feedbackStore, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dictionary?language=de", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var exported apiV1DictionaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &exported); err != nil {
		t.Fatalf("decode dictionary: %v", err)
	}
	if len(exported.Entries) != 1 || exported.Entries[0].UsageCount != 1 {
		t.Fatalf("exported entries = %#v", exported.Entries)
	}

	body := `{"language":"de","entries":[{"spoken":"acme os","canonical":"AcmeOS","enabled":true}]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/dictionary", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got, want := cfg.Vocabulary.Dictionary, "acme os => AcmeOS"; got != want {
		t.Fatalf("cfg dictionary = %q, want %q", got, want)
	}
}

func TestAPIV1VoiceSessionsListsStoredSummaries(t *testing.T) {
	cfg := defaultTestConfig()
	feedbackStore, err := store.NewSQLiteStore(store.StoreConfig{
		SQLitePath: filepath.Join(t.TempDir(), "feedback.db"),
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer feedbackStore.Close()
	if _, err := feedbackStore.SaveVoiceAgentSession(context.Background(), store.VoiceAgentSession{
		Language: "en",
		Summary:  store.VoiceAgentSessionSummary{Summary: "Decided to harden live UX."},
	}); err != nil {
		t.Fatalf("SaveVoiceAgentSession: %v", err)
	}

	handler := assetHandler(cfg, filepath.Join(t.TempDir(), "config.toml"), &appState{}, &router.Router{}, feedbackStore, &config.InstallState{Mode: config.InstallModeLocal})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/voice-sessions", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var sessions []store.VoiceAgentSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode voice sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if got := sessions[0].Summary.Summary; got != "Decided to harden live UX." {
		t.Fatalf("summary = %q", got)
	}
}

func TestAPIV1OpenAPISpecDocumentsRegisteredRoutes(t *testing.T) {
	specPath := filepath.Join("..", "..", "docs", "api", "openapi.v1.yaml")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	spec := string(raw)
	for _, needle := range []string{
		"/api/v1/modes:",
		"/api/v1/modes/{mode}/settings:",
		"/api/v1/modes/{mode}/start:",
		"/api/v1/modes/{mode}/stop:",
		"/api/v1/providers/profiles:",
		"/api/v1/providers/readiness:",
		"/api/v1/providers/artifacts:",
		"/api/v1/providers/artifacts/jobs:",
		"/api/v1/providers/artifacts/{artifactId}/download:",
		"/api/v1/providers/artifacts/{artifactId}/select:",
		"/api/v1/providers/{profileId}/activate:",
		"/api/v1/dictionary:",
		"/api/v1/voice-sessions:",
		"X-SpeechKit-Control-Token",
		"speechkit_control_plane",
	} {
		if !strings.Contains(spec, needle) {
			t.Fatalf("openapi spec missing %q", needle)
		}
	}
}
