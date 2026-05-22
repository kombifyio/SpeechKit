package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/transcription"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/secrets"
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
	if len(payload.Contracts) != 4 {
		t.Fatalf("contracts = %d, want 4 (Dictation, Assist, VoiceAgent, TTS)", len(payload.Contracts))
	}
	if payload.Settings.Dictation.PrimaryProfileID == "" {
		t.Fatal("dictation primary profile missing")
	}
	if payload.Settings.VoiceAgent.SessionSummary != cfg.VoiceAgent.EnableSessionSummary {
		t.Fatal("voice agent session summary setting did not reflect config")
	}
}

func TestAPIV1ServerConnectionEnableDoesNotBackfillServerURL(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ServerConnection.URL = ""
	cfg.ServerConnection.Enabled = false
	cfg.ServerConnection.BearerTokenEnv = ""
	cfg.ServerConnection.RequestTimeoutSec = 0
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	enabled := true
	if err := applyAPIV1ServerConnectionPatch(cfgPath, cfg, &appState{}, apiV1ServerConnectionPatch{
		Enabled: &enabled,
	}); err != nil {
		t.Fatalf("applyAPIV1ServerConnectionPatch: %v", err)
	}

	if !cfg.ServerConnection.Enabled {
		t.Fatal("server connection enabled = false, want true")
	}
	if got, want := cfg.ServerConnection.URL, ""; got != want {
		t.Fatalf("server connection URL = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.AuthMode, config.ServerConnectionAuthModeBearer; got != want {
		t.Fatalf("server connection auth mode = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN"; got != want {
		t.Fatalf("bearer token env = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.RequestTimeoutSec, 30; got != want {
		t.Fatalf("request timeout = %d, want %d", got, want)
	}
}

func TestServerConnectionSettingMarksStoredTokenAsSet(t *testing.T) {
	restore := secrets.UseMemoryStoreForTests()
	defer restore()
	t.Setenv("SC_TEST_STORED_SERVER_TOKEN", "")
	if err := secrets.SetNamedSecret("SC_TEST_STORED_SERVER_TOKEN", "stored-token"); err != nil {
		t.Fatalf("set named secret: %v", err)
	}

	setting := serverConnectionSettingFromConfig(config.ServerConnectionConfig{
		Enabled:        false,
		URL:            "https://speechkit.example.com",
		BearerTokenEnv: "SC_TEST_STORED_SERVER_TOKEN",
		AuthMode:       config.ServerConnectionAuthModeBearer,
	})

	if !setting.BearerTokenSet {
		t.Fatal("BearerTokenSet = false, want true for stored named secret")
	}
	if setting.AuthMode != config.ServerConnectionAuthModeBearer {
		t.Fatalf("AuthMode = %q", setting.AuthMode)
	}
	if len(setting.Targets) != 1 {
		t.Fatalf("targets = %#v, want one configured server target", setting.Targets)
	}
	if !setting.Targets[0].BearerTokenSet {
		t.Fatal("target BearerTokenSet = false, want true for stored named secret")
	}
}

func TestAPIV1ServerConnectionPatchRegistersAndActivatesTarget(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ServerConnection.URL = "http://127.0.0.1:8080"
	cfg.ServerConnection.BearerTokenEnv = "SPEECHKIT_LOCAL_TOKEN"
	cfg.ServerConnection.AuthMode = config.ServerConnectionAuthModeBearer
	cfg.ServerConnection.RequestTimeoutSec = 30
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	activeTargetID := "staging"
	targets := []apiV1ServerConnectionTargetPatch{
		{
			ID:                "local",
			Label:             "Local server",
			URL:               "http://127.0.0.1:8080",
			BearerTokenEnv:    "SPEECHKIT_LOCAL_TOKEN",
			AuthMode:          config.ServerConnectionAuthModeBearer,
			FallbackToLocal:   true,
			RequestTimeoutSec: 30,
		},
		{
			ID:                "staging",
			Label:             "Staging server",
			URL:               "https://speechkit-staging.example.com",
			BearerTokenEnv:    "SPEECHKIT_STAGING_TOKEN",
			AuthMode:          config.ServerConnectionAuthModeAPIKey,
			FallbackToLocal:   false,
			RequestTimeoutSec: 10,
		},
	}
	if err := applyAPIV1ServerConnectionPatch(cfgPath, cfg, &appState{}, apiV1ServerConnectionPatch{
		ActiveTargetID: &activeTargetID,
		Targets:        &targets,
	}); err != nil {
		t.Fatalf("applyAPIV1ServerConnectionPatch: %v", err)
	}

	if got, want := cfg.ServerConnection.ActiveTargetID, "staging"; got != want {
		t.Fatalf("active target id = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.URL, "https://speechkit-staging.example.com"; got != want {
		t.Fatalf("active URL = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.AuthMode, config.ServerConnectionAuthModeAPIKey; got != want {
		t.Fatalf("active auth mode = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.BearerTokenEnv, "SPEECHKIT_STAGING_TOKEN"; got != want {
		t.Fatalf("active token env = %q, want %q", got, want)
	}
	if cfg.ServerConnection.FallbackToLocal {
		t.Fatal("active fallback_to_local = true, want false")
	}
	if got, want := cfg.ServerConnection.RequestTimeoutSec, 10; got != want {
		t.Fatalf("active timeout = %d, want %d", got, want)
	}
	if got, want := len(cfg.ServerConnection.Targets), 2; got != want {
		t.Fatalf("registered targets = %d, want %d", got, want)
	}
}

func TestAPIV1ServerConnectionSmokeTestsHealthAndReadiness(t *testing.T) {
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/healthz", "/readyz":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := `{"url":` + strconv.Quote(server.URL) + `,"token":"server-token","authMode":"bearer"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/server-connection/smoke", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handleAPIV1ServerConnectionSmoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload apiV1ServerConnectionSmokeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode smoke response: %v", err)
	}
	if !payload.OK || payload.HealthStatus != http.StatusOK || payload.ReadyStatus != http.StatusOK {
		t.Fatalf("smoke response = %#v, want ok health+ready", payload)
	}
	if sawAuth != "Bearer server-token" {
		t.Fatalf("auth header = %q, want bearer token", sawAuth)
	}
}

func TestAPIV1ServerConnectionPatchAppliesAPIKeyAuthMode(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ServerConnection.URL = "https://speechkit.example.com"
	cfg.ServerConnection.AuthMode = config.ServerConnectionAuthModeBearer
	cfg.ServerConnection.BearerTokenEnv = "SPEECHKIT_SERVER_TOKEN"
	cfgPath := filepath.Join(t.TempDir(), "config.toml")

	authMode := config.ServerConnectionAuthModeAPIKey
	tokenEnv := ""
	if err := applyAPIV1ServerConnectionPatch(cfgPath, cfg, &appState{}, apiV1ServerConnectionPatch{
		AuthMode:       &authMode,
		BearerTokenEnv: &tokenEnv,
	}); err != nil {
		t.Fatalf("applyAPIV1ServerConnectionPatch: %v", err)
	}

	if got, want := cfg.ServerConnection.AuthMode, config.ServerConnectionAuthModeAPIKey; got != want {
		t.Fatalf("auth mode = %q, want %q", got, want)
	}
	if got, want := cfg.ServerConnection.BearerTokenEnv, "SPEECHKIT_SERVER_TOKEN"; got != want {
		t.Fatalf("token env = %q, want %q", got, want)
	}
}

func TestAPIV1ModeSourceServerRejectsWhenServerURLMissing(t *testing.T) {
	// Reproduces the bug where the UI showed a green "token set" badge
	// (SPEECHKIT_SERVER_TOKEN resolved in env) but flipping a mode to
	// "server" with no registered target produced the raw Go error
	// "serverclient: server_connection.url is required when enabled".
	// The patch now must fail with a user-facing message that names
	// the next action.
	t.Setenv("SPEECHKIT_SERVER_TOKEN", "secret")
	cfg := defaultTestConfig()
	cfg.ServerConnection = config.ServerConnectionConfig{
		Enabled:        false,
		URL:            "",
		BearerTokenEnv: "SPEECHKIT_SERVER_TOKEN",
		AuthMode:       config.ServerConnectionAuthModeBearer,
	}
	cfg.ModelSelection.Dictate.ModeSource = config.ModeSourceLocal
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	modeSource := config.ModeSourceServer

	err := applyAPIV1ModeSettingsPatch(
		context.Background(),
		cfgPath,
		cfg,
		&appState{},
		&router.Router{},
		modeDictate,
		apiV1ModeSettingsPatch{ModeSource: &modeSource},
	)
	if err == nil {
		t.Fatal("err = nil, want user-facing validation error")
	}
	if strings.Contains(err.Error(), "serverclient:") {
		t.Fatalf("error %q leaks the internal package name; should be user-friendly", err.Error())
	}
	if !strings.Contains(err.Error(), "Server Target") && !strings.Contains(err.Error(), "server URL") {
		t.Fatalf("error %q does not point the user at the next action", err.Error())
	}
}

func TestAPIV1ModeSourceServerRejectsWhenTokenMissing(t *testing.T) {
	// URL is configured but the env var holding the bearer token is empty.
	// The patch must surface the env var name so the operator knows what
	// to set.
	t.Setenv("SC_TEST_NO_TOKEN", "")
	cfg := defaultTestConfig()
	cfg.ServerConnection = config.ServerConnectionConfig{
		Enabled:        false,
		URL:            "http://127.0.0.1:8080",
		BearerTokenEnv: "SC_TEST_NO_TOKEN",
		AuthMode:       config.ServerConnectionAuthModeBearer,
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	modeSource := config.ModeSourceServer

	err := applyAPIV1ModeSettingsPatch(
		context.Background(),
		cfgPath,
		cfg,
		&appState{},
		&router.Router{},
		modeDictate,
		apiV1ModeSettingsPatch{ModeSource: &modeSource},
	)
	if err == nil {
		t.Fatal("err = nil, want user-facing validation error")
	}
	if !strings.Contains(err.Error(), "SC_TEST_NO_TOKEN") {
		t.Fatalf("error %q does not name the missing env var", err.Error())
	}
}

func TestAPIV1VoiceAgentModeSourceSwitchResetsCachedSession(t *testing.T) {
	t.Setenv("SC_TEST_SERVER_TOKEN", "secret")
	cfg := defaultTestConfig()
	cfg.ModelSelection.VoiceAgent.ModeSource = config.ModeSourceLocal
	cfg.ServerConnection = config.ServerConnectionConfig{
		Enabled:        false,
		URL:            "http://localhost:8080",
		BearerTokenEnv: "SC_TEST_SERVER_TOKEN",
		AuthMode:       config.ServerConnectionAuthModeBearer,
	}
	state := &appState{
		voiceAgentSession: prepareVoiceAgentSession(&appState{}, cfg),
	}
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	modeSource := config.ModeSourceServer

	if err := applyAPIV1ModeSettingsPatch(
		context.Background(),
		cfgPath,
		cfg,
		state,
		&router.Router{},
		modeVoiceAgent,
		apiV1ModeSettingsPatch{ModeSource: &modeSource},
	); err != nil {
		t.Fatalf("applyAPIV1ModeSettingsPatch: %v", err)
	}

	if got := state.voiceAgentSession; got != nil {
		t.Fatalf("voiceAgentSession = %#v, want nil after switching voice_agent to server", got)
	}
	if !currentServerDelegates(state).hasVoiceAgent() {
		t.Fatal("server voice agent delegate not enabled after mode_source=server")
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
		if item.Artifacts[0].ID != "llamacpp.gemma-4-e4b-it-q4-k-m-voice" {
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
		case "llamacpp.gemma-4-e4b-it-q4-k-m-voice":
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

	body := `{"enabled":false,"hotkey":"win+alt+j","hotkeyBehavior":"toggle","ttsEnabled":false}`
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
	if got := cfg.General.AssistHotkey; got != "win+alt+j" {
		t.Fatalf("assist hotkey = %q, want win+alt+j", got)
	}
	if got := cfg.General.AssistHotkeyBehavior; got != config.HotkeyBehaviorToggle {
		t.Fatalf("assist hotkey behavior = %q, want toggle", got)
	}
	if cfg.TTS.Enabled {
		t.Fatal("tts enabled = true, want false")
	}
}

func TestAPIV1PatchVoiceAgentSequenceUpdatesConfig(t *testing.T) {
	cfg := defaultTestConfig()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	state := &appState{activeProfiles: map[string]string{}}
	handler := assetHandler(cfg, cfgPath, state, &router.Router{}, nil, &config.InstallState{Mode: config.InstallModeLocal})

	previousReload := reloadAIRuntime
	reloadAIRuntime = func(ctx context.Context, state *appState, cfg *config.Config) error {
		return nil
	}
	defer func() { reloadAIRuntime = previousReload }()

	body := `{"agentProfileId":"support_companion","agentSequenceId":"support_companion_sequence"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/modes/voice_agent/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got, want := cfg.VoiceAgent.AgentProfileID, "support_companion"; got != want {
		t.Fatalf("agent profile = %q, want %q", got, want)
	}
	if got, want := cfg.VoiceAgent.AgentSequenceID, "support_companion_sequence"; got != want {
		t.Fatalf("agent sequence = %q, want %q", got, want)
	}

	var payload speechkit.VoiceAgentSetting
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got, want := payload.AgentSequenceID, "support_companion_sequence"; got != want {
		t.Fatalf("response agent sequence = %q, want %q", got, want)
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
	if err := transcription.SyncVocabularyDictionaryStore(context.Background(), feedbackStore, "de", cfg.Vocabulary.Dictionary); err != nil {
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
	id, err := feedbackStore.SaveVoiceAgentSession(context.Background(), store.VoiceAgentSession{
		Language:   "en",
		Transcript: "User: harden UX",
		Turns:      []store.VoiceAgentTurn{{Role: "user", Text: "harden UX"}},
		Summary: store.VoiceAgentSessionSummary{
			Summary:   "Decided to harden live UX.",
			NextSteps: []string{"Build detail route"},
		},
	})
	if err != nil {
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
	if sessions[0].Transcript != "" || len(sessions[0].Turns) != 0 || len(sessions[0].Summary.NextSteps) != 0 {
		t.Fatalf("list response should be light, got %+v", sessions[0])
	}

	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/voice-sessions/%d", id), http.NoBody)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var detail store.VoiceAgentSession
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode voice session detail: %v", err)
	}
	if detail.Transcript == "" || len(detail.Turns) != 1 || len(detail.Summary.NextSteps) != 1 {
		t.Fatalf("detail response missing heavy fields: %+v", detail)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/voice-sessions/999999", http.NoBody)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing detail status = %d, want 404", rec.Code)
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
		"/api/v1/server-connection:",
		"/api/v1/server-connection/smoke:",
		"/api/v1/providers/profiles:",
		"/api/v1/providers/readiness:",
		"/api/v1/providers/artifacts:",
		"/api/v1/providers/artifacts/jobs:",
		"/api/v1/providers/artifacts/{artifactId}/download:",
		"/api/v1/providers/artifacts/{artifactId}/select:",
		"/api/v1/providers/{profileId}/activate:",
		"/api/v1/dictionary:",
		"/api/v1/voice-sessions:",
		"/api/v1/voice-sessions/{sessionId}:",
		"X-SpeechKit-Control-Token",
		"speechkit_control_plane",
	} {
		if !strings.Contains(spec, needle) {
			t.Fatalf("openapi spec missing %q", needle)
		}
	}
}
