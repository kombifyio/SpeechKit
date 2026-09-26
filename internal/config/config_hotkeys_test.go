package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHotkeyBehaviors(t *testing.T) {
	cfg := defaults()
	if cfg.General.HotkeyMode != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default HotkeyMode = %q, want %q", cfg.General.HotkeyMode, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.DictateHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default DictateHotkeyBehavior = %q, want %q", cfg.General.DictateHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.AssistHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default AssistHotkeyBehavior = %q, want %q", cfg.General.AssistHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("default VoiceAgentHotkeyBehavior = %q, want %q", cfg.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.VoiceAgent.CloseBehavior != VoiceAgentCloseBehaviorContinue {
		t.Fatalf("default VoiceAgent.CloseBehavior = %q, want %q", cfg.VoiceAgent.CloseBehavior, VoiceAgentCloseBehaviorContinue)
	}
}

func TestNormalizeDictationProcessingMode(t *testing.T) {
	tests := []struct {
		in       string
		fallback string
		want     string
	}{
		{in: "", want: DictationProcessingModeFinalFull},
		{in: "final_full", want: DictationProcessingModeFinalFull},
		{in: "SEGMENT_BATCH", want: DictationProcessingModeSegmentBatch},
		{in: " provider_stream ", want: DictationProcessingModeProviderStream},
		{in: "auto", want: DictationProcessingModeAuto},
		{in: "bad", fallback: DictationProcessingModeSegmentBatch, want: DictationProcessingModeSegmentBatch},
		{in: "bad", fallback: "bad", want: DictationProcessingModeFinalFull},
	}
	for _, tt := range tests {
		if got := NormalizeDictationProcessingMode(tt.in, tt.fallback); got != tt.want {
			t.Fatalf("NormalizeDictationProcessingMode(%q, %q) = %q, want %q", tt.in, tt.fallback, got, tt.want)
		}
	}
}

func TestNormalizeAudioInputSource(t *testing.T) {
	tests := []struct {
		in       string
		fallback string
		want     string
	}{
		{in: "", want: AudioInputSourceMicrophone},
		{in: "microphone", want: AudioInputSourceMicrophone},
		{in: "system", want: AudioInputSourceSystemLoopback},
		{in: "loopback", want: AudioInputSourceSystemLoopback},
		{in: "MIC+SYSTEM", want: AudioInputSourceMicAndSystem},
		{in: "bad", fallback: AudioInputSourceSystemLoopback, want: AudioInputSourceSystemLoopback},
		{in: "bad", fallback: "bad", want: AudioInputSourceMicrophone},
	}
	for _, tt := range tests {
		if got := NormalizeAudioInputSource(tt.in, tt.fallback); got != tt.want {
			t.Fatalf("NormalizeAudioInputSource(%q, %q) = %q, want %q", tt.in, tt.fallback, got, tt.want)
		}
	}
}

func TestDefaultOverlayPosition(t *testing.T) {
	cfg := defaults()
	if cfg.UI.OverlayPosition != "bottom" {
		t.Fatalf("default OverlayPosition = %q, want %q", cfg.UI.OverlayPosition, "bottom")
	}
	if cfg.UI.OverlayMovable {
		t.Fatal("default OverlayMovable = true, want false")
	}
	if cfg.UI.OverlayFreeX != 0 || cfg.UI.OverlayFreeY != 0 {
		t.Fatalf("default free overlay coordinates = (%d,%d), want (0,0)", cfg.UI.OverlayFreeX, cfg.UI.OverlayFreeY)
	}
}

func TestDefaultStoreAudioSettings(t *testing.T) {
	cfg := defaults()
	if cfg.General.Hotkey != "ctrl+win" {
		t.Fatalf("default Hotkey = %q, want %q", cfg.General.Hotkey, "ctrl+win")
	}
	if cfg.General.DictateHotkey != "ctrl+win" {
		t.Fatalf("default DictateHotkey = %q, want %q", cfg.General.DictateHotkey, "ctrl+win")
	}
	if cfg.General.AssistHotkey != "win+alt" {
		t.Fatalf("default AssistHotkey = %q, want %q", cfg.General.AssistHotkey, "win+alt")
	}
	if cfg.General.AgentHotkey != "win+alt" {
		t.Fatalf("default AgentHotkey = %q, want %q", cfg.General.AgentHotkey, "win+alt")
	}
	if !cfg.Store.SaveAudio {
		t.Fatal("default Store.SaveAudio = false, want true")
	}
	if cfg.Store.AudioRetentionDays != 7 {
		t.Fatalf("default Store.AudioRetentionDays = %d, want %d", cfg.Store.AudioRetentionDays, 7)
	}
}

func TestSaveRoundTripNewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := defaults()
	cfg.General.HotkeyMode = HotkeyBehaviorToggle
	cfg.General.DictateHotkeyBehavior = HotkeyBehaviorToggle
	cfg.General.AssistHotkeyBehavior = HotkeyBehaviorHoldToTalk
	cfg.General.VoiceAgentHotkeyBehavior = HotkeyBehaviorToggle
	cfg.General.DictationProcessingMode = DictationProcessingModeSegmentBatch
	cfg.Audio.InputSource = AudioInputSourceSystemLoopback
	cfg.UI.OverlayPosition = "bottom"
	cfg.UI.OverlayMovable = true
	cfg.UI.OverlayFreeX = 864
	cfg.UI.OverlayFreeY = 512

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.General.HotkeyMode != HotkeyBehaviorToggle {
		t.Fatalf("HotkeyMode = %q, want %q", reloaded.General.HotkeyMode, HotkeyBehaviorToggle)
	}
	if reloaded.General.DictateHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("DictateHotkeyBehavior = %q, want %q", reloaded.General.DictateHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if reloaded.General.AssistHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("AssistHotkeyBehavior = %q, want %q", reloaded.General.AssistHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if reloaded.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("VoiceAgentHotkeyBehavior = %q, want %q", reloaded.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if reloaded.General.DictationProcessingMode != DictationProcessingModeSegmentBatch {
		t.Fatalf("DictationProcessingMode = %q, want %q", reloaded.General.DictationProcessingMode, DictationProcessingModeSegmentBatch)
	}
	if reloaded.Audio.InputSource != AudioInputSourceSystemLoopback {
		t.Fatalf("Audio.InputSource = %q, want %q", reloaded.Audio.InputSource, AudioInputSourceSystemLoopback)
	}
	if reloaded.UI.OverlayPosition != "bottom" {
		t.Fatalf("OverlayPosition = %q, want %q", reloaded.UI.OverlayPosition, "bottom")
	}
	if !reloaded.UI.OverlayMovable {
		t.Fatal("OverlayMovable = false, want true")
	}
	if reloaded.UI.OverlayFreeX != 864 || reloaded.UI.OverlayFreeY != 512 {
		t.Fatalf("free overlay coordinates = (%d,%d), want (864,512)", reloaded.UI.OverlayFreeX, reloaded.UI.OverlayFreeY)
	}
}

func TestLoadPreservesUnsetNewFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	// Write a config file that does NOT contain hotkey_mode or overlay_position.
	content := `[general]
language = "en"
hotkey = "ctrl+shift"
auto_stop_silence_ms = 300

[ui]
overlay_enabled = true
visualizer = "pill"
design = "default"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Fields absent from file should retain defaults.
	if cfg.General.HotkeyMode != HotkeyBehaviorHoldToTalk {
		t.Fatalf("HotkeyMode = %q, want default %q", cfg.General.HotkeyMode, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.DictateHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("DictateHotkeyBehavior = %q, want default %q", cfg.General.DictateHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.AssistHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("AssistHotkeyBehavior = %q, want default %q", cfg.General.AssistHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorHoldToTalk {
		t.Fatalf("VoiceAgentHotkeyBehavior = %q, want default %q", cfg.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorHoldToTalk)
	}
	if cfg.VoiceAgent.CloseBehavior != VoiceAgentCloseBehaviorContinue {
		t.Fatalf("VoiceAgent.CloseBehavior = %q, want default %q", cfg.VoiceAgent.CloseBehavior, VoiceAgentCloseBehaviorContinue)
	}
	if cfg.General.DictationProcessingMode != DictationProcessingModeAuto {
		t.Fatalf("DictationProcessingMode = %q, want default %q", cfg.General.DictationProcessingMode, DictationProcessingModeAuto)
	}
	if cfg.Audio.InputSource != AudioInputSourceMicrophone {
		t.Fatalf("Audio.InputSource = %q, want default %q", cfg.Audio.InputSource, AudioInputSourceMicrophone)
	}
	if cfg.UI.OverlayPosition != "bottom" {
		t.Fatalf("OverlayPosition = %q, want default %q", cfg.UI.OverlayPosition, "bottom")
	}
}

func TestLoadBackfillsLegacyHotkeyModeIntoPerModeBehaviors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[general]
hotkey_mode = "toggle"
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.General.DictateHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("DictateHotkeyBehavior = %q, want %q", cfg.General.DictateHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if cfg.General.AssistHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("AssistHotkeyBehavior = %q, want %q", cfg.General.AssistHotkeyBehavior, HotkeyBehaviorToggle)
	}
	if cfg.General.VoiceAgentHotkeyBehavior != HotkeyBehaviorToggle {
		t.Fatalf("VoiceAgentHotkeyBehavior = %q, want %q", cfg.General.VoiceAgentHotkeyBehavior, HotkeyBehaviorToggle)
	}
}

func TestLoadMigratesOldBuiltInHotkeyDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[general]
hotkey = "win+alt"
dictate_hotkey = "win+alt"
assist_hotkey = "ctrl+win"
voice_agent_hotkey = "ctrl+shift"
agent_hotkey = "ctrl+win"
agent_mode = "assist"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.General.Hotkey, "ctrl+win"; got != want {
		t.Fatalf("legacy hotkey alias = %q, want %q", got, want)
	}
	if got, want := cfg.General.DictateHotkey, "ctrl+win"; got != want {
		t.Fatalf("dictate hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AssistHotkey, "win+alt"; got != want {
		t.Fatalf("assist hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AgentHotkey, "win+alt"; got != want {
		t.Fatalf("agent hotkey alias = %q, want %q", got, want)
	}
}

func TestLoadPreservesCustomHotkeyPair(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `[general]
hotkey = "ctrl+shift+d"
dictate_hotkey = "ctrl+shift+d"
assist_hotkey = "ctrl+win+j"
voice_agent_hotkey = "win+alt+k"
agent_hotkey = "ctrl+win+j"
agent_mode = "assist"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := cfg.General.DictateHotkey, "ctrl+shift+d"; got != want {
		t.Fatalf("dictate hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.AssistHotkey, "ctrl+win+j"; got != want {
		t.Fatalf("assist hotkey = %q, want %q", got, want)
	}
	if got, want := cfg.General.VoiceAgentHotkey, "win+alt+k"; got != want {
		t.Fatalf("voice agent hotkey = %q, want %q", got, want)
	}
}
