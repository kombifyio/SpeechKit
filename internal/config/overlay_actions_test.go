package config

import "testing"

func TestNormalizeOverlayActionsEmptyMeansDefault(t *testing.T) {
	got := NormalizeOverlayActions(nil)
	if len(got) != 4 {
		t.Fatalf("default actions = %v, want the four shipped actions", got)
	}
}

func TestNormalizeOverlayActionsDropsUnknown(t *testing.T) {
	got := NormalizeOverlayActions([]string{"copy", "nope", "COPY", "language"})
	if len(got) != 2 || got[0] != OverlayActionCopy || got[1] != OverlayActionLanguage {
		t.Fatalf("got %v", got)
	}
}

func TestNextSpeechLanguageRotates(t *testing.T) {
	if got := NextSpeechLanguage("multi", "de"); got != "en" {
		t.Fatalf("multi → %q, want en", got)
	}
	if got := NextSpeechLanguage("en", "de"); got != "de" {
		t.Fatalf("en → %q, want de", got)
	}
	if got := NextSpeechLanguage("de", "de"); got != "multi" {
		t.Fatalf("de → %q, want multi", got)
	}
	if got := NextSpeechLanguage("en", "en"); got != "multi" {
		t.Fatalf("en/en → %q, want multi", got)
	}
}
