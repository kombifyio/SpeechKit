//go:build linux

package core

import (
	"context"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/telemetry"
)

// initServerTelemetry installs the process-global OpenTelemetry trace pipeline
// from cfg.Telemetry. This is the seam that turns the Framework kernel's
// otel.Tracer spans (STT routing, TTS, Voice Agent) from zero-cost no-ops into
// real spans exported over OTLP — the server dogfooding its own telemetry. It
// is a no-op when no traces_otlp_endpoint is configured, so the OSS default
// build sends nothing anywhere.
func initServerTelemetry(ctx context.Context, app *App) {
	t := app.Cfg.Telemetry
	if !t.TraceExportEnabled() {
		app.Health.SetReadyWithOptions("telemetry.tracing", StatusOK, "disabled (no traces_otlp_endpoint)", ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		return
	}

	headers := map[string]string{}
	if name, value := t.ResolveOTLPAuthHeader(); name != "" {
		if value == "" {
			slog.Warn("telemetry: OTLP auth header env resolved empty; exporting without auth",
				"header", name, "env", t.OTLPAuthHeaderEnv)
		} else {
			headers[name] = value
		}
	}

	environment := strings.TrimSpace(t.Environment)
	serviceName := strings.TrimSpace(t.ServiceName)
	if serviceName == "" {
		serviceName = "speechkit-server"
	}

	shutdown, err := telemetry.ConfigureTracing(ctx, telemetry.TracingOptions{
		Endpoint:    t.TracesOTLPEndpoint,
		Headers:     headers,
		SampleRate:  t.TracesSampleRate,
		ServiceName: serviceName,
		Environment: environment,
		Release:     app.Version,
	})
	if err != nil {
		slog.Warn("telemetry: trace export init failed; spans stay no-op", "err", err)
		app.Health.SetReadyWithOptions("telemetry.tracing", StatusDegraded, err.Error(), ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		return
	}
	app.telemetryShutdown = shutdown

	// The endpoint URL carries no secret (auth lives in the header), so it is
	// safe to surface on /readyz and in logs.
	app.Health.SetReady("telemetry.tracing", StatusOK, "exporting OTLP traces to "+strings.TrimSpace(t.TracesOTLPEndpoint))
	slog.Info("telemetry: OTLP trace export enabled",
		"endpoint", strings.TrimSpace(t.TracesOTLPEndpoint),
		"service", serviceName,
		"environment", environment,
		"authenticated", len(headers) > 0,
	)
}
