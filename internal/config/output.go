package config

import "strings"

// Output strategy values for OutputConfig.DefaultStrategy.
const (
	OutputStrategyAuto      = "auto"
	OutputStrategyClipboard = "clipboard"
	OutputStrategyType      = "type"
)

// Paste combo values for OutputAppOverride.PasteCombo.
const (
	OutputPasteComboCtrlV       = "ctrl+v"
	OutputPasteComboCtrlShiftV  = "ctrl+shift+v"
	OutputPasteComboShiftInsert = "shift+insert"
)

// OutputConfig tunes how the Device-Target injects transcribed text into the
// focused application. Server- and Local-Target have no injection path and
// ignore this block.
type OutputConfig struct {
	// DefaultStrategy: "auto" (per-app paste profiles, recommended),
	// "clipboard" (always plain Ctrl+V paste), or "type" (simulated
	// Unicode typing, bypasses the clipboard).
	DefaultStrategy string `toml:"default_strategy"`
	// ModifierWaitMs bounds how long injection waits for physically held
	// hotkey modifiers to be released before sending the paste chord.
	ModifierWaitMs int `toml:"modifier_wait_ms"`
	// FocusVerifyMs bounds how long injection waits for the target window
	// to reach the foreground.
	FocusVerifyMs int `toml:"focus_verify_ms"`
	// RestoreClipboard restores the previous clipboard content after a
	// clipboard-paste injection.
	RestoreClipboard bool `toml:"restore_clipboard"`
	// RestoreDelayMs is the pause between the paste chord and the clipboard
	// restore, giving the target app time to consume the paste.
	RestoreDelayMs int `toml:"restore_delay_ms"`
	// AppOverrides force a strategy and/or paste chord per process name.
	AppOverrides []OutputAppOverride `toml:"app_overrides"`
}

// OutputAppOverride forces injection behavior for one application.
type OutputAppOverride struct {
	// ProcessName is the executable base name without extension, matched
	// case-insensitively, e.g. "termius".
	ProcessName string `toml:"process_name"`
	// Strategy: "" (inherit), "clipboard", or "type".
	Strategy string `toml:"strategy"`
	// PasteCombo: "" (inherit), "ctrl+v", "ctrl+shift+v", or "shift+insert".
	PasteCombo string `toml:"paste_combo"`
}

func defaultOutputConfig() OutputConfig {
	return OutputConfig{
		DefaultStrategy:  OutputStrategyAuto,
		ModifierWaitMs:   1000,
		FocusVerifyMs:    300,
		RestoreClipboard: true,
		RestoreDelayMs:   300,
	}
}

// NormalizeOutputStrategy maps arbitrary input to a valid strategy value,
// falling back when the input is unknown.
func NormalizeOutputStrategy(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case OutputStrategyAuto:
		return OutputStrategyAuto
	case OutputStrategyClipboard:
		return OutputStrategyClipboard
	case OutputStrategyType:
		return OutputStrategyType
	}
	return fallback
}

// NormalizeOutputConfig clamps timings, normalizes strategy/combo values,
// and drops unusable app overrides.
func NormalizeOutputConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	out := &cfg.Output
	d := defaultOutputConfig()
	out.DefaultStrategy = NormalizeOutputStrategy(out.DefaultStrategy, d.DefaultStrategy)
	if out.ModifierWaitMs <= 0 {
		out.ModifierWaitMs = d.ModifierWaitMs
	}
	if out.FocusVerifyMs <= 0 {
		out.FocusVerifyMs = d.FocusVerifyMs
	}
	if out.RestoreDelayMs <= 0 {
		out.RestoreDelayMs = d.RestoreDelayMs
	}
	normalized := out.AppOverrides[:0]
	for _, ov := range out.AppOverrides {
		ov.ProcessName = strings.ToLower(strings.TrimSpace(ov.ProcessName))
		ov.ProcessName = strings.TrimSuffix(ov.ProcessName, ".exe")
		if ov.ProcessName == "" {
			continue
		}
		ov.Strategy = normalizeOutputOverrideStrategy(ov.Strategy)
		ov.PasteCombo = normalizeOutputOverrideCombo(ov.PasteCombo)
		if ov.Strategy == "" && ov.PasteCombo == "" {
			continue // nothing left to override
		}
		normalized = append(normalized, ov)
	}
	out.AppOverrides = normalized
}

// normalizeOutputOverrideStrategy returns a concrete override strategy or ""
// for inherit; "auto" and unknown values both mean inherit.
func normalizeOutputOverrideStrategy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case OutputStrategyClipboard:
		return OutputStrategyClipboard
	case OutputStrategyType:
		return OutputStrategyType
	}
	return ""
}

func normalizeOutputOverrideCombo(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case OutputPasteComboCtrlV:
		return OutputPasteComboCtrlV
	case OutputPasteComboCtrlShiftV:
		return OutputPasteComboCtrlShiftV
	case OutputPasteComboShiftInsert:
		return OutputPasteComboShiftInsert
	}
	return ""
}
