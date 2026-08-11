package config

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// The product defaults must not pin a locale.
//
// This is where the live defect sat: SpeechDefaultsValues feeds
// OptionLanguage in as a GLOBAL default, and ResolvedTranscribeOptions.
// APILanguage forwards global defaults to the wire (unlike provider defaults,
// which it suppresses). So a "de" here was actually sent to every provider,
// and the Device target pinned German for every user who never touched the
// setting.

func TestDefaultsDoNotPinALocale(t *testing.T) {
	cfg := defaults()
	for name, value := range map[string]string{
		"General.Language": cfg.General.Language,
		"Speech.Language":  cfg.Speech.Language,
	} {
		if isLocaleLike(value) {
			t.Errorf("%s = %q, want a multilanguage value — a default locale is silently sent to every provider", name, value)
		}
	}
}

func TestSpeechDefaultsValuesCarryMultilanguage(t *testing.T) {
	values := SpeechDefaultsValues(defaults())
	language, _ := values[provideropts.OptionLanguage].(string)
	if isLocaleLike(language) {
		t.Fatalf("SpeechDefaultsValues language = %q, want a multilanguage value", language)
	}
}

// A user who deliberately pins a language must still get it: the rule forbids
// inferring a locale, not choosing one.
func TestAnExplicitLanguageSurvivesNormalization(t *testing.T) {
	cfg := defaults()
	cfg.Speech.Language = "pt-BR"
	cfg.General.Language = "pt-BR"
	normalizeSpeechDefaults(cfg, true, true, true)
	if cfg.Speech.Language != "pt-BR" {
		t.Fatalf("Speech.Language = %q after normalization, want pt-BR", cfg.Speech.Language)
	}
}

func isLocaleLike(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" || trimmed == "auto" || trimmed == "multi" {
		return false
	}
	return true
}
