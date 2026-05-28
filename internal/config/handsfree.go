package config

import "strings"

const defaultHandsFreeAutoEndSilenceCutoffSec = 10

// NormalizeHandsFreeConfig keeps the user-facing [hands_free] block and the
// legacy low-level [wakeword] block in sync. When handsFreeDefined is false,
// old configs without [hands_free] are migrated from [wakeword]. When true,
// [hands_free] is treated as the source of truth and Wakeword is mirrored for
// existing runtime paths and sidecars.
func NormalizeHandsFreeConfig(cfg *Config, handsFreeDefined bool) {
	if cfg == nil {
		return
	}
	if handsFreeDefined {
		normalizeHandsFreeSource(cfg)
		syncWakewordFromHandsFree(cfg)
		return
	}
	deriveHandsFreeFromWakeword(cfg)
	syncWakewordFromHandsFree(cfg)
}

func normalizeHandsFreeSource(cfg *Config) {
	cfg.HandsFree.TargetMode = NormalizeHandsFreeTargetMode(cfg.HandsFree.TargetMode)
	cfg.HandsFree.ActivationPhraseID = strings.TrimSpace(cfg.HandsFree.ActivationPhraseID)
	if cfg.HandsFree.ActivationPhraseID == "" {
		cfg.HandsFree.ActivationPhraseID = "hey_quby"
	}
	if cfg.HandsFree.AutoEndSilenceCutoffSec <= 0 {
		cfg.HandsFree.AutoEndSilenceCutoffSec = defaultHandsFreeAutoEndSilenceCutoffSec
	}
	if cfg.HandsFree.TargetMode == HandsFreeTargetDictationUIAssisted {
		cfg.HandsFree.VoiceOutputEnabled = false
	}
}

func deriveHandsFreeFromWakeword(cfg *Config) {
	cfg.HandsFree.Enabled = cfg.Wakeword.Enabled
	cfg.HandsFree.ActivationPhraseID = strings.TrimSpace(cfg.Wakeword.PhraseID)
	if cfg.HandsFree.ActivationPhraseID == "" {
		cfg.HandsFree.ActivationPhraseID = "hey_quby"
	}
	cfg.HandsFree.TargetMode = WakewordDefaultModeToHandsFreeTarget(cfg.Wakeword.DefaultMode)
	cfg.HandsFree.AutoEndSilenceCutoffSec = cfg.Wakeword.AutoEnd.SilenceCutoffSec
	if cfg.HandsFree.AutoEndSilenceCutoffSec <= 0 {
		cfg.HandsFree.AutoEndSilenceCutoffSec = defaultHandsFreeAutoEndSilenceCutoffSec
	}
	cfg.HandsFree.VoiceOutputEnabled = cfg.HandsFree.TargetMode != HandsFreeTargetDictationUIAssisted && cfg.TTS.Enabled
}

func syncWakewordFromHandsFree(cfg *Config) {
	cfg.Wakeword.Enabled = cfg.HandsFree.Enabled
	cfg.Wakeword.PhraseID = cfg.HandsFree.ActivationPhraseID
	cfg.Wakeword.DefaultMode = HandsFreeTargetToWakewordDefaultMode(cfg.HandsFree.TargetMode)
	if cfg.HandsFree.AutoEndSilenceCutoffSec > 0 {
		cfg.Wakeword.AutoEnd.SilenceCutoffSec = cfg.HandsFree.AutoEndSilenceCutoffSec
	}
}
