//go:build linux

package core

import (
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
)

func deepgramOptionsFromConfig(cfg *config.Config) deepgram.Options {
	if cfg == nil {
		return deepgram.Options{}
	}
	dg := cfg.Providers.Deepgram
	return deepgram.Options{
		Configured:            true,
		SmartFormat:           dg.STTSmartFormat,
		Dictation:             dg.STTDictation,
		FillerWords:           dg.STTFillerWords,
		Numerals:              dg.STTNumerals,
		DetectLanguage:        dg.STTDetectLanguage,
		LanguageOverride:      config.DeepgramSTTLanguageOverride(dg.STTLanguage),
		UseVocabularyKeyterms: dg.STTUseVocabularyKeyterms,
		Keyterms:              deepgram.ParseKeyterms(dg.STTKeyterms),
		EndpointingMs:         dg.STTEndpointingMs,
	}
}
