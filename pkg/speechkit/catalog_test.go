package speechkit

import "testing"

func TestNormalizeMode_TTSAliases(t *testing.T) {
	for _, alias := range []string{"tts", "voice_output", "speak", "speech"} {
		if got := NormalizeMode(Mode(alias)); got != ModeTTS {
			t.Errorf("NormalizeMode(%q) = %q, want ModeTTS", alias, got)
		}
	}
}
