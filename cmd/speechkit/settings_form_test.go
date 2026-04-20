package main

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestParseSettingsFormDefaults(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.DictateHotkey = "win+alt"
	cfg.General.AssistHotkey = "ctrl+win"
	cfg.General.VoiceAgentHotkey = "ctrl+shift"
	cfg.General.AgentHotkey = "ctrl+win"
	cfg.General.AgentMode = "assist"
	cfg.General.ActiveMode = "none"
	cfg.General.DictateEnabled = true
	cfg.General.AssistEnabled = true
	cfg.General.VoiceAgentEnabled = true
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.AssistOverlayMode = config.OverlayFeedbackModeBigProductivity
	cfg.UI.VoiceAgentOverlayMode = config.OverlayFeedbackModeSmallFeedback
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
	if form.AssistHotkey != "ctrl+win" {
		t.Errorf("AssistHotkey = %q, want %q", form.AssistHotkey, "ctrl+win")
	}
	if form.VoiceAgentHotkey != "ctrl+shift" {
		t.Errorf("VoiceAgentHotkey = %q, want %q", form.VoiceAgentHotkey, "ctrl+shift")
	}
	if form.AgentHotkey != "ctrl+win" {
		t.Errorf("AgentHotkey = %q, want %q", form.AgentHotkey, "ctrl+win")
	}
	if form.AgentMode != "assist" {
		t.Errorf("AgentMode = %q, want %q", form.AgentMode, "assist")
	}
	if form.ActiveMode != "none" {
		t.Errorf("ActiveMode = %q, want %q", form.ActiveMode, "none")
	}
	if !form.DictateEnabled || !form.AssistEnabled || !form.VoiceAgentEnabled {
		t.Fatal("expected all mode enabled flags to stay true by default")
	}
	if form.Visualizer != "pill" {
		t.Errorf("Visualizer = %q, want %q", form.Visualizer, "pill")
	}
	if form.AssistOverlayMode != config.OverlayFeedbackModeBigProductivity {
		t.Errorf("AssistOverlayMode = %q, want %q", form.AssistOverlayMode, config.OverlayFeedbackModeBigProductivity)
	}
	if form.VoiceAgentOverlayMode != config.OverlayFeedbackModeSmallFeedback {
		t.Errorf("VoiceAgentOverlayMode = %q, want %q", form.VoiceAgentOverlayMode, config.OverlayFeedbackModeSmallFeedback)
	}
	if form.StoreBackend != "sqlite" {
		t.Errorf("StoreBackend = %q, want %q", form.StoreBackend, "sqlite")
	}
	if form.VoiceAgentHotkeyBehavior != config.HotkeyBehaviorPushToTalk {
		t.Errorf("VoiceAgentHotkeyBehavior = %q, want %q", form.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorPushToTalk)
	}
	if form.VoiceAgentCloseBehavior != config.VoiceAgentCloseBehaviorContinue {
		t.Errorf("VoiceAgentCloseBehavior = %q, want %q", form.VoiceAgentCloseBehavior, config.VoiceAgentCloseBehaviorContinue)
	}
	if form.VoiceAgentRefinementPrompt != "" {
		t.Errorf("VoiceAgentRefinementPrompt = %q, want empty", form.VoiceAgentRefinementPrompt)
	}
	if !form.VoiceAgentSessionSummary {
		t.Error("VoiceAgentSessionSummary = false, want true by default")
	}
	if form.AutoStartOnLaunch {
		t.Error("AutoStartOnLaunch = true, want false by default")
	}
}

func TestParseSettingsFormOverrides(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.DictateHotkey = "win+alt"
	cfg.General.AssistHotkey = "ctrl+win"
	cfg.General.VoiceAgentHotkey = "ctrl+shift"
	cfg.General.AgentHotkey = "ctrl+win"
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"dictate_hotkey":                {"ctrl+shift+d"},
		"assist_hotkey":                 {"ctrl+win+j"},
		"voice_agent_hotkey":            {"win+alt+k"},
		"agent_mode":                    {"voice_agent"},
		"active_mode":                   {"none"},
		"dictate_enabled":               {"1"},
		"assist_enabled":                {"1"},
		"voice_agent_enabled":           {"0"},
		"overlay_enabled":               {"1"},
		"overlay_visualizer":            {"pill"},
		"overlay_design":                {"default"},
		"assist_overlay_mode":           {config.OverlayFeedbackModeSmallFeedback},
		"voice_agent_overlay_mode":      {config.OverlayFeedbackModeBigProductivity},
		"overlay_position":              {"bottom"},
		"overlay_movable":               {"1"},
		"overlay_free_x":                {"100"},
		"overlay_free_y":                {"200"},
		"voice_agent_hotkey_behavior":   {"toggle"},
		"voice_agent_close_behavior":    {"new_chat"},
		"voice_agent_refinement_prompt": {"Address the user by first name."},
		"voice_agent_session_summary":   {"0"},
		"auto_start_on_launch":          {"1"},
		"store_backend":                 {"sqlite"},
		"audio_device_id":               {"dev-123"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	if form.DictateHotkey != "ctrl+shift+d" {
		t.Errorf("DictateHotkey = %q, want %q", form.DictateHotkey, "ctrl+shift+d")
	}
	if form.AssistHotkey != "ctrl+win+j" {
		t.Errorf("AssistHotkey = %q, want %q", form.AssistHotkey, "ctrl+win+j")
	}
	if form.VoiceAgentHotkey != "win+alt+k" {
		t.Errorf("VoiceAgentHotkey = %q, want %q", form.VoiceAgentHotkey, "win+alt+k")
	}
	if form.AgentHotkey != "win+alt+k" {
		t.Errorf("AgentHotkey = %q, want %q", form.AgentHotkey, "win+alt+k")
	}
	if form.AgentMode != "voice_agent" {
		t.Errorf("AgentMode = %q, want %q", form.AgentMode, "voice_agent")
	}
	if form.ActiveMode != "none" {
		t.Errorf("ActiveMode = %q, want %q", form.ActiveMode, "none")
	}
	if !form.DictateEnabled || !form.AssistEnabled || form.VoiceAgentEnabled {
		t.Fatalf("unexpected mode enabled flags: %+v", form)
	}
	if !form.OverlayEnabled {
		t.Error("expected OverlayEnabled=true")
	}
	if form.AssistOverlayMode != config.OverlayFeedbackModeSmallFeedback {
		t.Errorf("AssistOverlayMode = %q, want %q", form.AssistOverlayMode, config.OverlayFeedbackModeSmallFeedback)
	}
	if form.VoiceAgentOverlayMode != config.OverlayFeedbackModeBigProductivity {
		t.Errorf("VoiceAgentOverlayMode = %q, want %q", form.VoiceAgentOverlayMode, config.OverlayFeedbackModeBigProductivity)
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
	if form.VoiceAgentHotkeyBehavior != config.HotkeyBehaviorToggle {
		t.Errorf("VoiceAgentHotkeyBehavior = %q, want %q", form.VoiceAgentHotkeyBehavior, config.HotkeyBehaviorToggle)
	}
	if form.VoiceAgentCloseBehavior != config.VoiceAgentCloseBehaviorNewChat {
		t.Errorf("VoiceAgentCloseBehavior = %q, want %q", form.VoiceAgentCloseBehavior, config.VoiceAgentCloseBehaviorNewChat)
	}
	if form.VoiceAgentRefinementPrompt != "Address the user by first name." {
		t.Errorf("VoiceAgentRefinementPrompt = %q", form.VoiceAgentRefinementPrompt)
	}
	if form.VoiceAgentSessionSummary {
		t.Error("VoiceAgentSessionSummary = true, want false")
	}
	if !form.AutoStartOnLaunch {
		t.Error("AutoStartOnLaunch = false, want true")
	}
	if form.AudioDeviceID != "dev-123" {
		t.Errorf("AudioDeviceID = %q, want %q", form.AudioDeviceID, "dev-123")
	}
	next := buildNextConfig(form, cfg)
	if next.UI.AssistOverlayMode != config.OverlayFeedbackModeSmallFeedback {
		t.Errorf("next AssistOverlayMode = %q, want %q", next.UI.AssistOverlayMode, config.OverlayFeedbackModeSmallFeedback)
	}
	if next.UI.VoiceAgentOverlayMode != config.OverlayFeedbackModeBigProductivity {
		t.Errorf("next VoiceAgentOverlayMode = %q, want %q", next.UI.VoiceAgentOverlayMode, config.OverlayFeedbackModeBigProductivity)
	}
}

func TestParseSettingsFormModelDownloadDir(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.ModelDownloadDir = filepath.Join("C:", "SpeechKit", "models")
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	want := filepath.Join("D:", "AI", "SpeechKitModels")
	formValues := url.Values{
		"model_download_dir": {want},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
		"overlay_position":   {"top"},
		"store_backend":      {"sqlite"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if form.ModelDownloadDir != want {
		t.Fatalf("ModelDownloadDir = %q, want %q", form.ModelDownloadDir, want)
	}

	next := buildNextConfig(form, cfg)
	if next.General.ModelDownloadDir != want {
		t.Fatalf("next General.ModelDownloadDir = %q, want %q", next.General.ModelDownloadDir, want)
	}
}

func TestParseSettingsFormSupportsIndependentModeHotkeysAndNone(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.DictateHotkey = "win+alt"
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"dictate_hotkey":     {"win+alt"},
		"assist_hotkey":      {"ctrl+win"},
		"voice_agent_hotkey": {"ctrl+shift"},
		"active_mode":        {"none"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
		"overlay_position":   {"top"},
		"store_backend":      {"sqlite"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	if form.AssistHotkey != "ctrl+win" {
		t.Errorf("AssistHotkey = %q, want %q", form.AssistHotkey, "ctrl+win")
	}
	if form.VoiceAgentHotkey != "ctrl+shift" {
		t.Errorf("VoiceAgentHotkey = %q, want %q", form.VoiceAgentHotkey, "ctrl+shift")
	}
	if form.ActiveMode != "none" {
		t.Errorf("ActiveMode = %q, want %q", form.ActiveMode, "none")
	}
}

func TestParseSettingsFormMapsLegacyAgentHotkeyToAssistHotkey(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"dictate_hotkey":     {"win+alt"},
		"agent_hotkey":       {"ctrl+win+j"},
		"agent_mode":         {"assist"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
		"overlay_position":   {"top"},
		"store_backend":      {"sqlite"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	if form.AssistHotkey != "ctrl+win+j" {
		t.Errorf("AssistHotkey = %q, want %q", form.AssistHotkey, "ctrl+win+j")
	}
	if form.VoiceAgentHotkey != "" {
		t.Errorf("VoiceAgentHotkey = %q, want empty", form.VoiceAgentHotkey)
	}
}

func TestParseSettingsFormMapsLegacyAgentHotkeyToVoiceAgentHotkey(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"dictate_hotkey":     {"win+alt"},
		"agent_hotkey":       {"ctrl+shift+k"},
		"agent_mode":         {"voice_agent"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
		"overlay_position":   {"top"},
		"store_backend":      {"sqlite"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	if form.AssistHotkey != "" {
		t.Errorf("AssistHotkey = %q, want empty", form.AssistHotkey)
	}
	if form.VoiceAgentHotkey != "ctrl+shift+k" {
		t.Errorf("VoiceAgentHotkey = %q, want %q", form.VoiceAgentHotkey, "ctrl+shift+k")
	}
}

func TestParseSettingsFormRejectsDuplicateModeBases(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"dictate_hotkey":     {"win+alt"},
		"assist_hotkey":      {"win+alt+j"},
		"voice_agent_hotkey": {"ctrl+shift"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
		"overlay_position":   {"top"},
		"store_backend":      {"sqlite"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	_, errMsg := parseSettingsForm(req, cfg)
	if errMsg == "" {
		t.Fatal("expected duplicate hotkey error")
	}
}

func TestParseSettingsFormRejectsUnsupportedModeHotkeyPattern(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.UI.OverlayPosition = "top"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"dictate_hotkey":     {"alt+space"},
		"assist_hotkey":      {"ctrl+win"},
		"voice_agent_hotkey": {"ctrl+shift"},
		"overlay_visualizer": {"pill"},
		"overlay_design":     {"default"},
		"overlay_position":   {"top"},
		"store_backend":      {"sqlite"},
	}

	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	_, errMsg := parseSettingsForm(req, cfg)
	if errMsg == "" {
		t.Fatal("expected unsupported mode hotkey error")
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

func TestParseSettingsFormActiveModeDefaultsToNone(t *testing.T) {
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
	if form.ActiveMode != "none" {
		t.Errorf("ActiveMode = %q, want %q", form.ActiveMode, "none")
	}
}

func TestParseSettingsFormAgentModeDefaultsToAssist(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.General.AgentMode = "voice_agent"
	cfg.UI.Visualizer = "pill"
	cfg.UI.Design = "default"
	cfg.Store.Backend = "sqlite"

	formValues := url.Values{
		"agent_mode": {"invalid"},
	}
	req, _ := http.NewRequest(http.MethodPost, "/settings/update", strings.NewReader(formValues.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.ParseForm()

	form, errMsg := parseSettingsForm(req, cfg)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if form.AgentMode != "assist" {
		t.Errorf("AgentMode = %q, want %q", form.AgentMode, "assist")
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
	cfg.VoiceAgent.FrameworkPrompt = "Framework prompt"
	cfg.VoiceAgent.Instruction = "Framework prompt"

	form := settingsFormData{
		DictateHotkey:              "ctrl+shift+d",
		AssistHotkey:               "ctrl+win+j",
		VoiceAgentHotkey:           "win+alt+k",
		AgentHotkey:                "ctrl+win+j",
		AgentMode:                  "voice_agent",
		ActiveMode:                 "none",
		DictateEnabled:             true,
		AssistEnabled:              true,
		VoiceAgentEnabled:          false,
		VoiceAgentHotkeyBehavior:   config.HotkeyBehaviorToggle,
		VoiceAgentCloseBehavior:    config.VoiceAgentCloseBehaviorNewChat,
		VoiceAgentRefinementPrompt: "Refinement prompt",
		VoiceAgentSessionSummary:   false,
		AutoStartOnLaunch:          true,
		AudioDeviceID:              "dev-1",
		HFModel:                    "whisper-large",
		OverlayEnabled:             true,
		Visualizer:                 "pill",
		Design:                     "default",
		OverlayPosition:            "bottom",
		OverlayMovable:             true,
		OverlayFreeX:               50,
		OverlayFreeY:               60,
		StoreBackend:               "sqlite",
		StoreSQLitePath:            "/tmp/new.db",
		StoreSaveAudio:             true,
		StoreAudioRetention:        30,
		StoreMaxAudioStorage:       500,
		VocabularyDictionary:       "hello=hi",
	}

	result := buildNextConfig(form, cfg)

	if result.General.Hotkey != "ctrl+shift+d" {
		t.Errorf("Hotkey = %q, want %q", result.General.Hotkey, "ctrl+shift+d")
	}
	if result.General.DictateHotkey != "ctrl+shift+d" {
		t.Errorf("DictateHotkey = %q", result.General.DictateHotkey)
	}
	if result.General.AssistHotkey != "ctrl+win+j" {
		t.Errorf("AssistHotkey = %q", result.General.AssistHotkey)
	}
	if result.General.VoiceAgentHotkey != "win+alt+k" {
		t.Errorf("VoiceAgentHotkey = %q", result.General.VoiceAgentHotkey)
	}
	if result.General.VoiceAgentHotkeyBehavior != config.HotkeyBehaviorToggle {
		t.Errorf("VoiceAgentHotkeyBehavior = %q", result.General.VoiceAgentHotkeyBehavior)
	}
	if result.General.AgentHotkey != "ctrl+win+j" {
		t.Errorf("AgentHotkey = %q", result.General.AgentHotkey)
	}
	if result.General.AgentMode != "assist" {
		t.Errorf("AgentMode = %q", result.General.AgentMode)
	}
	if result.General.ActiveMode != "none" {
		t.Errorf("ActiveMode = %q", result.General.ActiveMode)
	}
	if !result.General.DictateEnabled || !result.General.AssistEnabled || result.General.VoiceAgentEnabled {
		t.Fatalf("unexpected mode enabled flags: %+v", result.General)
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
	if result.VoiceAgent.CloseBehavior != config.VoiceAgentCloseBehaviorNewChat {
		t.Errorf("VoiceAgent.CloseBehavior = %q", result.VoiceAgent.CloseBehavior)
	}
	if result.VoiceAgent.FrameworkPrompt != "Framework prompt" {
		t.Errorf("VoiceAgent.FrameworkPrompt = %q", result.VoiceAgent.FrameworkPrompt)
	}
	if result.VoiceAgent.RefinementPrompt != "Refinement prompt" {
		t.Errorf("VoiceAgent.RefinementPrompt = %q", result.VoiceAgent.RefinementPrompt)
	}
	if result.VoiceAgent.EnableSessionSummary {
		t.Error("VoiceAgent.EnableSessionSummary = true, want false")
	}
	if result.VoiceAgent.Instruction != "Framework prompt" {
		t.Errorf("VoiceAgent.Instruction = %q", result.VoiceAgent.Instruction)
	}
	if !result.General.AutoStartOnLaunch {
		t.Error("General.AutoStartOnLaunch = false, want true")
	}
	if !result.VoiceAgent.AutoStartOnLaunch {
		t.Error("VoiceAgent.AutoStartOnLaunch = false, want true")
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
		DictateHotkey:     "win+alt",
		AssistHotkey:      "ctrl+win",
		VoiceAgentHotkey:  "ctrl+shift",
		AgentHotkey:       "ctrl+win",
		AgentMode:         "assist",
		ActiveMode:        "none",
		DictateEnabled:    true,
		AssistEnabled:     true,
		VoiceAgentEnabled: true,
		Visualizer:        "pill",
		Design:            "default",
		OverlayPosition:   "top",
		StoreBackend:      "postgres",
		StorePostgresDSN:  "postgres://localhost/test",
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
