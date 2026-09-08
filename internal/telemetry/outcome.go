package telemetry

// outcome.go is the error half of this package. tracing.go answers "where do
// spans go"; this answers "how does a failure become something a backend can
// alert on".
//
// Why it exists: before this, a failure ended in slog and nowhere else. A span
// that merely finishes says nothing about whether the user got what they asked
// for, so no backend could tell a dictation that worked from one that lost the
// user's words. Alerting needs that distinction to exist in the data.
//
// It stays vendor-neutral for the same reason the rest of the package does. An
// outcome is a named event with a severity and typed attributes on an OTel
// span; whether that reaches Sentry, a collector or nothing at all is the
// endpoint the caller configured in tracing.go. With no endpoint the global
// TracerProvider is OTel's no-op, every call here is a few nil checks, and the
// documented local-only path emits nothing — which outcome_test.go asserts
// rather than assumes.

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// outcomeTracer names the instrumentation scope, so a backend can separate
// outcomes from the kernel's ordinary spans without parsing names.
const outcomeTracer = "github.com/kombifyio/SpeechKit/internal/telemetry/outcome"

// OutcomeSeverity says how much the user lost, which is the only question
// alerting actually asks. It is deliberately not a log level: "warn" and
// "error" describe how loud a line is, not whether someone's words survived.
type OutcomeSeverity string

const (
	// OutcomeOK is the thing working. Recorded so a rate can be computed;
	// nothing should ever page on it.
	OutcomeOK OutcomeSeverity = "ok"
	// OutcomeDegraded is a caveat the user can act on and recover from — the
	// text exists somewhere, even if not where they wanted it.
	OutcomeDegraded OutcomeSeverity = "degraded"
	// OutcomeLost is what should page: the user spoke and the words reached
	// neither the target application nor the history. Nothing is recoverable.
	OutcomeLost OutcomeSeverity = "lost"
)

// Attribute keys. Fixed names so a dashboard or alert rule can be written once
// and keep working; a typo in a call site would otherwise create a second,
// silently empty series.
const (
	AttrOutcomeSeverity = "speechkit.outcome.severity"
	AttrOutcomeName     = "speechkit.outcome.name"
)

// ReportOutcome records a named outcome against the trace in ctx.
//
// It attaches to the active span when there is one, so an outcome raised while
// serving a request stays attached to that request. With no active span — the
// desktop finalizes a dictation on its own goroutine, not inside a request —
// it opens a short span of its own, because an event with no span reaches no
// backend at all.
//
// OutcomeLost additionally sets the span status to Error, which is what makes
// it visible to a backend's error view and to an alert rule, rather than being
// one more successful span with an unusual attribute.
//
// Attributes must stay free of transcript text, audio, window titles and
// clipboard contents; the privacy invariant in AGENTS.md is binding and this
// path is exported to a third party by definition. Pass counts, states and
// enumerations, never content.
func ReportOutcome(ctx context.Context, name string, severity OutcomeSeverity, attrs ...attribute.KeyValue) {
	if name == "" {
		return
	}
	all := make([]attribute.KeyValue, 0, len(attrs)+2)
	all = append(all,
		attribute.String(AttrOutcomeName, name),
		attribute.String(AttrOutcomeSeverity, string(severity)),
	)
	all = append(all, attrs...)

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		// No live span to hang this on — either there is none, or the one there
		// is was not sampled. Open one so the outcome is exportable; under a
		// no-op provider this is still free. The attributes go in at Start so
		// the sampler can read the severity and refuse to drop a loss.
		var created trace.Span
		_, created = otel.Tracer(outcomeTracer).Start(ctx, name, trace.WithAttributes(all...))
		defer created.End()
		span = created
	}

	span.AddEvent(name, trace.WithAttributes(all...))
	span.SetAttributes(all...)
	if severity == OutcomeLost {
		span.SetStatus(codes.Error, name)
	}
}

// KeepLostOutcomes wraps a sampler so an outcome that says the user lost their
// words is never dropped, whatever the head sampling ratio is.
//
// This exists because of a real conflict between two correct settings.
// Production samples at 0.2, which is right for ordinary traffic and wrong for
// the one event that should page: at that ratio four out of five losses would
// never leave the machine, and the alert built on them would under-report by
// design while looking healthy.
//
// It reads the severity from the attributes passed at span creation, not from
// the span name, so adding an outcome does not mean remembering to register it
// here.
func KeepLostOutcomes(base sdktrace.Sampler) sdktrace.Sampler {
	return keepLostOutcomes{base: base}
}

type keepLostOutcomes struct{ base sdktrace.Sampler }

func (s keepLostOutcomes) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	for _, kv := range p.Attributes {
		if string(kv.Key) == AttrOutcomeSeverity && kv.Value.AsString() == string(OutcomeLost) {
			return sdktrace.SamplingResult{
				Decision:   sdktrace.RecordAndSample,
				Tracestate: trace.SpanContextFromContext(p.ParentContext).TraceState(),
			}
		}
	}
	return s.base.ShouldSample(p)
}

func (s keepLostOutcomes) Description() string {
	return "KeepLostOutcomes{" + s.base.Description() + "}"
}
