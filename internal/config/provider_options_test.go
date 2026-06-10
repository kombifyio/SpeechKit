package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func TestProviderOptionOverridesPreserveExplicitFalse(t *testing.T) {
	overrides := ProviderOptionOverrides{}
	overrides.SetValues(provideropts.Values{
		provideropts.OptionDetectLanguage: false,
		provideropts.OptionSmartFormat:    false,
		provideropts.OptionLanguage:       "en",
	})

	values := overrides.Values()
	if !values.Has(provideropts.OptionDetectLanguage) {
		t.Fatal("detect_language override missing")
	}
	if values.Bool(provideropts.OptionDetectLanguage) {
		t.Fatal("detect_language = true, want explicit false")
	}
	if !values.Has(provideropts.OptionSmartFormat) {
		t.Fatal("smart_format override missing")
	}
	if values.Bool(provideropts.OptionSmartFormat) {
		t.Fatal("smart_format = true, want explicit false")
	}
	if values.String(provideropts.OptionLanguage) != "en" {
		t.Fatalf("language = %q, want en", values.String(provideropts.OptionLanguage))
	}
}

func TestProviderOptionsSaveReloadPreservesFalseOverride(t *testing.T) {
	cfg := defaults()
	cfg.ProviderOptions.SetOverrides("openai", provideropts.ModalitySTT, provideropts.Values{
		provideropts.OptionDetectLanguage: false,
		provideropts.OptionLanguage:       "en",
	})

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if !strings.Contains(string(raw), "detect_language = false") {
		t.Fatalf("saved config does not contain explicit false override:\n%s", raw)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	values := ProviderOptionOverridesFor(loaded, "openai", provideropts.ModalitySTT)
	if !values.Has(provideropts.OptionDetectLanguage) {
		t.Fatal("detect_language override missing after reload")
	}
	if values.Bool(provideropts.OptionDetectLanguage) {
		t.Fatal("detect_language = true after reload, want explicit false")
	}
	if values.String(provideropts.OptionLanguage) != "en" {
		t.Fatalf("language = %q, want en", values.String(provideropts.OptionLanguage))
	}
}

func TestLegacyDeepgramDefaultsDoNotBecomeProviderOverrides(t *testing.T) {
	cfg := defaults()

	values := ProviderOptionOverridesFor(cfg, "deepgram", provideropts.ModalitySTT)
	if values.Has(provideropts.OptionSmartFormat) {
		t.Fatalf("smart_format default projected as override: %#v", values.Get(provideropts.OptionSmartFormat))
	}
	if values.Has(provideropts.OptionDetectLanguage) {
		t.Fatalf("detect_language default projected as override: %#v", values.Get(provideropts.OptionDetectLanguage))
	}

	cfg.Providers.Deepgram.STTDictation = true
	cfg.Providers.Deepgram.STTLanguage = "multi"
	values = ProviderOptionOverridesFor(cfg, "deepgram", provideropts.ModalitySTT)
	if !values.Bool(provideropts.OptionDictation) {
		t.Fatal("legacy deepgram dictation=true was not projected")
	}
	if got := values.String(provideropts.OptionLanguage); got != "multi" {
		t.Fatalf("legacy deepgram language = %q, want multi", got)
	}
}
