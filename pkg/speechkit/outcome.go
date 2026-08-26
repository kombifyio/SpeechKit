package speechkit

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Outcome names for RecordOutcome. Keep these stable — backends and alerts
// key on the string, not on log message text.
const (
	OutcomeEmptyFinalTranscript = "empty_final_transcript"
	OutcomePCMQueueDrop         = "pcm_queue_drop"
	OutcomeAssistEmptySpeak     = "assist_empty_speak"
)

// RecordOutcome attaches a named framework result to the active span.
//
// This is the vendor-neutral error/outcome seam (kombify-SpeechKit-5te4 M2):
// callers keep using slog for operators, but user-visible failures also land
// on the trace so a configured OTLP backend can surface them. With no
// TracerProvider installed (the local-only default) this is a zero-cost no-op.
func RecordOutcome(ctx context.Context, name string, err error, attrs ...attribute.KeyValue) {
	if ctx == nil || strings.TrimSpace(name) == "" {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	eventAttrs := make([]attribute.KeyValue, 0, len(attrs)+1)
	eventAttrs = append(eventAttrs, attribute.String("speechkit.outcome", name))
	eventAttrs = append(eventAttrs, attrs...)
	span.AddEvent("speechkit.outcome", trace.WithAttributes(eventAttrs...))
	if err != nil {
		span.RecordError(err, trace.WithAttributes(eventAttrs...))
		span.SetStatus(codes.Error, name)
	}
}
