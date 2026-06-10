package config

import (
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

func SpeechDefaultsValues(cfg *Config) provideropts.Values {
	if cfg == nil {
		return nil
	}
	language := strings.TrimSpace(cfg.Speech.Language)
	if language == "" {
		language = strings.TrimSpace(cfg.General.Language)
	}
	values := provideropts.Values{
		provideropts.OptionLanguage:       firstNonEmptyConfig(language, "de"),
		provideropts.OptionDetectLanguage: cfg.Speech.DetectLanguage,
		provideropts.OptionPunctuation:    cfg.Speech.Punctuation,
		provideropts.OptionSmartFormat:    cfg.Speech.SmartFormat,
		provideropts.OptionVocabularyBias: cfg.Speech.VocabularyBias,
		provideropts.OptionTimestamps:     cfg.Speech.Timestamps,
		provideropts.OptionTurnDetection:  cfg.Speech.TurnDetection,
	}
	if cfg.Speech.EndpointingMs > 0 {
		values[provideropts.OptionEndpointingMs] = cfg.Speech.EndpointingMs
	}
	if voice := strings.TrimSpace(cfg.Speech.Voice); voice != "" {
		values[provideropts.OptionVoice] = voice
	}
	if cfg.Speech.Speed > 0 {
		values[provideropts.OptionSpeed] = cfg.Speech.Speed
	}
	if format := strings.TrimSpace(cfg.Speech.AudioFormat); format != "" {
		values[provideropts.OptionAudioFormat] = format
	}
	return values
}
