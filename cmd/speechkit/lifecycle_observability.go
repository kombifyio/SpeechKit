package main

import (
	"context"
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/lifecycle"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	desktopLifecycleMetricsOnce sync.Once
	desktopLifecycleTransitions metric.Int64Counter
	desktopLifecycleDepRefs     metric.Int64Histogram
)

func (b *desktopLifecycleBridge) recordTransitionObservability(ctx context.Context, ev lifecycle.TransitionEvent) {
	if b == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("speechkit.mode", string(ev.Mode)),
		attribute.String("speechkit.lifecycle.from", string(ev.From)),
		attribute.String("speechkit.lifecycle.to", string(ev.To)),
	}
	if ev.Err != nil {
		attrs = append(attrs, attribute.String("error.message", ev.Err.Error()))
	}

	tracer := otel.Tracer("github.com/kombifyio/SpeechKit/cmd/speechkit/lifecycle")
	spanCtx, span := tracer.Start(ctx, "speechkit.lifecycle.transition")
	span.SetAttributes(attrs...)
	if ev.Err != nil {
		span.RecordError(ev.Err)
	}
	span.End()

	desktopLifecycleMetricsOnce.Do(func() {
		meter := otel.Meter("github.com/kombifyio/SpeechKit/cmd/speechkit/lifecycle")
		desktopLifecycleTransitions, _ = meter.Int64Counter("speechkit.lifecycle.transitions")
		desktopLifecycleDepRefs, _ = meter.Int64Histogram("speechkit.lifecycle.shared_dep.refs")
	})
	if desktopLifecycleTransitions != nil {
		desktopLifecycleTransitions.Add(spanCtx, 1, metric.WithAttributes(attrs...))
	}
	if desktopLifecycleDepRefs == nil {
		return
	}
	for _, dep := range b.Snapshot().SharedDeps {
		desktopLifecycleDepRefs.Record(spanCtx, int64(dep.Refs), metric.WithAttributes(
			attribute.String("speechkit.shared_dep", string(dep.Key)),
		))
	}
}
