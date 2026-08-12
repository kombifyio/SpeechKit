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

// The response direction, which the request-side gates above left open.
//
// Providers that cannot report a detected language used to fall back to a
// hardcoded "de" label while Deepgram and Google fell back to the sentinel.
// That inconsistency was not cosmetic: consumers pick a customization
// dictionary from Transcript.Language, so an unpinned huggingface or local
// session had German vocabulary applied to speech in any language.
//
// The rule is the same in both directions. A label is either something the
// provider actually reported, or the value that was explicitly asked for. It
// is never a locale nobody chose.
func TestReportedLanguageNeverInventsALocale(t *testing.T) {
	for _, tc := range []struct {
		name     string
		detected string
		asked    string
		want     string
	}{
		{name: "detected language wins", detected: "pt-BR", asked: "", want: "pt-BR"},
		{name: "detected wins over a pin", detected: "en", asked: "de", want: "en"},
		{name: "pin survives when nothing detected", detected: "", asked: "de", want: "de"},
		{name: "unpinned falls back to the sentinel", detected: "", asked: "", want: LanguageMulti},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstNonEmptyTrimmed(tc.detected, tc.asked, LanguageMulti); got != tc.want {
				t.Errorf("reported language = %q, want %q", got, tc.want)
			}
		})
	}
}

// APILanguage is what the adapters hand to firstNonEmptyTrimmed as the "asked
// for" value, so it must not leak the sentinel into a label either — reporting
// the literal "auto" would be as wrong as reporting "de".
func TestAPILanguageResolvesTheSentinelToEmpty(t *testing.T) {
	for _, language := range []string{"", "auto", "multi"} {
		resolved := ResolvedTranscribeOptions{Language: language}
		if got := resolved.APILanguage(); got != "" {
			t.Errorf("APILanguage(%q) = %q, want \"\" so the label falls through to the sentinel", language, got)
		}
	}
	resolved := ResolvedTranscribeOptions{Language: "de"}
	if got := resolved.APILanguage(); got != "de" {
		t.Errorf("APILanguage(\"de\") = %q, want \"de\"", got)
	}
}
