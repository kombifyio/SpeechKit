package auditlog

import (
	"context"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// STTProviderSelected is the stt.Router OnProviderSelected observer both
// hosts install: it records which STT provider served a request and under
// which routing strategy as an EventProviderSelected audit record.
func STTProviderSelected(ctx context.Context, providerName string, strategy stt.Strategy) {
	_ = AppendEvent(ctx, Record{
		Event: EventProviderSelected,
		Resource: map[string]any{
			"provider_name": providerName,
			"provider_kind": "stt",
			"strategy":      string(strategy),
		},
		Outcome: OutcomeSuccess,
	})
}
