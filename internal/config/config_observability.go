package config

// Update, logging, audit, telemetry, and feedback configuration types.

import "strings"

// UpdateConfig controls the auto-update channel. The default values mirror
// the historical hard-coded constants so existing installations keep working.
// Enterprise customers set Enabled = false (full air-gap) or override
// ManifestURL with an internal mirror that serves the same JSON shape as
// https://api.github.com/repos/<owner>/<repo>/releases/latest.
type UpdateConfig struct {
	Enabled     bool   `toml:"enabled"`
	ManifestURL string `toml:"manifest_url"`
	// AutoDownload fetches an available update in the background as soon as
	// the check finds one, so installing is a single click instead of a
	// download wait. Installation is never automatic: the user always
	// confirms, and the app never restarts itself. Set false to keep the
	// fully manual flow (check -> download on demand).
	AutoDownload           bool   `toml:"auto_download"`
	CheckIntervalHours     int    `toml:"check_interval_hours"`
	SignaturePinThumbprint string `toml:"signature_pin_thumbprint"` // optional Authenticode SHA-1 thumbprint; if set, installer signature verification additionally checks cert thumbprint matches (defense against compromised signing cert)
}

// LoggingConfig controls the general application log — the stream that
// surfaces transcription events, mode switches, wake-word triggers and is
// visible in the dashboard's "Logs" tab when enabled. This is one of two
// independent log surfaces in SpeechKit; the other is AuditConfig (the
// SOC2/ISO27001 compliance trail). Both default to OFF so a privacy-first
// install writes nothing to disk until the operator explicitly opts in.
//
// Level options: "debug" | "info" | "warn" | "error" | "off". The
// SPEECHKIT_LOG_LEVEL environment variable overrides this field at startup
// — the recommended path for support engineers who need a one-session
// debug toggle without touching config.toml. When Level="off" the
// fanoutWriter short-circuits to a no-op before any I/O syscall, so even
// extremely chatty hot paths (overlay sync loop, audio status pumps) carry
// zero log overhead.
//
// MaxFileSizeMB and MaxFiles apply only when Level != "off". They are
// preserved at enterprise-friendly defaults (50 MB / 30 files) for the
// case where an operator opts logging in.
type LoggingConfig struct {
	MaxFileSizeMB int    `toml:"max_file_size_mb"`
	MaxFiles      int    `toml:"max_files"`
	Level         string `toml:"level"` // "debug" | "info" | "warn" | "error" | "off"
}

// AuditConfig controls the dedicated audit-log stream introduced in Phase 0.
// This is the structured compliance trail (SOC2 / ISO27001 evidence) — no
// transcript content, only event metadata (when, who, which model, success
// vs failure). It is one of two independent log surfaces in SpeechKit; the
// other is LoggingConfig (the general application log).
//
// As of 2026-05-19 Enabled defaults to FALSE — opt-in. The earlier
// "default-true so we have evidence" stance was overridden by the privacy
// principle: a user with no compliance obligations should not produce
// audit artefacts on disk by default. Enterprises that need the audit
// trail flip Enabled=true in Settings → Compliance (or via config.toml)
// and configure RetentionDays plus the OTLP exporter.
type AuditConfig struct {
	Enabled         bool   `toml:"enabled"`
	RetentionDays   int    `toml:"retention_days"`
	EventLogEnabled bool   `toml:"event_log_enabled"` // wired in P2.1 (cpv.3.1) — Windows Event Log mirror
	OTLPEndpoint    string `toml:"otlp_endpoint"`     // wired in P2.2 (cpv.3.2) — OTLP exporter
	OTLPCertFile    string `toml:"otlp_cert_file"`
	OTLPKeyFile     string `toml:"otlp_key_file"`
	OTLPCAFile      string `toml:"otlp_ca_file"`
}

// TelemetryConfig is the single switch surface for every outbound non-provider
// HTTP call SpeechKit may make. Today such calls are the auto-update check and
// the OpenTelemetry trace export; future calls (crash reports, usage stats)
// must add a field here rather than create a parallel toggle.
type TelemetryConfig struct {
	UpdateCheck bool `toml:"update_check"`

	// TracesOTLPEndpoint enables exporting the framework's OpenTelemetry spans
	// (STT routing, TTS, Voice Agent, server lifecycle) to a vendor-neutral
	// OTLP/HTTP traces receiver. When empty (the default) the server installs
	// no TracerProvider, so every otel.Tracer span stays a zero-cost no-op and
	// behaviour is unchanged. Point it at any OTLP/HTTP collector; for Sentry's
	// OTLP ingestion use the full URL
	// https://<org>.ingest.<region>.sentry.io/api/<project>/otlp/v1/traces.
	TracesOTLPEndpoint string `toml:"traces_otlp_endpoint"`
	// TracesSampleRate is the head sampling ratio in [0,1]. 0 (or >=1) means
	// always-sample, which is the right default for a low-traffic dogfood
	// deployment where we want to see every trace.
	TracesSampleRate float64 `toml:"traces_sample_rate"`
	// The OTLP auth secret stays out of config files. OTLPAuthHeaderName is the
	// HTTP header to attach (e.g. "x-sentry-auth"); its value is read at startup
	// from the environment variable named by OTLPAuthHeaderEnv (e.g. a
	// Doppler/Render secret holding "sentry sentry_key=<public_key>"). When
	// either is empty no auth header is sent (suitable for a local collector).
	OTLPAuthHeaderName string `toml:"otlp_auth_header_name"`
	OTLPAuthHeaderEnv  string `toml:"otlp_auth_header_env"`
	// ServiceName + Environment tag every exported span's resource so Sentry can
	// separate speechkit-server from other services and staging from prod.
	ServiceName string `toml:"service_name"`
	Environment string `toml:"environment"`
}

// TraceExportEnabled reports whether OTLP trace export is configured.
func (t TelemetryConfig) TraceExportEnabled() bool {
	return strings.TrimSpace(t.TracesOTLPEndpoint) != ""
}

// ResolveOTLPAuthHeader returns the configured OTLP auth header name and its
// value resolved from the environment, or empty strings when not configured.
func (t TelemetryConfig) ResolveOTLPAuthHeader() (name, value string) {
	name = strings.TrimSpace(t.OTLPAuthHeaderName)
	envName := strings.TrimSpace(t.OTLPAuthHeaderEnv)
	if name == "" || envName == "" {
		return "", ""
	}
	return name, strings.TrimSpace(ResolveSecret(envName))
}

type FeedbackConfig struct {
	SaveAudio          bool   `toml:"save_audio"`
	AudioRetentionDays int    `toml:"audio_retention_days"`
	DBPath             string `toml:"db_path"`
	MaxAudioStorageMB  int    `toml:"max_audio_storage_mb"`
}
