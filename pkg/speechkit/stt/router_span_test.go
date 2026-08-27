package stt

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func spanAttr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

// TestRouteSpans covers the success and error STT spans in one test: the global
// OpenTelemetry provider can only be installed once per test binary, so both
// cases share a single recorder.
func TestRouteSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	okRouter := newTestRouter(&mockProvider{name: "whispercpp", text: "hi", healthy: true}, nil, nil, StrategyLocalOnly)
	if _, err := okRouter.Route(context.Background(), []byte("audio"), 1.0, TranscribeOpts{Language: "en"}); err != nil {
		t.Fatalf("Route (success): %v", err)
	}

	errRouter := newTestRouter(&mockProvider{name: "local", healthy: true, failNext: true}, nil, nil, StrategyLocalOnly)
	if _, err := errRouter.Route(context.Background(), []byte("audio"), 1.0, TranscribeOpts{}); err == nil {
		t.Fatal("Route (error): expected a transcription error")
	}

	var sawSuccess, sawError bool
	for _, s := range sr.Ended() {
		if s.Name() != "stt.transcribe" {
			continue
		}
		if spanAttr(s, "speechkit.stt.provider") == "whispercpp" {
			sawSuccess = true
			if got := spanAttr(s, "speechkit.stt.language"); got != "en" {
				t.Errorf("success span language attr = %q, want en", got)
			}
		}
		if s.Status().Code.String() == "Error" {
			sawError = true
		}
	}
	if !sawSuccess {
		t.Error("no successful stt.transcribe span with provider=whispercpp recorded")
	}
	if !sawError {
		t.Error("no stt.transcribe span with Error status recorded")
	}
}
