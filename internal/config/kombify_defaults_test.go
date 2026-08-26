package config

import "testing"

func TestApplyKombifyDeploymentDefaults_DeepgramKeyFlipsDefaults(t *testing.T) {
	t.Setenv("DEEPGRAM_API_KEY", "dg-test-key")

	cfg := &Config{}
	cfg.ModelSelection.Dictate.PrimaryProfileID = DefaultDictatePrimaryProfileID
	cfg.ModelSelection.TTS.PrimaryProfileID = DefaultTTSPrimaryProfileID

	notes := ApplyKombifyDeploymentDefaults(cfg)
	if len(notes) == 0 {
		t.Fatal("expected overlay notes when the Deepgram key resolves")
	}

	if got := cfg.ModelSelection.Dictate.PrimaryProfileID; got != "stt.deepgram.nova-3" {
		t.Errorf("Dictate primary = %q, want stt.deepgram.nova-3", got)
	}
	if got := cfg.ModelSelection.Dictate.FallbackProfileID; got != DefaultDictatePrimaryProfileID {
		t.Errorf("Dictate fallback = %q, want demoted %q", got, DefaultDictatePrimaryProfileID)
	}
	if !cfg.Providers.Deepgram.Enabled {
		t.Error("Deepgram STT provider should be enabled")
	}
	if cfg.Providers.Deepgram.STTModel != "nova-3" {
		t.Errorf("Deepgram STT model = %q, want nova-3", cfg.Providers.Deepgram.STTModel)
	}
	if !cfg.Providers.Deepgram.STTSmartFormat || !cfg.Providers.Deepgram.STTNumerals || !cfg.Providers.Deepgram.STTUseVocabularyKeyterms {
		t.Errorf("Deepgram quality defaults = smart:%v numerals:%v keyterms:%v, want true/true/true", cfg.Providers.Deepgram.STTSmartFormat, cfg.Providers.Deepgram.STTNumerals, cfg.Providers.Deepgram.STTUseVocabularyKeyterms)
	}
	if !cfg.TTS.Enabled || !cfg.TTS.Deepgram.Enabled {
		t.Error("Deepgram Aura TTS should be enabled")
	}
	if got := cfg.ModelSelection.TTS.PrimaryProfileID; got != "tts.deepgram.aura-2" {
		t.Errorf("TTS primary = %q, want tts.deepgram.aura-2", got)
	}
	if cfg.ModelSelection.TTS.FallbackProfileID != DefaultTTSPrimaryProfileID {
		t.Errorf("TTS fallback = %q, want demoted %q", cfg.ModelSelection.TTS.FallbackProfileID, DefaultTTSPrimaryProfileID)
	}
	if cfg.VoiceAgent.Provider != "deepgram" {
		t.Errorf("VoiceAgent.Provider = %q, want deepgram", cfg.VoiceAgent.Provider)
	}
	if got := cfg.ModelSelection.VoiceAgent.PrimaryProfileID; got != kombifyDeepgramVAProfileID {
		t.Errorf("VoiceAgent primary = %q, want %q (must match the serving provider)", got, kombifyDeepgramVAProfileID)
	}
	if got := EffectiveVoiceAgentProfileID(cfg); got != kombifyDeepgramVAProfileID {
		t.Errorf("effective VA profile = %q, want %q", got, kombifyDeepgramVAProfileID)
	}
}

func TestApplyKombifyDeploymentDefaults_NoKeyKeepsLocalFirst(t *testing.T) {
	// Bogus, never-provisioned env name so neither the environment nor Doppler
	// resolves a key — this exercises the OSS-neutral no-op path.
	t.Setenv("DOPPLER_PROJECT", "")
	t.Setenv("DOPPLER_CONFIG", "")
	t.Setenv("SPEECHKIT_TEST_ABSENT_DEEPGRAM_KEY", "")

	cfg := &Config{}
	cfg.Providers.Deepgram.APIKeyEnv = "SPEECHKIT_TEST_ABSENT_DEEPGRAM_KEY"
	cfg.ModelSelection.Dictate.PrimaryProfileID = DefaultDictatePrimaryProfileID

	ApplyKombifyDeploymentDefaults(cfg)

	if got := cfg.ModelSelection.Dictate.PrimaryProfileID; got != DefaultDictatePrimaryProfileID {
		t.Errorf("without a Deepgram key, Dictate primary should stay %q, got %q", DefaultDictatePrimaryProfileID, got)
	}
	if cfg.Providers.Deepgram.Enabled {
		t.Error("without a Deepgram key, the provider must remain disabled")
	}
	if cfg.VoiceAgent.Provider != "" {
		t.Errorf("without a Deepgram key, VoiceAgent.Provider should stay empty, got %q", cfg.VoiceAgent.Provider)
	}
}

func TestSetModePrimaryWithFallback(t *testing.T) {
	// Demotes the previous primary into the fallback slot.
	sel := ModeModelSelection{PrimaryProfileID: "old.primary"}
	if !setModePrimaryWithFallback(&sel, "new.primary", "ignored.default") {
		t.Fatal("expected a change")
	}
	if sel.PrimaryProfileID != "new.primary" || sel.FallbackProfileID != "old.primary" {
		t.Errorf("got primary=%q fallback=%q", sel.PrimaryProfileID, sel.FallbackProfileID)
	}

	// Idempotent when already primary.
	if setModePrimaryWithFallback(&sel, "new.primary", "x") {
		t.Error("re-applying the same primary should report no change")
	}

	// Uses the supplied default when there is no previous primary.
	empty := ModeModelSelection{}
	setModePrimaryWithFallback(&empty, "p", "the.default")
	if empty.FallbackProfileID != "the.default" {
		t.Errorf("fallback = %q, want the.default", empty.FallbackProfileID)
	}

	// Respects an existing explicit fallback.
	withFallback := ModeModelSelection{PrimaryProfileID: "a", FallbackProfileID: "explicit"}
	setModePrimaryWithFallback(&withFallback, "b", "default")
	if withFallback.FallbackProfileID != "explicit" {
		t.Errorf("explicit fallback overwritten: %q", withFallback.FallbackProfileID)
	}
}
