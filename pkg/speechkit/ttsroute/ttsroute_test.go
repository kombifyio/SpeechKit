package ttsroute

import "testing"

func TestPreferredProvider(t *testing.T) {
	cases := []struct {
		profileID string
		want      string
	}{
		{"", ""},
		{"  ", ""},
		{"unknown.profile", ""},
		{"tts.openai.nova", "openai"},
		{"tts.google.studio-o", "google"},
		{"tts.huggingface.parler", "huggingface"},
		{"tts.openedai.kokoro-v1", "kokoro"},
		{"tts.kokoro.default", "kokoro"},
		{"tts.local.kokoro-int8", "kokoro_local"},
		{"tts.local.supertonic-ml", "supertonic_local"},
		{"tts.local.chatterbox-clone", "chatterbox_local"},
		{"tts.local.piper", "piper"},
		{"tts.local.piper-de", "piper"},
		// Surrounding whitespace is trimmed before matching.
		{"  tts.openai.nova  ", "openai"},
	}
	for _, tc := range cases {
		if got := PreferredProvider(tc.profileID); got != tc.want {
			t.Errorf("PreferredProvider(%q) = %q, want %q", tc.profileID, got, tc.want)
		}
	}
}
