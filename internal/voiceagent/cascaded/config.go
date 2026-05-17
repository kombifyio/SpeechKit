package cascaded

// Config tunes turn detection and processing. Zero values are filled by
// NewProvider with sensible defaults via WithDefaults().
type Config struct {
	// SilenceRMSThreshold is the RMS level (0.0-1.0) below which a frame
	// is considered silence. Default 0.02.
	SilenceRMSThreshold float64
	// SilenceTurnMs is the minimum silence duration before the current
	// turn is committed. Default 800 ms.
	SilenceTurnMs int
	// MinTurnMs is the minimum accumulated non-silence needed to treat a
	// buffer as a real turn. Default 300 ms. Filters coughs and clicks.
	MinTurnMs int
	// MaxTurnMs caps a runaway turn before forcing processing. Default
	// 30_000 ms.
	MaxTurnMs int
	// HistoryTurns is how many (user, assistant) turns to keep as rolling
	// context for the agent flow. Default 5.
	HistoryTurns int
	// TTSFormat is the audio format the TTS provider should emit.
	// Default "mp3".
	TTSFormat string
	// TTSSpeed is the TTS speech rate. Default 1.0.
	TTSSpeed float64
}

// WithDefaults returns a copy of cfg with every zero field replaced by
// a production default.
func (cfg Config) WithDefaults() Config {
	if cfg.SilenceRMSThreshold <= 0 {
		cfg.SilenceRMSThreshold = 0.02
	}
	if cfg.SilenceTurnMs <= 0 {
		cfg.SilenceTurnMs = 800
	}
	if cfg.MinTurnMs <= 0 {
		cfg.MinTurnMs = 300
	}
	if cfg.MaxTurnMs <= 0 {
		cfg.MaxTurnMs = 30000
	}
	if cfg.HistoryTurns <= 0 {
		cfg.HistoryTurns = 5
	}
	if cfg.TTSFormat == "" {
		cfg.TTSFormat = "mp3"
	}
	if cfg.TTSSpeed == 0 {
		cfg.TTSSpeed = 1.0
	}
	return cfg
}
