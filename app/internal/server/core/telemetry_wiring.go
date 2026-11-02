//go:build linux

package core

import (
	"context"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/telemetry"
)

// initServerTelemetry installs the process-global OpenTelemetry trace pipeline
// from cfg.Telemetry. This is the seam that turns the Framework kernel's
// otel.Tracer spans (STT routing, TTS, Voice Agent) from zero-cost no-ops into
// real spans exported over OTLP — the server dogfooding its own telemetry. It
// retains local instrumentation when no traces_otlp_endpoint is configured;
// the OSS default sends no spans over the network.
func initServerTelemetry(ctx context.Context, app *App) {
	t := app.Cfg.Telemetry

	headers := map[string]string{}
	if t.TraceExportEnabled() {
		if name, value := t.ResolveOTLPAuthHeader(); name != "" {
			if value == "" {
				slog.Warn("telemetry: OTLP auth header env resolved empty; exporting without auth",
					"header", name, "env", t.OTLPAuthHeaderEnv)
			} else {
				headers[name] = value
			}
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
		Revision:    app.SourceRevision,
	})
	app.telemetryShutdown = shutdown
	if err != nil {
		slog.Warn("telemetry: trace export init failed; local tracing retained", "err", err)
		app.Health.SetReadyWithOptions("telemetry.tracing", StatusDegraded, err.Error(), ComponentOptions{
			Blocking: false,
			Kind:     "feature",
		})
		return
	}
	if !t.TraceExportEnabled() {
		app.Health.SetReadyWithOptions("telemetry.tracing", StatusOK, "local instrumentation active; export unconfigured", ComponentOptions{Blocking: false, Kind: "feature"})
		return
	}

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
