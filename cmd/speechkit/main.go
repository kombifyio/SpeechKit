package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	_ "github.com/kombifyio/SpeechKit/internal/kombify"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// noTelemetry is the parsed value of the --no-telemetry CLI flag.
// Declared as a package-level *bool so app_lifecycle.go can read it after
// flag.Parse() returns. flag.Bool always returns a non-nil pointer, so
// consumers can dereference directly without a nil guard.
var noTelemetry = flag.Bool("no-telemetry", false,
	"disable all outbound non-provider telemetry (currently: update check). "+
		"overrides [telemetry] and [update] in config.toml.")

// collectBundle and includeTranscripts support the --collect-support-bundle
// subcommand (cpv.3.5). When collectBundle is non-empty, main exits after
// writing the ZIP — the regular desktop startup chain is skipped entirely.
var (
	collectBundle = flag.String("collect-support-bundle", "",
		"produce a diagnostics support bundle ZIP at the given path and exit. "+
			"Bundle contains redacted logs, redacted config, system info, and SBOM.")
	includeTranscripts = flag.Bool("include-transcripts", false,
		"include audit-log transcript content in the support bundle (default: redacted). "+
			"Review the bundle before sending when this flag is set.")
)

// installEventLogSource supports the --install-event-log-source subcommand
// (cpv.3.1). Registers the "kombify SpeechKit" Windows Event Log source
// (admin-required) then exits. Idempotent: safe to run more than once.
var installEventLogSource = flag.Bool("install-event-log-source", false,
	"register the 'kombify SpeechKit' Windows Event Log source (requires admin) then exit. "+
		"Required before [audit] event_log_enabled = true takes effect. "+
		"Can also be invoked via the MSI installer custom action.")

var newHuggingFaceProvider = func(model, token string) stt.STTProvider {
	return stt.NewHuggingFaceProvider(model, token)
}

func main() {
	flag.Parse()

	// --install-event-log-source is a standalone admin subcommand: register the
	// Windows Event Log source and exit. Must run before initAppLogging so it
	// does not create/lock the regular log file.
	if *installEventLogSource {
		if err := auditlog.InstallEventLogSource(); err != nil {
			fmt.Fprintln(os.Stderr, "event log install error:", err)
			os.Exit(1)
		}
		fmt.Println("Event Log source 'kombify SpeechKit' registered.")
		os.Exit(0)
	}

	// --collect-support-bundle is a standalone subcommand: write the ZIP and exit.
	// Must be handled before initAppLogging so the log file is not created/locked.
	if *collectBundle != "" {
		err := collectSupportBundle(diagnosticsBundleOptions{
			OutPath:            *collectBundle,
			IncludeTranscripts: *includeTranscripts,
			SinceDays:          defaultSinceDays,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "support-bundle error:", err)
			os.Exit(1)
		}
		fmt.Println("Support bundle written to:", *collectBundle)
		os.Exit(0)
	}

	_, closeLogFile := initAppLogging()
	defer closeLogFile()

	runDesktopApp(closeLogFile)
}

// buildRouter, buildGenkitConfig, buildTTSRouter, validateCloudProviders,
// missingProviderHint, executableDir, defaultLocalModelPath, escapeJS, and
// runtimeConfigPath are in app_init.go.
