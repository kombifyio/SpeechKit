//go:build linux

package core

import (
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

func deepgramOptionsFromConfig(cfg *config.Config) stt.DeepgramOptions {
	if cfg == nil {
		return stt.DeepgramOptions{}
	}
	dg := cfg.Providers.Deepgram
	return stt.DeepgramOptions{
		Configured:            true,
		SmartFormat:           dg.STTSmartFormat,
		Dictation:             dg.STTDictation,
		FillerWords:           dg.STTFillerWords,
		Numerals:              dg.STTNumerals,
		DetectLanguage:        dg.STTDetectLanguage,
		LanguageOverride:      config.DeepgramSTTLanguageOverride(dg.STTLanguage),
		UseVocabularyKeyterms: dg.STTUseVocabularyKeyterms,
		Keyterms:              stt.ParseDeepgramKeyterms(dg.STTKeyterms),
		EndpointingMs:         dg.STTEndpointingMs,
	}
}
