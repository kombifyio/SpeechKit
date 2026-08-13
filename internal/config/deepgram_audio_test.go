package config

import "testing"

func TestSplitDeepgramAudioModelID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		model      string
		wantListen string
		wantSpeak  string
	}{
		{"catalog composite", "flux-general-multi+aura-2", "flux-general-multi", ""},
		{"legacy nova composite", "nova-3+aura-2", "nova-3", ""},
		{"composite with a full voice", "flux-general-en+flux-kit-en", "flux-general-en", "flux-kit-en"},
		{"composite with an aura voice", "nova-3+aura-2-thalia-en", "nova-3", "aura-2-thalia-en"},
		{"bare listen model", "flux-general-en", "flux-general-en", ""},
		{"think model", "gpt-4o-mini", "", ""},
		{"gemini model", "gemini-3.1-flash-live-preview", "", ""},
		{"empty", "", "", ""},
	}
	for _, tc := range cases {
		listen, speak := splitDeepgramAudioModelID(tc.model)
		if listen != tc.wantListen || speak != tc.wantSpeak {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.name, listen, speak, tc.wantListen, tc.wantSpeak)
		}
	}
}

func TestDeepgramAudioConfig(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	cfg.VoiceAgent.Model = "flux-general-multi+aura-2"
	cfg.VoiceAgent.DeepgramListenEOTThreshold = 0.8
	cfg.VoiceAgent.DeepgramListenEagerEOTThreshold = 0.45
	cfg.VoiceAgent.DeepgramListenEOTTimeoutMs = 4000

	got := cfg.DeepgramAudioConfig()
	if got.ListenModel != "flux-general-multi" {
		t.Errorf("composite should supply the listen leg, got %q", got.ListenModel)
	}
	if got.SpeakModel != "" {
		t.Errorf("the aura-2 family name is not a voice id, got %q", got.SpeakModel)
	}
	if got.EOTThreshold != 0.8 || got.EagerEOTThreshold != 0.45 || got.EOTTimeoutMs != 4000 {
		t.Errorf("turn-detection tuning not carried through: %+v", got)
	}

	// Explicit fields win over the composite.
	cfg.VoiceAgent.DeepgramListenModel = "flux-general-en"
	cfg.VoiceAgent.DeepgramSpeakModel = "flux-kit-en"
	cfg.VoiceAgent.DeepgramSpeakSpeed = 1.05
	got = cfg.DeepgramAudioConfig()
	if got.ListenModel != "flux-general-en" || got.SpeakModel != "flux-kit-en" || got.SpeakSpeed != 1.05 {
		t.Errorf("explicit fields should win: %+v", got)
	}
}

// A Deepgram audio model id in [voice_agent].model must never leak into the
// think leg — the Flux ids are the newest members of that class.
func TestDeepgramThinkConfigIgnoresFluxAudioModels(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"flux-general-multi", "flux-general-multi+aura-2", "flux-kit-en", "nova-3"} {
		cfg := &Config{}
		cfg.VoiceAgent.Model = model
		if got := cfg.DeepgramThinkConfig().Model; got != "" {
			t.Errorf("%q leaked into the think leg as %q", model, got)
		}
	}
	cfg := &Config{}
	cfg.VoiceAgent.Model = "gpt-4o-mini"
	if got := cfg.DeepgramThinkConfig().Model; got != "gpt-4o-mini" {
		t.Errorf("a think model must still be reused, got %q", got)
	}
}
