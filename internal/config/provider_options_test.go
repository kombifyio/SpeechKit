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

func TestDeepgramSTTLanguageOverridesAreMultilingualOnly(t *testing.T) {
	cfg := defaults()
	cfg.Providers.Deepgram.STTLanguage = "de"
	cfg.Providers.Deepgram.STTDetectLanguage = true
	cfg.ProviderOptions.SetOverrides("deepgram", provideropts.ModalitySTT, provideropts.Values{
		provideropts.OptionLanguage:       "en",
		provideropts.OptionDetectLanguage: true,
		provideropts.OptionFillerWords:    true,
	})

	NormalizeDeepgramSTTCodeSwitching(cfg)
	if got := cfg.Providers.Deepgram.STTLanguage; got != DeepgramSTTMultilingualLanguage {
		t.Fatalf("legacy deepgram stt_language = %q, want %q", got, DeepgramSTTMultilingualLanguage)
	}
	if cfg.Providers.Deepgram.STTDetectLanguage {
		t.Fatal("legacy deepgram detect_language = true, want false")
	}
	if got := cfg.ProviderOptions.Deepgram.STT.Language; got != DeepgramSTTMultilingualLanguage {
		t.Fatalf("provider_options.deepgram.stt.language = %q, want %q", got, DeepgramSTTMultilingualLanguage)
	}
	if cfg.ProviderOptions.Deepgram.STT.DetectLanguage != nil {
		t.Fatalf("provider_options.deepgram.stt.detect_language = %#v, want nil", cfg.ProviderOptions.Deepgram.STT.DetectLanguage)
	}

	values := ProviderOptionOverridesFor(cfg, "deepgram", provideropts.ModalitySTT)
	if got := values.String(provideropts.OptionLanguage); got != DeepgramSTTMultilingualLanguage {
		t.Fatalf("effective deepgram language = %q, want %q", got, DeepgramSTTMultilingualLanguage)
	}
	if values.Has(provideropts.OptionDetectLanguage) {
		t.Fatalf("effective deepgram detect_language should be removed, got %#v", values.Get(provideropts.OptionDetectLanguage))
	}
	if !values.Bool(provideropts.OptionFillerWords) {
		t.Fatal("non-language Deepgram override was not preserved")
	}
}

func TestDeepgramSTTManifestDoesNotExposeFixedLanguageControls(t *testing.T) {
	manifest, ok := provideropts.FindManifest("deepgram", provideropts.ModalitySTT)
	if !ok {
		t.Fatal("deepgram STT manifest missing")
	}
	support := manifest.SupportByID()
	if _, ok := support[provideropts.OptionLanguage]; ok {
		t.Fatal("deepgram STT manifest exposes language override")
	}
	if _, ok := support[provideropts.OptionDetectLanguage]; ok {
		t.Fatal("deepgram STT manifest exposes detect_language")
	}
}
