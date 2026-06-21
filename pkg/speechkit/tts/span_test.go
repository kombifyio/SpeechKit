package tts

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func spanByName(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func spanAttr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

func TestSynthesizeEmitsSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	r := NewRouter(StrategyCloudFirst, &mockProvider{name: "openai", kind: ProviderKindDirectProvider})
	if _, err := r.Synthesize(context.Background(), "hello", SynthesizeOpts{Format: "mp3"}); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	span := spanByName(sr.Ended(), "tts.synthesize")
	if span == nil {
		t.Fatal("no tts.synthesize span recorded")
	}
	if got := spanAttr(span, "speechkit.tts.provider"); got != "openai" {
		t.Errorf("provider attr = %q, want openai", got)
	}
	if got := spanAttr(span, "speechkit.tts.format"); got != "mp3" {
		t.Errorf("format attr = %q, want mp3", got)
	}
}
