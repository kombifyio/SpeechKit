// Package telemetry installs the process-wide OpenTelemetry trace pipeline that
// the Framework kernel's spans (STT routing, TTS, Voice Agent, server
// lifecycle) feed into. The kernel itself only ever calls otel.Tracer(...),
// which is a zero-cost no-op until ConfigureTracing installs a real
// TracerProvider here — so this package is the single, vendor-neutral seam
// between "SpeechKit emits OpenTelemetry" and "the spans actually go
// somewhere". It exports over OTLP/HTTP, so any OTLP receiver works (a local
// collector, Grafana Tempo, or Sentry's OTLP ingestion); nothing in here is
// vendor-specific beyond the endpoint URL + auth header the caller supplies.
package telemetry

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TracingOptions configures the OTLP/HTTP trace exporter and the global
// TracerProvider it backs.
type TracingOptions struct {
	// Endpoint is the full OTLP/HTTP traces URL (scheme + host + path), e.g.
	// https://oORG.ingest.de.sentry.io/api/PROJECT/otlp/v1/traces. Empty
	// disables tracing entirely (ConfigureTracing becomes a no-op).
	Endpoint string
	// Headers are attached to every export request — e.g. an auth header such
	// as {"x-sentry-auth": "sentry sentry_key=..."}.
	Headers map[string]string
	// SampleRate is the head sampling ratio in (0,1). Values <=0 or >=1 mean
	// always-sample, the right default for a low-traffic dogfood deployment.
	SampleRate float64
	// ServiceName + Environment + Release tag the exported resource so a
	// backend can separate services and deployments.
	ServiceName string
	Environment string
	Release     string
}

// noopShutdown is returned whenever no provider was installed, so callers can
// always `defer shutdown(ctx)` without a nil check.
func noopShutdown(context.Context) error { return nil }

// ConfigureTracing installs a process-global OTel TracerProvider that batches
// the framework's spans to opts.Endpoint over OTLP/HTTP. It returns a shutdown
// func that flushes pending spans and stops the provider; call it on graceful
// shutdown. When opts.Endpoint is empty it is a no-op: no provider is set, so
// otel.Tracer spans stay zero-cost and the prior (no-op) behaviour is kept.
func ConfigureTracing(ctx context.Context, opts TracingOptions) (func(context.Context) error, error) {
	if strings.TrimSpace(opts.Endpoint) == "" {
		return noopShutdown, nil
	}

	expOpts := []otlptracehttp.Option{otlptracehttp.WithEndpointURL(opts.Endpoint)}
	if len(opts.Headers) > 0 {
		expOpts = append(expOpts, otlptracehttp.WithHeaders(opts.Headers))
	}
	exp, err := otlptracehttp.New(ctx, expOpts...)
	if err != nil {
		return noopShutdown, fmt.Errorf("telemetry: create OTLP trace exporter: %w", err)
	}

	sampler := sdktrace.AlwaysSample()
	if opts.SampleRate > 0 && opts.SampleRate < 1 {
		// ParentBased so a sampled inbound trace keeps its children sampled.
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(opts.SampleRate))
	}

	var attrs []attribute.KeyValue
	if s := strings.TrimSpace(opts.ServiceName); s != "" {
		attrs = append(attrs, attribute.String("service.name", s))
	}
	if e := strings.TrimSpace(opts.Environment); e != "" {
		attrs = append(attrs, attribute.String("deployment.environment", e))
	}
	if r := strings.TrimSpace(opts.Release); r != "" {
		attrs = append(attrs, attribute.String("service.version", r))
	}
	// NewSchemaless avoids the schema-URL conflict that resource.Merge with
	// resource.Default() raises; service.name/environment are all a backend
	// needs to attribute the spans.
	res := resource.NewSchemaless(attrs...)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
