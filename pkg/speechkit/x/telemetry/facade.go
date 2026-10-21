// Package telemetry records SpeechKit outcomes on the current OpenTelemetry span.
//
// Stability: Experimental — may change in any release.
package telemetry

import (
	"context"
	core "github.com/kombifyio/SpeechKit/pkg/speechkit/internal/telemetry"
)

const (
	OutcomeEmptyFinalTranscript = core.OutcomeEmptyFinalTranscript
	OutcomePCMQueueDrop         = core.OutcomePCMQueueDrop
	OutcomeAssistEmptySpeak     = core.OutcomeAssistEmptySpeak
	OutcomeAssistTTSFailed      = core.OutcomeAssistTTSFailed
)

// Attr is an outcome attribute shared with the kernel's instrumentation.
type Attr = core.Attr

func StringAttr(key, value string) Attr          { return core.StringAttr(key, value) }
func Int64Attr(key string, value int64) Attr     { return core.Int64Attr(key, value) }
func Float64Attr(key string, value float64) Attr { return core.Float64Attr(key, value) }
func BoolAttr(key string, value bool) Attr       { return core.BoolAttr(key, value) }
func RecordOutcome(ctx context.Context, name string, err error, attrs ...Attr) {
	core.RecordOutcome(ctx, name, err, attrs...)
}
