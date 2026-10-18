package config

import (
	"path/filepath"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Regression (owner report 2026-10-07): the installer ships config.example.toml
// as a fresh install's config.toml. Pinning one language there made mixed
// German/English dictation drop the English words; a fresh install must
// dictate multilingually until the user explicitly pins a language.
func TestFreshInstallConfigDictatesMultilingually(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "..", "config.example.toml"))
	if err != nil {
		t.Fatalf("load config.example.toml: %v", err)
	}
	if got := CurrentSpeechLanguage(cfg.Speech.Language, cfg.Speech.DetectLanguage); got != stt.LanguageMulti {
		t.Fatalf("fresh install dictation language = %q, want multilingual %q", got, stt.LanguageMulti)
	}
}
