package speechkit

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRecordOutcomeMarksSpanErrorOnFailure(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	ctx, span := tp.Tracer("test").Start(context.Background(), "dictation")
	RecordOutcome(ctx, OutcomeEmptyFinalTranscript, errors.New("empty"), attribute.String("provider", "deepgram"))
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want the dictation span", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Fatalf("status = %s, want Error so a backend can page a lost-words outcome", spans[0].Status.Code)
	}
	sawOutcome := false
	for _, ev := range spans[0].Events {
		if ev.Name == "speechkit.outcome" {
			sawOutcome = true
			break
		}
	}
	if !sawOutcome {
		t.Fatal("expected a speechkit.outcome event on the active span")
	}
}

func TestRecordOutcomeNoopWithoutRecordingSpan(t *testing.T) {
	RecordOutcome(context.Background(), OutcomeEmptyFinalTranscript, errors.New("empty"))
}
