package config

import "testing"

func TestNormalizeOverlayActionsEmptyMeansDefault(t *testing.T) {
	got := NormalizeOverlayActions(nil)
	want := DefaultOverlayActions()
	if len(got) != len(want) {
		t.Fatalf("default actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default actions = %v, want %v", got, want)
		}
	}
}

func TestNormalizeOverlayActionsDropsUnknown(t *testing.T) {
	got := NormalizeOverlayActions([]string{"copy", "nope", "COPY", "language"})
	if len(got) != 2 || got[0] != OverlayActionCopy || got[1] != OverlayActionLanguage {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeOverlayActionsNoneMeansEmpty(t *testing.T) {
	got := NormalizeOverlayActions([]string{OverlayActionNone})
	if len(got) != 0 {
		t.Fatalf("none sentinel = %v, want an empty strip", got)
	}
}

func TestNormalizeOverlayActionsEmptySliceMeansEmpty(t *testing.T) {
	got := NormalizeOverlayActions([]string{})
	if len(got) != 0 {
		t.Fatalf("empty slice = %v, want an empty strip", got)
	}
}

func TestNormalizeOverlayActionsLegacyDefaultKeepsMic(t *testing.T) {
	got := NormalizeOverlayActions([]string{"copy", "note", "language", "meeting"})
	if len(got) == 0 || got[0] != OverlayActionMic {
		t.Fatalf("legacy default = %v, want mic kept on", got)
	}
}

func TestToggleDictationProcessingMode(t *testing.T) {
	if got := ToggleDictationProcessingMode(DictationProcessingModeAuto); got != DictationProcessingModeFinalFull {
		t.Fatalf("live → %q, want full", got)
	}
	if got := ToggleDictationProcessingMode(DictationProcessingModeFinalFull); got != DictationProcessingModeAuto {
		t.Fatalf("full → %q, want live", got)
	}
	if !DictationModeIsLive(DictationProcessingModeProviderStream) {
		t.Fatal("provider stream should count as live")
	}
}

func TestNextDictateSTTProfileCyclesFlagshipEngines(t *testing.T) {
	primary, fallback := NextDictateSTTProfile(DictateDeepgramProfileID)
	if primary != DictateAssemblyAIProfileID || fallback != DictateDeepgramProfileID {
		t.Fatalf("deepgram → %q / %q", primary, fallback)
	}
	primary, fallback = NextDictateSTTProfile(DictateAssemblyAIProfileID)
	if primary != DictateDeepgramProfileID || fallback != DictateAssemblyAIProfileID {
		t.Fatalf("assemblyai → %q / %q", primary, fallback)
	}
}

func TestPersistOverlayActionsWritesNone(t *testing.T) {
	got := PersistOverlayActions([]string{OverlayActionNone})
	if len(got) != 1 || got[0] != OverlayActionNone {
		t.Fatalf("persist empty = %v, want [%s]", got, OverlayActionNone)
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
