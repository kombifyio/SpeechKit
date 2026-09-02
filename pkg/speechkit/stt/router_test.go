package stt

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// mockProvider implements STTProvider for testing.
type mockProvider struct {
	name     string
	text     string
	latency  time.Duration
	healthy  bool
	failNext bool
	called   int
}

func (m *mockProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	m.called++
	if m.failNext {
		return nil, fmt.Errorf("mock %s failure", m.name)
	}
	time.Sleep(m.latency)
	return &Result{
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

type mockStreamingProvider struct {
	mockProvider
	started bool
}

func (m *mockStreamingProvider) StartSpeakerStream(_ context.Context, _ speaker.Options, _ speaker.AudioFormat) (speaker.SpeakerStream, error) {
	m.started = true
	return &mockSpeakerStream{}, nil
}

type mockDictationStreamingProvider struct {
	mockProvider
	started bool
	opts    speechkit.DictationStreamOptions
}

func (m *mockDictationStreamingProvider) StartDictationStream(_ context.Context, opts speechkit.DictationStreamOptions, _ speaker.AudioFormat) (speechkit.DictationStream, error) {
	m.started = true
	m.opts = opts
	return mockDictationStream{}, nil
}

type mockSpeakerStream struct{}

func (mockSpeakerStream) SendAudio(context.Context, []byte) error { return nil }
func (mockSpeakerStream) EndAudio(context.Context) error          { return nil }
func (mockSpeakerStream) Receive(context.Context) (*speaker.SpeakerFrame, error) {
	return nil, context.Canceled
}
func (mockSpeakerStream) Close() error { return nil }

type mockDictationStream struct{}

func (mockDictationStream) SendPCM(context.Context, []byte) error { return nil }
func (mockDictationStream) Finalize(context.Context) error        { return nil }
func (mockDictationStream) Receive(context.Context) (speechkit.DictationStreamEvent, error) {
	return speechkit.DictationStreamEvent{}, context.Canceled
}
func (mockDictationStream) Close() error { return nil }

func newTestRouter(local, vps, hf STTProvider, strategy Strategy) *Router {
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

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{Language: "de"})
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

	result, err := r.Route(context.Background(), []byte("audio"), 15.0, TranscribeOpts{Language: "de"})
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

	result, err := r.Route(context.Background(), []byte("audio"), 15.0, TranscribeOpts{Language: "de"})
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

	result, err := r.Route(context.Background(), []byte("audio"), 15.0, TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "local" {
		t.Errorf("expected local provider offline, got %s", result.Provider)
	}
}

func TestRouteDynamic_OfflineProbeStillAllowsCloud(t *testing.T) {
	r := newTestRouter(
		nil,
		nil,
		&mockProvider{name: "hf", text: "hf fallback", healthy: true},
		StrategyDynamic,
	)
	r.internetOnline.Store(false)
	r.internetAt.Store(time.Now().UnixNano())

	result, err := r.Route(context.Background(), []byte("audio"), 12.0, TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "hf" {
		t.Errorf("expected cloud fallback despite offline probe, got %s", result.Provider)
	}
}

func TestRouteLocalOnly(t *testing.T) {
	r := newTestRouter(
		&mockProvider{name: "local", text: "only local", healthy: true},
		nil, nil,
		StrategyLocalOnly,
	)

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
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

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "hf" {
		t.Errorf("expected hf, got %s", result.Provider)
	}
}

func TestPreferCloudMakesSelectedProviderPrimary(t *testing.T) {
	r := newTestRouter(
		nil,
		&mockProvider{name: "vps", text: "vps result", healthy: true},
		&mockProvider{name: "hf", text: "hf result", healthy: true},
		StrategyCloudOnly,
	)
	r.PreferCloud("groq", &mockProvider{name: "groq", text: "groq result", healthy: true})

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if result.Provider != "groq" {
		t.Fatalf("expected groq to become the primary provider, got %s", result.Provider)
	}
}

func TestRouteNoProviders(t *testing.T) {
	r := &Router{Strategy: StrategyDynamic}

	_, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
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

func TestStartSpeakerStreamSelectsMatchingProviderProfile(t *testing.T) {
	deepgram := &mockStreamingProvider{mockProvider: mockProvider{name: "deepgram"}}
	assembly := &mockStreamingProvider{mockProvider: mockProvider{name: "assemblyai"}}
	r := newTestRouter(nil, deepgram, assembly, StrategyCloudOnly)

	stream, err := r.StartSpeakerStream(context.Background(),
		speaker.Options{Diarization: true, ProviderProfileID: "speaker.assemblyai.diarization"},
		speaker.AudioFormat{},
	)
	if err != nil {
		t.Fatalf("StartSpeakerStream: %v", err)
	}
	if stream == nil {
		t.Fatal("expected stream")
	}
	if !assembly.started {
		t.Fatal("expected assemblyai provider to start")
	}
	if deepgram.started {
		t.Fatal("deepgram should not start when assemblyai profile is requested")
	}
}

func TestStartDictationStreamPrefersCloudStreamingProvider(t *testing.T) {
	local := &mockDictationStreamingProvider{mockProvider: mockProvider{name: "local"}}
	cloud := &mockDictationStreamingProvider{mockProvider: mockProvider{name: "deepgram"}}
	r := newTestRouter(local, nil, cloud, StrategyDynamic)

	stream, err := r.StartDictationStream(context.Background(),
		speechkit.DictationStreamOptions{Language: "de", InterimResults: true},
		speaker.AudioFormat{},
	)
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	if stream == nil {
		t.Fatal("expected stream")
	}
	if !cloud.started {
		t.Fatal("expected cloud streaming provider to start")
	}
	if local.started {
		t.Fatal("local stream should not start when cloud streaming provider is available")
	}
	if got, want := cloud.opts.Language, "de"; got != want {
		t.Fatalf("stream language = %q, want %q", got, want)
	}
}

func TestStartDictationStreamPrioritizesRequestedProviderProfile(t *testing.T) {
	deepgram := &mockDictationStreamingProvider{mockProvider: mockProvider{name: "deepgram"}}
	assembly := &mockDictationStreamingProvider{mockProvider: mockProvider{name: "assemblyai"}}
	r := newTestRouter(nil, deepgram, assembly, StrategyCloudOnly)

	stream, err := r.StartDictationStream(context.Background(),
		speechkit.DictationStreamOptions{ProviderProfileID: "stt.assemblyai.universal", Language: "en"},
		speaker.AudioFormat{},
	)
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	if stream == nil {
		t.Fatal("expected stream")
	}
	if !assembly.started {
		t.Fatal("expected assemblyai provider to start")
	}
	if deepgram.started {
		t.Fatal("deepgram should not start when assemblyai profile is requested")
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

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{Language: "de"})
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

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
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

	result, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
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

	_, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error for failing hf provider")
	}
}

func TestRouteLocalOnly_NoLocal(t *testing.T) {
	r := &Router{Strategy: StrategyLocalOnly}

	_, err := r.Route(context.Background(), []byte("audio"), 5.0, TranscribeOpts{})
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
				TranscribeOpts{ProviderProfileID: tt.preference})
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
		TranscribeOpts{ProviderProfileID: "deepgram"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if res.Provider != "alpha" {
		t.Fatalf("provider = %q, want fallback alpha after preferred provider failed", res.Provider)
	}
}

func TestRouterOnProviderSelectedIsPerInstance(t *testing.T) {
	var seenA, seenB []string
	a := newTestRouter(&mockProvider{name: "local-a", text: "a", healthy: true}, nil, nil, StrategyLocalOnly)
	a.OnProviderSelected = func(_ context.Context, name string, _ Strategy) { seenA = append(seenA, name) }
	b := newTestRouter(&mockProvider{name: "local-b", text: "b", healthy: true}, nil, nil, StrategyLocalOnly)
	b.OnProviderSelected = func(_ context.Context, name string, _ Strategy) { seenB = append(seenB, name) }

	if _, err := a.Route(context.Background(), []byte("pcm"), 1.0, TranscribeOpts{}); err != nil {
		t.Fatalf("Route a: %v", err)
	}
	if _, err := b.Route(context.Background(), []byte("pcm"), 1.0, TranscribeOpts{}); err != nil {
		t.Fatalf("Route b: %v", err)
	}

	if len(seenA) != 1 || seenA[0] != "local-a" {
		t.Fatalf("observer a saw %v, want only local-a", seenA)
	}
	if len(seenB) != 1 || seenB[0] != "local-b" {
		t.Fatalf("observer b saw %v, want only local-b", seenB)
	}
}
