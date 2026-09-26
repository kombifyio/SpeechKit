package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvedModeSource(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to local", in: "", want: ModeSourceLocal},
		{name: "explicit local", in: "local", want: ModeSourceLocal},
		{name: "explicit server", in: "server", want: ModeSourceServer},
		{name: "uppercase server", in: "SERVER", want: ModeSourceServer},
		{name: "mixed case server", in: "Server", want: ModeSourceServer},
		{name: "padded server", in: "  server  ", want: ModeSourceServer},
		{name: "garbage falls back to local", in: "remote", want: ModeSourceLocal},
		{name: "uppercase local", in: "LOCAL", want: ModeSourceLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := ModeModelSelection{ModeSource: tt.in}
			if got := sel.ResolvedModeSource(); got != tt.want {
				t.Fatalf("ResolvedModeSource(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeServerConnectionAuthMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ServerConnectionAuthModeBearer},
		{in: "bearer", want: ServerConnectionAuthModeBearer},
		{in: "api_key", want: ServerConnectionAuthModeAPIKey},
		{in: "API_KEY", want: ServerConnectionAuthModeAPIKey},
		{in: "edge_beta", want: ServerConnectionAuthModeEdgeBeta},
		{in: "EDGE_BETA", want: ServerConnectionAuthModeEdgeBeta},
		{in: "unknown", want: ServerConnectionAuthModeBearer},
	}
	for _, tt := range tests {
		if got := NormalizeServerConnectionAuthMode(tt.in); got != tt.want {
			t.Fatalf("NormalizeServerConnectionAuthMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeOverlayFeedbackMode(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{name: "big", value: OverlayFeedbackModeBigProductivity, fallback: OverlayFeedbackModeSmallFeedback, want: OverlayFeedbackModeBigProductivity},
		{name: "small", value: OverlayFeedbackModeSmallFeedback, fallback: OverlayFeedbackModeBigProductivity, want: OverlayFeedbackModeSmallFeedback},
		{name: "fallback", value: "unknown", fallback: OverlayFeedbackModeBigProductivity, want: OverlayFeedbackModeBigProductivity},
		{name: "empty fallback", value: "unknown", fallback: "", want: OverlayFeedbackModeSmallFeedback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeOverlayFeedbackMode(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("NormalizeOverlayFeedbackMode(%q, %q) = %q, want %q", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestNormalizeWakewordBackend(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to sherpa_kws (bundle-consistent default)", in: "", want: WakewordBackendSherpaKWS},
		{name: "sherpa explicit pin survives", in: "sherpa_kws", want: WakewordBackendSherpaKWS},
		{name: "livekit explicit pin survives", in: "livekit", want: WakewordBackendLiveKitOpenWakeWord},
		{name: "openwakeword alias survives", in: "openWakeWord", want: WakewordBackendLiveKitOpenWakeWord},
		{name: "stt alias", in: "phrase_match", want: WakewordBackendSTTPhrase},
		{name: "unknown falls back to sherpa_kws (bundle-consistent default)", in: "unknown", want: WakewordBackendSherpaKWS},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeWakewordBackend(tt.in); got != tt.want {
				t.Fatalf("NormalizeWakewordBackend(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoadHandsFreeBlockMirrorsWakewordCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[hands_free]
enabled = true
activation_phrase_id = "hey_mira"
target_mode = "assist"
auto_end_silence_cutoff_sec = 7
voice_output_enabled = true

[wakeword]
enabled = false
phrase_id = "hey_quby"
default_mode = "voice_agent"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.HandsFree.Enabled || !cfg.Wakeword.Enabled {
		t.Fatal("hands-free enabled should mirror to wakeword enabled")
	}
	if got, want := cfg.HandsFree.ActivationPhraseID, "hey_mira"; got != want {
		t.Fatalf("hands-free phrase = %q, want %q", got, want)
	}
	if got, want := cfg.Wakeword.PhraseID, "hey_mira"; got != want {
		t.Fatalf("wakeword phrase = %q, want %q", got, want)
	}
	if got, want := cfg.HandsFree.TargetMode, HandsFreeTargetAssist; got != want {
		t.Fatalf("hands-free target = %q, want %q", got, want)
	}
	if got, want := cfg.Wakeword.DefaultMode, WakewordDefaultModeAssist; got != want {
		t.Fatalf("wakeword default mode = %q, want %q", got, want)
	}
	if got, want := cfg.Wakeword.AutoEnd.SilenceCutoffSec, 7; got != want {
		t.Fatalf("wakeword auto-end = %d, want %d", got, want)
	}
}

func TestLoadLegacyWakewordDerivesHandsFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `
[wakeword]
enabled = true
phrase_id = "hey_kombify"
default_mode = "dictate"

[wakeword.auto_end]
silence_cutoff_sec = 4
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.HandsFree.Enabled {
		t.Fatal("hands-free enabled should be derived from legacy wakeword")
	}
	if got, want := cfg.HandsFree.ActivationPhraseID, "hey_kombify"; got != want {
		t.Fatalf("hands-free phrase = %q, want %q", got, want)
	}
	if got, want := cfg.HandsFree.TargetMode, HandsFreeTargetDictationUIAssisted; got != want {
		t.Fatalf("hands-free target = %q, want %q", got, want)
	}
	if cfg.HandsFree.VoiceOutputEnabled {
		t.Fatal("dictation UI-assisted target must not enable hands-free voice output")
	}
	if got, want := cfg.HandsFree.AutoEndSilenceCutoffSec, 4; got != want {
		t.Fatalf("hands-free auto-end = %d, want %d", got, want)
	}
}
