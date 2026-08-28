package speechkit

import (
	"context"
	"fmt"
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

// Attr is one key/value pair attached to a recorded outcome. It exists so
// RecordOutcome does not put OpenTelemetry in an embedder's own signatures:
// a host records outcomes with SpeechKit's own vocabulary, and whether a
// tracing backend is installed stays SpeechKit's business.
type Attr struct {
	Key   string
	Value any
}

// StringAttr, Int64Attr, Float64Attr and BoolAttr build an [Attr] of the
// matching type. They read better at a call site than a struct literal and
// keep the value's type explicit.
func StringAttr(key, value string) Attr      { return Attr{Key: key, Value: value} }
func Int64Attr(key string, v int64) Attr     { return Attr{Key: key, Value: v} }
func Float64Attr(key string, v float64) Attr { return Attr{Key: key, Value: v} }
func BoolAttr(key string, v bool) Attr       { return Attr{Key: key, Value: v} }

func (a Attr) keyValue() attribute.KeyValue {
	switch v := a.Value.(type) {
	case string:
		return attribute.String(a.Key, v)
	case int:
		return attribute.Int(a.Key, v)
	case int64:
		return attribute.Int64(a.Key, v)
	case float64:
		return attribute.Float64(a.Key, v)
	case bool:
		return attribute.Bool(a.Key, v)
	default:
		return attribute.String(a.Key, strings.TrimSpace(fmt.Sprint(v)))
	}
}

// RecordOutcome attaches a named framework result to the active span.
//
// This is the vendor-neutral error/outcome seam: callers keep using slog for
// operators, but user-visible failures also land on the trace so a configured
// OTLP backend can surface them. With no TracerProvider installed (the
// local-only default) this is a zero-cost no-op.
func RecordOutcome(ctx context.Context, name string, err error, attrs ...Attr) {
	if ctx == nil || strings.TrimSpace(name) == "" {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	eventAttrs := make([]attribute.KeyValue, 0, len(attrs)+1)
	eventAttrs = append(eventAttrs, attribute.String("speechkit.outcome", name))
	for _, attr := range attrs {
		eventAttrs = append(eventAttrs, attr.keyValue())
	}
	span.AddEvent("speechkit.outcome", trace.WithAttributes(eventAttrs...))
	if err != nil {
		span.RecordError(err, trace.WithAttributes(eventAttrs...))
		span.SetStatus(codes.Error, name)
	}
}
