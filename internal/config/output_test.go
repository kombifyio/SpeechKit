package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputConfigDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.Output.DefaultStrategy != OutputStrategyAuto {
		t.Errorf("DefaultStrategy = %q, want auto", cfg.Output.DefaultStrategy)
	}
	if !cfg.Output.RestoreClipboard {
		t.Error("RestoreClipboard = false, want true by default")
	}
	if cfg.Output.ModifierWaitMs != 1000 || cfg.Output.FocusVerifyMs != 300 || cfg.Output.RestoreDelayMs != 300 {
		t.Errorf("timing defaults = %d/%d/%d, want 1000/300/300",
			cfg.Output.ModifierWaitMs, cfg.Output.FocusVerifyMs, cfg.Output.RestoreDelayMs)
	}
}

func TestNormalizeOutputConfig(t *testing.T) {
	cfg := &Config{Output: OutputConfig{
		DefaultStrategy: " TYPE ",
		ModifierWaitMs:  -5,
		FocusVerifyMs:   0,
		RestoreDelayMs:  0,
		AppOverrides: []OutputAppOverride{
			{ProcessName: " Termius.EXE ", Strategy: "auto", PasteCombo: "CTRL+SHIFT+V"},
			{ProcessName: "", Strategy: "type"},                              // dropped: no process name
			{ProcessName: "notepad", Strategy: "bogus", PasteCombo: "alt+v"}, // dropped: nothing valid left
			{ProcessName: "Putty", PasteCombo: "shift+insert"},
		},
	}}

	NormalizeOutputConfig(cfg)

	out := cfg.Output
	if out.DefaultStrategy != OutputStrategyType {
		t.Errorf("DefaultStrategy = %q, want type", out.DefaultStrategy)
	}
	if out.ModifierWaitMs != 1000 || out.FocusVerifyMs != 300 || out.RestoreDelayMs != 300 {
		t.Errorf("timings = %d/%d/%d, want clamped to 1000/300/300",
			out.ModifierWaitMs, out.FocusVerifyMs, out.RestoreDelayMs)
	}
	if len(out.AppOverrides) != 2 {
		t.Fatalf("AppOverrides = %#v, want 2 surviving entries", out.AppOverrides)
	}
	first := out.AppOverrides[0]
	if first.ProcessName != "termius" || first.Strategy != "" || first.PasteCombo != OutputPasteComboCtrlShiftV {
		t.Errorf("first override = %+v, want termius / inherit strategy / ctrl+shift+v", first)
	}
	second := out.AppOverrides[1]
	if second.ProcessName != "putty" || second.PasteCombo != OutputPasteComboShiftInsert {
		t.Errorf("second override = %+v, want putty / shift+insert", second)
	}
}

func TestNormalizeOutputStrategy(t *testing.T) {
	cases := []struct {
		in, fallback, want string
	}{
		{"auto", OutputStrategyAuto, OutputStrategyAuto},
		{" Clipboard ", OutputStrategyAuto, OutputStrategyClipboard},
		{"TYPE", OutputStrategyAuto, OutputStrategyType},
		{"", OutputStrategyClipboard, OutputStrategyClipboard},
		{"bogus", OutputStrategyAuto, OutputStrategyAuto},
	}
	for _, tc := range cases {
		if got := NormalizeOutputStrategy(tc.in, tc.fallback); got != tc.want {
			t.Errorf("NormalizeOutputStrategy(%q, %q) = %q, want %q", tc.in, tc.fallback, got, tc.want)
		}
	}
}

func TestOutputConfigLoadSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[output]
default_strategy = "clipboard"
restore_clipboard = false
modifier_wait_ms = 500

[[output.app_overrides]]
process_name = "Termius"
paste_combo = "ctrl+shift+v"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	assertLoaded := func(cfg *Config, stage string) {
		t.Helper()
		if cfg.Output.DefaultStrategy != OutputStrategyClipboard {
			t.Errorf("%s: DefaultStrategy = %q, want clipboard", stage, cfg.Output.DefaultStrategy)
		}
		if cfg.Output.RestoreClipboard {
			t.Errorf("%s: RestoreClipboard = true, want explicit false to survive the defaults overlay", stage)
		}
		if cfg.Output.ModifierWaitMs != 500 {
			t.Errorf("%s: ModifierWaitMs = %d, want 500", stage, cfg.Output.ModifierWaitMs)
		}
		if cfg.Output.FocusVerifyMs != 300 {
			t.Errorf("%s: FocusVerifyMs = %d, want default 300 for unset key", stage, cfg.Output.FocusVerifyMs)
		}
		if len(cfg.Output.AppOverrides) != 1 ||
			cfg.Output.AppOverrides[0].ProcessName != "termius" ||
			cfg.Output.AppOverrides[0].PasteCombo != OutputPasteComboCtrlShiftV {
			t.Errorf("%s: AppOverrides = %#v, want normalized termius/ctrl+shift+v", stage, cfg.Output.AppOverrides)
		}
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertLoaded(cfg, "load")

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after Save error = %v", err)
	}
	assertLoaded(reloaded, "reload")
}
