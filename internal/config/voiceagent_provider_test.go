package config

import "testing"

// The catalog readiness surface marks the voice_agent profile Active from
// this derivation while session serving reads the same value — both sides of
// the kombify-SpeechKit-5nt5 contract hang off these two functions.
func TestEffectiveVoiceAgentProviderMatchesServingDefault(t *testing.T) {
	if got := EffectiveVoiceAgentProvider(nil); got != "gemini" {
		t.Errorf("nil config = %q, want gemini", got)
	}
	vanilla := &Config{}
	if got := EffectiveVoiceAgentProvider(vanilla); got != "gemini" {
		t.Errorf("vanilla config = %q, want gemini (empty provider serves Gemini Live)", got)
	}
	aliased := &Config{}
	aliased.VoiceAgent.Provider = "realtime.deepgram.voice-agent"
	if got := EffectiveVoiceAgentProvider(aliased); got != "deepgram" {
		t.Errorf("profile-id alias = %q, want deepgram", got)
	}
}

func TestEffectiveVoiceAgentProfileIDFollowsProvider(t *testing.T) {
	cases := []struct {
		provider string
		want     string
	}{
		{"", "realtime.google.gemini-native-audio"},
		{"deepgram", "realtime.deepgram.voice-agent"},
		{"openai", "realtime.openai.gpt-realtime-2"},
		{"assemblyai", "realtime.assemblyai.voice-agent"},
		{"local-cascaded", "realtime.builtin.pipeline"},
		// No catalog profile for the moshi stub: no profile shows Active.
		{"moshi", ""},
	}
	for _, tc := range cases {
		cfg := &Config{}
		cfg.VoiceAgent.Provider = tc.provider
		if got := EffectiveVoiceAgentProfileID(cfg); got != tc.want {
			t.Errorf("provider %q profile = %q, want %q", tc.provider, got, tc.want)
		}
	}
}
