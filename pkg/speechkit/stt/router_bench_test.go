package stt

import (
	"context"
	"testing"
	"time"
)

// benchProvider is an in-memory provider that answers instantly, so the
// benchmarks measure routing and selection, not transcription.
type benchProvider struct {
	name   string
	result *Result
}

func newBenchProvider(name string) *benchProvider {
	return &benchProvider{name: name, result: &Result{Text: "hello", Provider: name}}
}

func (p *benchProvider) Transcribe(context.Context, []byte, TranscribeOpts) (*Result, error) {
	return p.result, nil
}
func (p *benchProvider) Name() string                 { return p.name }
func (p *benchProvider) Health(context.Context) error { return nil }

func benchRouter(strategy Strategy) *Router {
	r := &Router{Strategy: strategy, PreferLocalUnderSecs: 10}
	r.SetLocal(newBenchProvider("local"))
	r.SetCloudProviders([]STTProvider{
		newBenchProvider("deepgram"),
		newBenchProvider("assemblyai"),
		newBenchProvider("openai"),
	})
	return r
}

// BenchmarkRouterRoute measures the per-request provider selection cost for
// each strategy with fake providers and no network. The dynamic cases keep
// the connectivity cache primed so no probe runs.
func BenchmarkRouterRoute(b *testing.B) {
	audio := make([]byte, 32000)
	ctx := context.Background()
	cases := []struct {
		name     string
		strategy Strategy
		secs     float64
		opts     TranscribeOpts
	}{
		{"local-only", StrategyLocalOnly, 1, TranscribeOpts{}},
		{"cloud-only", StrategyCloudOnly, 1, TranscribeOpts{}},
		{"cloud-only-profile", StrategyCloudOnly, 1, TranscribeOpts{ProviderProfileID: "stt.openai.whisper"}},
		{"dynamic-short", StrategyDynamic, 1, TranscribeOpts{}},
		{"dynamic-long", StrategyDynamic, 60, TranscribeOpts{}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			r := benchRouter(tc.strategy)
			b.ReportAllocs()
			for b.Loop() {
				// Keep the connectivity cache fresh so a long run never
				// triggers a real probe.
				r.internetOnline.Store(true)
				r.internetAt.Store(time.Now().UnixNano())
				if _, err := r.Route(ctx, audio, tc.secs, tc.opts); err != nil {
					b.Fatalf("Route: %v", err)
				}
			}
		})
	}
}
