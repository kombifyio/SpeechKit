package stt

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

type recordSink struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (s *recordSink) Enabled(context.Context, slog.Level) bool { return true }
func (s *recordSink) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recs = append(s.recs, r)
	return nil
}
func (s *recordSink) WithAttrs([]slog.Attr) slog.Handler { return s }
func (s *recordSink) WithGroup(string) slog.Handler      { return s }

// A router with an injected Logger reports provider fallback there instead of
// the process-global logger.
func TestRouterRoutesFallbackWarningToInjectedLogger(t *testing.T) {
	sink := &recordSink{}
	failing := &mockProvider{name: "deepgram", failNext: true}
	healthy := &mockProvider{name: "assemblyai", text: "ok"}
	r := newTestRouter(nil, failing, healthy, StrategyCloudOnly)
	r.Logger = slog.New(sink)

	res, err := r.Route(context.Background(), []byte("audio"), 1, TranscribeOpts{})
	if err != nil || res == nil || res.Provider != "assemblyai" {
		t.Fatalf("Route = %+v, %v; want fallback to assemblyai", res, err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	var warned bool
	for _, rec := range sink.recs {
		warned = warned || rec.Level == slog.LevelWarn
	}
	if !warned {
		t.Fatal("fallback warning did not reach the injected logger")
	}
}
