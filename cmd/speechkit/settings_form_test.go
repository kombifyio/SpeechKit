package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestParseSettingsFormDefaults(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.DictateHotkey = "win+alt"
	cfg.General.AgentHotkey = "ctrl+shift+k"
	cfg.General.ActiveMode = "dictate"
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"
	cfg.Store.SQLitePath = "/tmp/test.db"

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	if form.DictateHotkey != "win+alt" {
		t.Errorf("DictateHotkey = %q, want %q", form.DictateHotkey, "win+alt")
	}
	if form.AgentHotkey != "ctrl+shift+k" {
		t.Errorf("AgentHotkey = %q, want %q", form.AgentHotkey, "ctrl+shift+k")
	}
	if form.ActiveMode != "dictate" {
		t.Errorf("ActiveMode = %q, want %q", form.ActiveMode, "dictate")
	}
	if form.Visualizer != "pill" {
		t.Errorf("Visualizer = %q, want %q", form.Visualizer, "pill")
	}
	if form.StoreBackend != "sqlite" {
		t.Errorf("StoreBackend = %q, want %q", form.StoreBackend, "sqlite")
	}
}

func TestParseSettingsFormOverrides(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.DictateHotkey = "win+alt"
	cfg.General.AgentHotkey = "ctrl+shift+k"
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"dictate_hotkey":     {"ctrl+space"},
		"agent_hotkey":       {"ctrl+shift+j"},
		"active_mode":        {"agent"},
		"overlay_enabled":    {"1"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
		"overlay_position":   {"bottom"},
		"overlay_movable":    {"1"},
		"overlay_free_x":     {"100"},
		"overlay_free_y":     {"200"},
		"store_backend":      {"sqlite"},
		"audio_device_id":    {"dev-123"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	if form.DictateHotkey != "ctrl+space" {
		t.Errorf("DictateHotkey = %q, want %q", form.DictateHotkey, "ctrl+space")
	}
	if form.AgentHotkey != "ctrl+shift+j" {
		t.Errorf("AgentHotkey = %q, want %q", form.AgentHotkey, "ctrl+shift+j")
	}
	if form.ActiveMode != "agent" {
		t.Errorf("ActiveMode = %q, want %q", form.ActiveMode, "agent")
	}
	if !form.OverlayEnabled {
		t.Error("expected OverlayEnabled=true")
	}
	if form.OverlayPosition != "bottom" {
		t.Errorf("OverlayPosition = %q, want %q", form.OverlayPosition, "bottom")
	}
	if !form.OverlayMovable {
		t.Error("expected OverlayMovable=true")
	}
	if form.OverlayFreeX != 100 {
		t.Errorf("OverlayFreeX = %d, want %d", form.OverlayFreeX, 100)
	}
	if form.OverlayFreeY != 200 {
		t.Errorf("OverlayFreeY = %d, want %d", form.OverlayFreeY, 200)
	}
	if form.AudioDeviceID != "dev-123" {
		t.Errorf("AudioDeviceID = %q, want %q", form.AudioDeviceID, "dev-123")
	}
}

func TestParseSettingsFormRejectsUnsupportedStoreBackend(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"store_backend": {"redis"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	_, errMsg := parseSettingsForm(req, cfg)
	if errMsg != msgUnsupportedStore {
		t.Errorf("expected %q, got %q", msgUnsupportedStore, errMsg)
	}
}

func TestParseSettingsFormRejectsPostgresWithoutDSN(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"store_backend": {"postgres"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	_, errMsg := parseSettingsForm(req, cfg)
	if errMsg != msgPostgresDSNReq {
		t.Errorf("expected %q, got %q", msgPostgresDSNReq, errMsg)
	}
}

func TestParseSettingsFormActiveModeDefaultsToDictate(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"active_mode": {"invalid"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if form.ActiveMode != "dictate" {
		t.Errorf("ActiveMode = %q, want %q", form.ActiveMode, "dictate")
	}
}

func TestParseSettingsFormRejectsInvalidOverlayPosition(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"overlay_position": {"center"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	_, errMsg := parseSettingsForm(req, cfg)
	if errMsg != msgUnsupportedPos {
		t.Errorf("expected %q, got %q", msgUnsupportedPos, errMsg)
	}
}

func TestBuildNextConfig(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.Hotkey = "old"
	cfg.Store.Backend = "sqlite"

	form := settingsFormData{
		DictateHotkey:        "ctrl+space",
		AgentHotkey:          "ctrl+shift+k",
		ActiveMode:           "agent",
		AudioDeviceID:        "dev-1",
		HFModel:              "whisper-large",
		OverlayEnabled:       true,
		Visualizer:           "pill",
		Design:               "default",
		OverlayPosition:      "bottom",
		OverlayMovable:       true,
		OverlayFreeX:         50,
		OverlayFreeY:         60,
		StoreBackend:         "sqlite",
		StoreSQLitePath:      "/tmp/new.db",
		StoreSaveAudio:       true,
		StoreAudioRetention:  30,
		StoreMaxAudioStorage: 500,
		VocabularyDictionary: "hello=hi",
	}

	result := buildNextConfig(form, cfg)

	if result.General.Hotkey != "ctrl+space" {
		t.Errorf("Hotkey = %q, want %q", result.General.Hotkey, "ctrl+space")
	}
	if result.General.DictateHotkey != "ctrl+space" {
		t.Errorf("DictateHotkey = %q", result.General.DictateHotkey)
	}
	if result.General.AgentHotkey != "ctrl+shift+k" {
		t.Errorf("AgentHotkey = %q", result.General.AgentHotkey)
	}
	if result.General.ActiveMode != "agent" {
		t.Errorf("ActiveMode = %q", result.General.ActiveMode)
	}
	if result.Audio.DeviceID != "dev-1" {
		t.Errorf("DeviceID = %q", result.Audio.DeviceID)
	}
	if result.UI.OverlayEnabled != true {
		t.Error("expected OverlayEnabled=true")
	}
	if result.UI.OverlayPosition != "bottom" {
		t.Errorf("OverlayPosition = %q", result.UI.OverlayPosition)
	}
	if result.Store.Backend != "sqlite" {
		t.Errorf("StoreBackend = %q", result.Store.Backend)
	}
	if result.Store.SQLitePath != "/tmp/new.db" {
		t.Errorf("SQLitePath = %q", result.Store.SQLitePath)
	}
	if result.Store.SaveAudio != true {
		t.Error("expected SaveAudio=true")
	}
	if result.Store.AudioRetentionDays != 30 {
		t.Errorf("AudioRetentionDays = %d", result.Store.AudioRetentionDays)
	}
	if result.Store.MaxAudioStorageMB != 500 {
		t.Errorf("MaxAudioStorageMB = %d", result.Store.MaxAudioStorageMB)
	}
	if result.Feedback.SaveAudio != true {
		t.Error("expected Feedback.SaveAudio=true")
	}
	if result.Feedback.DBPath != "/tmp/new.db" {
		t.Errorf("Feedback.DBPath = %q", result.Feedback.DBPath)
	}
	if result.Vocabulary.Dictionary != "hello=hi" {
		t.Errorf("Dictionary = %q", result.Vocabulary.Dictionary)
	}
}

func TestBuildNextConfigPostgresDoesNotSetFeedbackDBPath(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Feedback.DBPath = "/old/path.db"

	form := settingsFormData{
		DictateHotkey:    "win+alt",
		AgentHotkey:      "ctrl+shift+k",
		ActiveMode:       "dictate",
		Visualizer:       "pill",
		Design:           "default",
		OverlayPosition:  "top",
		StoreBackend:     "postgres",
		StorePostgresDSN: "postgres://localhost/test",
	}

	result := buildNextConfig(form, cfg)
	// When backend is postgres, Feedback.DBPath should NOT be updated from StorePostgresDSN.
	if result.Feedback.DBPath != "/old/path.db" {
		t.Errorf("Feedback.DBPath = %q, expected unchanged from original", result.Feedback.DBPath)
	}
}

func TestNormalizeVocabularyDictionary(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello\r\nworld", "hello\nworld"},
		{"hello\rworld", "hello\nworld"},
		{"  hello  ", "hello"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeVocabularyDictionary(tt.input)
		if got != tt.want {
			t.Errorf("normalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
