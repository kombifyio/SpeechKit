package stt

import "testing"

// Mirrors the Kotlin gate at
// android/core/src/test/kotlin/io/kombify/speechkit/stt/MultilanguageContractTest.kt.
//
// SpeechKit does not narrow speech to one language. A pinned language breaks
// switching language mid-conversation and speaking different languages with
// different people in one session, and it is a silent data-loss bug: the same
// English audio returns a zero-length transcript when pinned to German, with
// HTTP 200 and no error. This has regressed repeatedly, so it is pinned by
// tests rather than by intent.
//
// Both directions are covered: an unset language must resolve to
// multilanguage, and a deliberate user pin must still survive — a gate that
// only forbade the value would break the supported override.

func TestIsMultilanguageAcceptsEveryUnpinnedSpelling(t *testing.T) {
	// The settings UI offers "auto" (Auto / provider detect) and "multi"
	// (Multilingual) as separate labels, so both have to resolve here.
	for _, language := range []string{"", "   ", "auto", "AUTO", "multi", "MULTI", " Multi "} {
		if !IsMultilanguage(language) {
			t.Errorf("IsMultilanguage(%q) = false, want true", language)
		}
	}
}

func TestIsMultilanguageRejectsALocale(t *testing.T) {
	for _, language := range []string{"de", "en", "de-DE", "pt-BR"} {
		if IsMultilanguage(language) {
			t.Errorf("IsMultilanguage(%q) = true, want false", language)
		}
	}
}

// The regression this fixes: "multi" was passed through verbatim because only
// "" and "auto" were normalized away. That is correct for Deepgram, whose API
// takes the literal token, and invalid for every provider that expects a
// language code or nothing at all — the same defect shape as the Android
// platform recognizer, where the literal token became an invalid BCP-47 tag.
func TestNormalizedRequestLanguageDoesNotForwardTheSentinel(t *testing.T) {
	for _, language := range []string{"", "auto", "multi"} {
		if got := normalizedRequestLanguage(language, false); got != "" {
			t.Errorf("normalizedRequestLanguage(%q) = %q, want \"\" so each adapter can express multilanguage natively", language, got)
		}
	}
}

func TestNormalizedRequestLanguageKeepsAnExplicitPin(t *testing.T) {
	if got := normalizedRequestLanguage("de", false); got != "de" {
		t.Errorf("normalizedRequestLanguage(\"de\") = %q, want \"de\" — an explicit pin is a supported override", got)
	}
	if got := normalizedRequestLanguage("pt-BR", false); got != "pt-BR" {
		t.Errorf("normalizedRequestLanguage(\"pt-BR\") = %q, want \"pt-BR\"", got)
	}
}

func TestDetectLanguageStillWins(t *testing.T) {
	if got := normalizedRequestLanguage("de", true); got != "" {
		t.Errorf("normalizedRequestLanguage(\"de\", detect=true) = %q, want \"\"", got)
	}
}
