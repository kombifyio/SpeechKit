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
	"regexp"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TracingOptions configures the OTLP/HTTP trace exporter and the global
// TracerProvider it backs.
type TracingOptions struct {
	// Endpoint is the full OTLP/HTTP traces URL (scheme + host + path), e.g.
	// https://oORG.ingest.de.sentry.io/api/PROJECT/otlp/v1/traces. Empty
	// disables export; local instrumentation and W3C propagation stay installed.
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
	// Revision is the exact lowercase 40-hex source commit of the build. It
	// tags the resource as vcs.ref.head.revision and every span as
	// sentry.release, the attribute Sentry's OTLP ingestion reads as the
	// release; any other value is ignored.
	Revision string
}

// exactRevision admits only a full lowercase commit SHA as a release.
var exactRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ConfigureTracing installs a process-global OTel TracerProvider that batches
// the framework's spans to opts.Endpoint over OTLP/HTTP. It returns a shutdown
// func that flushes pending spans and stops the provider; call it on graceful
// shutdown. An absent or failed exporter leaves the shared local SDK, resource
// and W3C propagator installed. It never substitutes a cloud destination.
func ConfigureTracing(ctx context.Context, opts TracingOptions) (func(context.Context) error, error) {
	sampler := sdktrace.AlwaysSample()
	if opts.SampleRate > 0 && opts.SampleRate < 1 {
		// ParentBased so a sampled inbound trace keeps its children sampled.
		sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(opts.SampleRate))
	}
	// A ratio is right for ordinary traffic and wrong for a lost dictation.
	// See KeepLostOutcomes in outcome.go.
	sampler = KeepLostOutcomes(sampler)

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
	revision := strings.ToLower(strings.TrimSpace(opts.Revision))
	if !exactRevision.MatchString(revision) {
		revision = ""
	}
	if revision != "" {
		attrs = append(attrs, attribute.String("vcs.ref.head.revision", revision))
	}
	// NewSchemaless avoids the schema-URL conflict that resource.Merge with
	// resource.Default() raises; service.name/environment are all a backend
	// needs to attribute the spans.
	res := resource.NewSchemaless(attrs...)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
	)
	if revision != "" {
		// Sentry's OTLP ingestion reads the release from the span attribute
		// sentry.release; it keeps resource attributes only as resource.*.
		tp.RegisterSpanProcessor(releaseStamp{release: attribute.String("sentry.release", revision)})
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if strings.TrimSpace(opts.Endpoint) == "" {
		return tp.Shutdown, nil
	}

	expOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(opts.Endpoint),
	}
	if netsec.FullLocal() {
		expOpts = append(expOpts, otlptracehttp.WithHTTPClient(netsec.NewClient(traceClientOptions())))
	}
	if len(opts.Headers) > 0 {
		expOpts = append(expOpts, otlptracehttp.WithHeaders(opts.Headers))
	}
	exp, err := otlptracehttp.New(ctx, expOpts...)
	if err != nil {
		return tp.Shutdown, fmt.Errorf("telemetry: create OTLP trace exporter; local tracing retained: %w", err)
	}
	tp.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(exp))

	return tp.Shutdown, nil
}

// releaseStamp sets the build's release attribute on every span at start.
type releaseStamp struct{ release attribute.KeyValue }

func (r releaseStamp) OnStart(_ context.Context, s sdktrace.ReadWriteSpan) {
	s.SetAttributes(r.release)
}
func (releaseStamp) OnEnd(sdktrace.ReadOnlySpan)      {}
func (releaseStamp) Shutdown(context.Context) error   { return nil }
func (releaseStamp) ForceFlush(context.Context) error { return nil }
