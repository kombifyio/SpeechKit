//go:build linux

// Package cli holds the small amount of CLI-level glue the
// speechkit-server and speechkit-voice binaries share. Each binary
// keeps its own `cmd/<name>/main.go` (so go install + ldflags work
// the conventional way), but the actual flag parsing, config loading,
// logger wiring, and lifecycle handoff to internal/server/core lives
// here. That keeps the two mains essentially boilerplate — they
// differ only in the default mode set and the log banner.
package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/core"
)

// Options describes the per-binary defaults each main supplies.
type Options struct {
	// Banner is the short program name printed in startup logs and the
	// CLI usage line. "speechkit-server" or "speechkit-voice".
	Banner string

	// Version is set at build time via -ldflags="-X main.version=...".
	// Each binary's main.go forwards its own version string here.
	Version string

	// DefaultModes is the mode set used when neither `[server].modes`
	// in config nor the `--modes` flag are provided. The full server
	// passes nil (= "all three"); the voice-only binary passes
	// []string{"voiceagent"}.
	DefaultModes []string
}

// Run is the canonical entry point each cmd/<name>/main.go calls. It
// owns the entire process lifecycle: parse flags, load config, set up
// logging, hand off to core.Run, log the exit reason, return. It does
// not return an exit code — it calls os.Exit on failure so callers
// stay one line.
//
// The body delegates to runOnce so we can keep `defer` clean: any
// non-zero exit code goes through os.Exit(code) at the end of Run,
// after all of runOnce's deferred cleanup has run. Calling os.Exit
// directly inside runOnce would short-circuit the deferred signal
// stop() that core.NotifySignals registers.
func Run(opts Options) {
	if code := runOnce(opts); code != 0 {
		os.Exit(code)
	}
}

func runOnce(opts Options) int {
	banner := strings.TrimSpace(opts.Banner)
	if banner == "" {
		banner = "speechkit-server"
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "dev"
	}

	fs := flag.NewFlagSet(banner, flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath(), "path to config.toml")
	modesFlag := fs.String("modes", "", "comma-separated list of enabled modes (dictation,assist,voiceagent); empty = built-in default for this binary")
	listenArg := fs.String("listen", "", "override server.listen_addr, e.g. :8080")
	printVer := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		// flag.ExitOnError already prints + exits; this is
		// effectively unreachable but keeps the linter quiet.
		return 2
	}

	if *printVer {
		// Best-effort print to stdout — exit code 0 even if stdout
		// is closed, since `--version` is supposed to be a cheap probe.
		_, _ = fmt.Fprintln(os.Stdout, version)
		return 0
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: load config %q: %v\n", banner, *configPath, err)
		return 2
	}

	// CLI flags > config > built-in default. The CLI flag is the
	// trump card so an operator can override the per-binary default
	// without editing config.toml.
	switch {
	case strings.TrimSpace(*modesFlag) != "":
		cfg.Server.Modes = splitModes(*modesFlag)
	case len(cfg.Server.Modes) == 0 && len(opts.DefaultModes) > 0:
		cfg.Server.Modes = append([]string(nil), opts.DefaultModes...)
	}
	if trimmed := strings.TrimSpace(*listenArg); trimmed != "" {
		cfg.Server.ListenAddr = trimmed
	}

	setupLogger(cfg)

	slog.Info(banner+" starting",
		"version", version,
		"config", *configPath,
		"listen", cfg.Server.ListenAddr,
		"modes", effectiveModes(cfg),
		"auth", cfg.Server.AuthMode,
	)

	ctx, stop := core.NotifySignals(context.Background())
	defer stop()

	if err := core.Run(ctx, cfg, core.RunOptions{Version: version}); err != nil {
		slog.Error(banner+" terminated with error", "err", err)
		return 1
	}

	slog.Info(banner + " stopped cleanly")
	return 0
}

func defaultConfigPath() string {
	if envPath := strings.TrimSpace(os.Getenv("SPEECHKIT_CONFIG_PATH")); envPath != "" {
		return envPath
	}
	return "/etc/speechkit/config.toml"
}

func splitModes(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, strings.ToLower(trimmed))
		}
	}
	return out
}

func effectiveModes(cfg *config.Config) []string {
	if cfg == nil || len(cfg.Server.Modes) == 0 {
		return []string{"dictation", "assist", "voiceagent"}
	}
	return cfg.Server.Modes
}

func setupLogger(cfg *config.Config) {
	level := parseLogLevel(cfg.Server.LogLevel)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(cfg.Server.LogFormat)) {
	case "text":
		handler = slog.NewTextHandler(os.Stderr, opts)
	default: // "json" or unset
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func parseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
