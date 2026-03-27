package router

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

// mockProvider implements stt.STTProvider for testing.
type mockProvider struct {
	name     string
	text     string
	latency  time.Duration
	healthy  bool
	failNext bool
	called   int
}

func (m *mockProvider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	m.called++
	if m.failNext {
		return nil, fmt.Errorf("mock %s failure", m.name)
	}
	time.Sleep(m.latency)
	return &stt.Result{
		Text:     m.text,
		Provider: m.name,
		Language: opts.Language,
		Duration: m.latency,
	}, nil
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Health(ctx context.Context) error {
	if !m.healthy {
		return fmt.Errorf("mock %s unhealthy", m.name)
	}
	return nil
}

func newTestRouter(local, vps, hf stt.STTProvider, strategy Strategy) *Router {
	r := &Router{
		Strategy:             strategy,
		PreferLocalUnderSecs: 10,
	}
	if local != nil {
		r.SetLocal(local)
	}
	if vps != nil {
		r.SetVPS(vps)
	}
	if hf != nil {
		r.SetHuggingFace(hf)
	}
	r.internetOnline.Store(true)
	r.internetAt.Store(time.Now().UnixNano())
	return r
}

func TestRouteDynamic_LocalShortAudio(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local", text: "local result", healthy: true},
		nil,
		&mockProvider{name: "hf", text: "hf result", healthy: true},
		StrategyDynamic,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "local" {
		t.Errorf("expected local provider, got %s", result.Provider)
	}
}

func TestRouteDynamic_LongAudioUsesCloud(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local", text: "local", healthy: true},
		nil,
		&mockProvider{name: "hf", text: "hf result", healthy: true},
		StrategyDynamic,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 15.0, stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	// Long audio should prefer cloud (VPS or HF)
	if result.Provider == "local" {
		t.Error("long audio should not use local provider")
	}
}

func TestRouteDynamic_FallbackToLocal(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local", text: "local fallback", healthy: true},
		nil,
		&mockProvider{name: "hf", text: "hf", healthy: true, failNext: true},
		StrategyDynamic,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 15.0, stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "local" {
		t.Errorf("expected local fallback, got %s", result.Provider)
	}
}

func TestRouteDynamic_NoInternetUsesLocal(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local", text: "local offline", healthy: true},
		nil,
		&mockProvider{name: "hf", text: "hf", healthy: true},
		StrategyDynamic,
	)
	r.internetOnline.Store(false)
	r.internetAt.Store(time.Now().UnixNano())

	result, err := r.Route(context.Background(), []byte("audio"), 15.0, stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "local" {
		t.Errorf("expected local provider offline, got %s", result.Provider)
	}
}

func TestRouteLocalOnly(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local", text: "only local", healthy: true},
		nil, nil,
		StrategyLocalOnly,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "local" {
		t.Errorf("expected local, got %s", result.Provider)
	}
}

func TestRouteCloudOnly(t *testing.T) {
	r := newTestRouter(
		nil, nil,
		&mockProvider{name: "hf", text: "cloud only", healthy: true},
		StrategyCloudOnly,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "hf" {
		t.Errorf("expected hf, got %s", result.Provider)
	}
}

func TestRouteNoProviders(t *testing.T) {
	r := &Router{Strategy: StrategyDynamic}

	_, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{})
	if err == nil {
		t.Error("expected error with no providers")
	}
}

func TestAvailableProviders(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local"},
		nil,
		&mockProvider{name: "hf"},
		StrategyDynamic,
	)

	providers := r.AvailableProviders()
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}
}

func TestRouteParallel_FirstResultWins(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local", text: "local fast", healthy: true, latency: 10 * time.Millisecond},
		nil,
		&mockProvider{name: "hf", text: "hf slow", healthy: true, latency: 200 * time.Millisecond},
		StrategyDynamic,
	)
	r.ParallelCloud = true

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Route parallel: %v", err)
	}
	// Local should win because it's faster
	if result.Provider != "local" {
		t.Errorf("expected local (faster), got %s", result.Provider)
	}
}

func TestRouteVPSPreferredOverHF(t *testing.T) {
	r := newTestRouter(
		nil,
		&mockProvider{name: "vps", text: "vps result", healthy: true},
		&mockProvider{name: "hf", text: "hf result", healthy: true},
		StrategyCloudOnly,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "vps" {
		t.Errorf("VPS should be preferred over HF, got %s", result.Provider)
	}
}

func TestRouteVPSFallbackToHF(t *testing.T) {
	r := newTestRouter(
		nil,
		&mockProvider{name: "vps", text: "vps", healthy: false, failNext: true},
		&mockProvider{name: "hf", text: "hf fallback", healthy: true},
		StrategyCloudOnly,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "hf" {
		t.Errorf("expected HF fallback, got %s", result.Provider)
	}
}

func TestRouteCloudOnly_FailingHFReturnsError(t *testing.T) {
	hf := &mockProvider{name: "hf", text: "should fail", healthy: true, failNext: true}
	r := newTestRouter(nil, nil, hf, StrategyCloudOnly)

	_, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for failing hf provider")
	}
}

func TestRouteLocalOnly_NoLocal(t *testing.T) {
	r := &Router{Strategy: StrategyLocalOnly}

	_, err := r.Route(context.Background(), []byte("audio"), 5.0, stt.TranscribeOpts{})
	if err == nil {
		t.Error("expected error with no local provider")
	}
}

func TestAvailableProviders_AllThree(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local"},
		&mockProvider{name: "vps"},
		&mockProvider{name: "hf"},
		StrategyDynamic,
	)
	providers := r.AvailableProviders()
	if len(providers) != 3 {
		t.Errorf("expected 3 providers, got %d: %v", len(providers), providers)
	}
}

func TestAvailableProviders_None(t *testing.T) {
	r := &Router{}
	if len(r.AvailableProviders()) != 0 {
		t.Error("expected 0 providers")
	}
}
