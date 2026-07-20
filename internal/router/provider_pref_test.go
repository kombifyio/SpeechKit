package router

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

// Batch-path counterpart to the streaming prioritization: a per-request
// provider preference (TranscribeOpts.ProviderProfileID) reorders the cloud
// candidate list without ever hard-failing an unsatisfiable preference.
func TestRouteCloudOnlyHonorsProviderProfilePreference(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		want       string
	}{
		{name: "empty preference keeps configured order", preference: "", want: "alpha"},
		{name: "bare provider name moves match to front", preference: "deepgram", want: "deepgram"},
		{name: "full profile id resolves to its provider", preference: "stt.deepgram.nova-3", want: "deepgram"},
		{name: "unknown preference falls back to configured order", preference: "stt.unknown.model", want: "alpha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Router{Strategy: StrategyCloudOnly}
			r.AddCloud(&mockProvider{name: "alpha", text: "from alpha", healthy: true})
			r.AddCloud(&mockProvider{name: "deepgram", text: "from deepgram", healthy: true})

			res, err := r.Route(context.Background(), []byte("pcm"), 1.0,
				stt.TranscribeOpts{ProviderProfileID: tt.preference})
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if res.Provider != tt.want {
				t.Fatalf("provider = %q, want %q (preference %q)", res.Provider, tt.want, tt.preference)
			}
		})
	}
}

func TestRouteCloudOnlyPreferredProviderFailureFallsBack(t *testing.T) {
	r := &Router{Strategy: StrategyCloudOnly}
	r.AddCloud(&mockProvider{name: "alpha", text: "from alpha", healthy: true})
	r.AddCloud(&mockProvider{name: "deepgram", failNext: true, healthy: true})

	res, err := r.Route(context.Background(), []byte("pcm"), 1.0,
		stt.TranscribeOpts{ProviderProfileID: "deepgram"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if res.Provider != "alpha" {
		t.Fatalf("provider = %q, want fallback alpha after preferred provider failed", res.Provider)
	}
}
