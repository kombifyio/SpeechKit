// router.go implements the STT routing layer. It picks the right provider
// (local, cloud, or direct) for a transcription request based on the
// configured Strategy (Dynamic, LocalOnly, CloudOnly) and current
// connectivity / readiness.
//
// The router is platform-neutral and is consumed by both the Device-Target
// (Wails client) and the Server-Target. TTS has a parallel routing layer in
// pkg/speechkit/tts; this file is STT-only. It was promoted from
// internal/router, which remains as a compatibility alias shim.
package stt

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// sttTracer instruments STT routing. When no OpenTelemetry provider is
// installed, otel.Tracer returns a no-op, so spans cost nothing.
var sttTracer = otel.Tracer("github.com/kombifyio/SpeechKit/pkg/speechkit/stt")

// providerSelectedObserverFn is the callback type installed via
// SetProviderSelectedObserver.
type providerSelectedObserverFn = func(ctx context.Context, providerName string, strategy Strategy)

var providerSelectedObserver atomic.Value // providerSelectedObserverFn

// SetProviderSelectedObserver installs a host callback invoked after every
// successful routed transcription with the winning provider's name and the
// active strategy. Hosts use it to record audit events (the reference app
// wires it to its audit log in internal/router); the framework itself stays
// free of host logging dependencies. Passing nil removes the observer.
func SetProviderSelectedObserver(fn func(ctx context.Context, providerName string, strategy Strategy)) {
	providerSelectedObserver.Store(providerSelectedObserverFn(fn))
}

// emitProviderSelected notifies the host observer on successful
// transcription. Observer failures must never abort a user-facing
// transcription, so the callback has no error return.
func emitProviderSelected(ctx context.Context, providerName string, strategy Strategy) {
	if fn, _ := providerSelectedObserver.Load().(providerSelectedObserverFn); fn != nil {
		fn(ctx, providerName, strategy)
	}
}

// Strategy defines the routing strategy.
type Strategy string

const (
	StrategyDynamic   Strategy = "dynamic"
	StrategyLocalOnly Strategy = "local-only"
	StrategyCloudOnly Strategy = "cloud-only"

	internetCacheTTL = 60 * time.Second
)

// Router selects the best STTProvider based on audio length, availability, and config.
type Router struct {
	mu    sync.RWMutex
	local STTProvider
	cloud []STTProvider // ordered cloud providers (tried in sequence)

	Strategy             Strategy
	PreferLocalUnderSecs float64
	ParallelCloud        bool
	ReplaceOnBetter      bool
	// ConnectivityProbe is the TCP address used to test internet connectivity.
	// Defaults to "1.1.1.1:443" when empty.
	ConnectivityProbe string

	internetOnline atomic.Bool
	internetAt     atomic.Int64 // UnixNano of last check
}

// SetLocal sets the local provider (thread-safe).
func (r *Router) SetLocal(p STTProvider) {
	r.mu.Lock()
	r.local = p
	r.mu.Unlock()
}

// AddCloud appends a cloud provider to the ordered list (thread-safe).
func (r *Router) AddCloud(p STTProvider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	r.cloud = append(r.cloud, p)
	r.mu.Unlock()
}

// SetCloudProviders replaces the ordered cloud provider list.
func (r *Router) SetCloudProviders(providers []STTProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(providers) == 0 {
		r.cloud = nil
		return
	}

	next := make([]STTProvider, 0, len(providers))
	for _, provider := range providers {
		if provider != nil {
			next = append(next, provider)
		}
	}
	r.cloud = next
}

// SetCloud replaces a cloud provider by name, or appends if not found.
// Pass nil to remove the provider with that name.
func (r *Router) SetCloud(name string, p STTProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i, existing := range r.cloud {
		if existing.Name() == name {
			if p == nil {
				r.cloud = append(r.cloud[:i], r.cloud[i+1:]...)
			} else {
				r.cloud[i] = p
			}
			return
		}
	}
	if p != nil {
		r.cloud = append(r.cloud, p)
	}
}

// PreferCloud sets/replaces a cloud provider and moves it to the front so it
// becomes the next cloud provider used by routing, while keeping remaining
// providers as fallbacks.
func (r *Router) PreferCloud(name string, p STTProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	next := make([]STTProvider, 0, len(r.cloud)+1)
	if p != nil {
		next = append(next, p)
	}
	for _, existing := range r.cloud {
		if existing.Name() == name {
			continue
		}
		next = append(next, existing)
	}
	r.cloud = next
}

// SetVPS sets/replaces the VPS cloud provider (backward-compatible convenience).
func (r *Router) SetVPS(p STTProvider) {
	r.SetCloud("vps", p)
}

// SetHuggingFace sets/replaces the HuggingFace cloud provider (backward-compatible convenience).
func (r *Router) SetHuggingFace(p STTProvider) {
	r.SetCloud("huggingface", p)
}

// Local returns the current local provider.
func (r *Router) Local() STTProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.local
}

// Cloud returns a cloud provider by name, or nil if not found.
func (r *Router) Cloud(name string) STTProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.cloud {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// VPS returns the VPS cloud provider (backward-compatible convenience).
func (r *Router) VPS() STTProvider {
	return r.Cloud("vps")
}

// HuggingFace returns the HuggingFace cloud provider (backward-compatible convenience).
func (r *Router) HuggingFace() STTProvider {
	return r.Cloud("huggingface")
}

// snapshot returns a copy of local + cloud providers under one lock.
func (r *Router) snapshot() (local STTProvider, cloud []STTProvider) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cloud = make([]STTProvider, len(r.cloud))
	copy(cloud, r.cloud)
	return r.local, cloud
}

// Providers returns a snapshot of every configured provider: the local
// provider first (when set), then the cloud providers in routing order.
// Hosts use it to register health probes without re-deriving the provider
// set from config.
func (r *Router) Providers() []STTProvider {
	local, cloud := r.snapshot()
	out := make([]STTProvider, 0, len(cloud)+1)
	if local != nil {
		out = append(out, local)
	}
	return append(out, cloud...)
}

// Route selects the appropriate provider(s) and returns the transcription result.
func (r *Router) Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts TranscribeOpts) (res *Result, err error) {
	ctx, span := sttTracer.Start(ctx, "stt.transcribe", trace.WithAttributes(
		attribute.String("speechkit.stt.strategy", string(r.Strategy)),
		attribute.String("speechkit.stt.language", opts.Language),
	))
	defer func() {
		switch {
		case err != nil:
			span.RecordError(err)
			span.SetStatus(codes.Error, "transcribe failed")
		case res != nil:
			span.SetAttributes(
				attribute.String("speechkit.stt.provider", res.Provider),
				attribute.String("speechkit.stt.model", res.Model),
			)
		}
		span.End()
	}()

	switch r.Strategy {
	case StrategyLocalOnly:
		return r.transcribeLocal(ctx, audio, opts)
	case StrategyCloudOnly:
		return r.transcribeCloud(ctx, audio, opts)
	default:
		return r.transcribeDynamic(ctx, audio, audioDurationSecs, opts)
	}
}

// StartSpeakerStream selects the first configured provider that can perform
// realtime speaker attribution. The stream is an add-on path and never changes
// the normal STT routing decision for Dictation/Assist.
func (r *Router) StartSpeakerStream(ctx context.Context, opts speaker.Options, format speaker.AudioFormat) (speaker.SpeakerStream, error) {
	candidates := r.streamingCandidates(opts)
	var lastErr error
	for _, p := range candidates {
		streamer, ok := p.(speaker.StreamingProvider)
		if !ok {
			continue
		}
		stream, err := streamer.StartSpeakerStream(ctx, opts, format)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		slog.Warn("speaker streaming provider failed", "provider", p.Name(), "err", err)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("no speaker streaming provider available: %w", lastErr)
	}
	return nil, fmt.Errorf("no speaker streaming provider available")
}

// StartDictationStream selects the first configured provider that can perform
// provider-native realtime dictation. It never changes batch routing; callers
// choose this path explicitly per recording session.
func (r *Router) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	candidates := prioritizeProviderProfile(r.dictationStreamingCandidates(), opts.ProviderProfileID)
	var lastErr error
	for _, p := range candidates {
		streamer, ok := p.(speechkit.DictationStreamProvider)
		if !ok {
			continue
		}
		stream, err := streamer.StartDictationStream(ctx, opts, format)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		slog.Warn("dictation streaming provider failed", "provider", p.Name(), "err", err)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("no dictation streaming provider available: %w", lastErr)
	}
	return nil, fmt.Errorf("no dictation streaming provider available")
}

func (r *Router) transcribeDynamic(ctx context.Context, audio []byte, durationSecs float64, opts TranscribeOpts) (*Result, error) {
	local, cloud := r.snapshot()
	online := r.checkInternet(ctx)
	cloudAvailable := len(cloud) > 0

	// Case 1: Internet probe failed. Try local first, but still allow cloud as fallback
	// because strict egress policies can block the probe target while provider APIs are reachable.
	if !online {
		slog.Info("internet probe unavailable; trying providers with local preference")
		if local != nil {
			result, err := local.Transcribe(ctx, audio, opts.ForProvider(local.Name()))
			if err == nil {
				return result, nil
			}
			slog.Warn("local transcribe failed", "err", err)
		}
		if cloudAvailable {
			result, err := r.transcribeCloud(ctx, audio, opts)
			if err == nil {
				return result, nil
			}
			slog.Warn("cloud transcribe failed after offline probe", "err", err)
		}
		return nil, fmt.Errorf("no STT provider available")
	}

	// Case 2: Local ready and short audio -- use local, optionally parallel cloud
	if local != nil && durationSecs < r.PreferLocalUnderSecs {
		if r.ParallelCloud && cloudAvailable {
			return r.transcribeParallel(ctx, audio, opts)
		}
		result, err := local.Transcribe(ctx, audio, opts.ForProvider(local.Name()))
		if err == nil {
			return result, nil
		}
		slog.Warn("local transcribe failed", "err", err)
	}

	// Case 3: No local or long audio -- prefer cloud
	if cloudAvailable {
		result, err := r.transcribeCloud(ctx, audio, opts)
		if err == nil {
			return result, nil
		}
		slog.Warn("cloud transcribe failed", "err", err)
	}

	// Case 4: Fallback to local via transcribeLocal so the provider-selected
	// observer fires.
	if local != nil {
		slog.Warn("cloud providers unavailable; falling back to local STT")
		return r.transcribeLocal(ctx, audio, opts)
	}

	return nil, fmt.Errorf("no STT provider available")
}

func (r *Router) streamingCandidates(opts speaker.Options) []STTProvider {
	local, cloud := r.snapshot()
	var candidates []STTProvider
	switch r.Strategy {
	case StrategyLocalOnly:
		if local != nil {
			candidates = append(candidates, local)
		}
	case StrategyCloudOnly:
		candidates = append(candidates, cloud...)
	default:
		candidates = append(candidates, cloud...)
		if local != nil {
			candidates = append(candidates, local)
		}
	}
	return prioritizeSpeakerProfile(candidates, opts.ProviderProfileID)
}

// HasDictationStreaming reports whether at least one configured provider
// (honoring the routing strategy) can serve provider-native realtime
// dictation. Server surfaces use this for capability discovery so clients can
// fall back to batch transcription without a doomed stream attempt.
func (r *Router) HasDictationStreaming() bool {
	for _, p := range r.dictationStreamingCandidates() {
		if _, ok := p.(speechkit.DictationStreamProvider); ok {
			return true
		}
	}
	return false
}

func (r *Router) dictationStreamingCandidates() []STTProvider {
	local, cloud := r.snapshot()
	var candidates []STTProvider
	switch r.Strategy {
	case StrategyLocalOnly:
		if local != nil {
			candidates = append(candidates, local)
		}
	case StrategyCloudOnly:
		candidates = append(candidates, cloud...)
	default:
		candidates = append(candidates, cloud...)
		if local != nil {
			candidates = append(candidates, local)
		}
	}
	return candidates
}

func prioritizeSpeakerProfile(candidates []STTProvider, profileID string) []STTProvider {
	return prioritizeProviderProfile(candidates, profileID)
}

func prioritizeProviderProfile(candidates []STTProvider, profileID string) []STTProvider {
	want := providerNameFromProfileID(profileID)
	if want == "" || len(candidates) < 2 {
		return candidates
	}
	out := make([]STTProvider, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.EqualFold(candidate.Name(), want) {
			out = append(out, candidate)
		}
	}
	for _, candidate := range candidates {
		if !strings.EqualFold(candidate.Name(), want) {
			out = append(out, candidate)
		}
	}
	return out
}

func providerNameFromProfileID(profileID string) string {
	provider := speechkit.NormalizeProviderID(profileID)
	if strings.Contains(provider, ".") {
		return ""
	}
	return provider
}

// checkInternet returns cached connectivity status, refreshing if stale.
func (r *Router) checkInternet(ctx context.Context) bool {
	now := time.Now().UnixNano()
	lastCheck := r.internetAt.Load()
	if lastCheck != 0 && now-lastCheck < int64(internetCacheTTL) {
		return r.internetOnline.Load()
	}

	// First check ever: probe synchronously (short timeout) to establish a
	// baseline. Every later refresh runs in the BACKGROUND and returns the last
	// known value immediately — the probe used to run inline before every cloud
	// transcribe, so a slow or egress-filtered probe target added up to its full
	// timeout to the user's perceived dictation latency on every utterance more
	// than internetCacheTTL apart. The CAS keeps the background refresh
	// single-flight.
	if lastCheck == 0 {
		online := r.probeInternet(ctx)
		r.internetOnline.Store(online)
		r.internetAt.Store(now)
		return online
	}
	if r.internetAt.CompareAndSwap(lastCheck, now) {
		go func() {
			r.internetOnline.Store(r.probeInternet(context.WithoutCancel(ctx)))
		}()
	}
	return r.internetOnline.Load()
}

// probeInternet does a quick TCP check to detect connectivity.
// Uses ConnectivityProbe address, defaulting to "1.1.1.1:443".
func (r *Router) probeInternet(ctx context.Context) bool {
	addr := r.ConnectivityProbe
	if addr == "" {
		addr = "1.1.1.1:443"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// transcribeCloud tries cloud providers in order. Attempts Transcribe directly
// without a separate Health check to avoid double round-trips in the hot path.
// A per-request provider preference (opts.ProviderProfileID — profile ID or
// bare provider name) moves the matching provider to the front; the remaining
// providers stay as fallbacks, so an unsatisfiable preference degrades to the
// configured order instead of failing the request.
func (r *Router) transcribeCloud(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	_, cloud := r.snapshot()
	cloud = prioritizeProviderProfile(cloud, opts.ProviderProfileID)

	for _, p := range cloud {
		result, err := p.Transcribe(ctx, audio, opts.ForProvider(p.Name()))
		if err == nil {
			emitProviderSelected(ctx, p.Name(), r.Strategy)
			return result, nil
		}
		slog.Warn("provider transcribe failed", "provider", p.Name(), "err", err)
	}

	return nil, fmt.Errorf("no cloud provider available")
}

func (r *Router) transcribeLocal(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	local := r.Local()
	if local == nil {
		return nil, fmt.Errorf("local provider not configured")
	}
	result, err := local.Transcribe(ctx, audio, opts.ForProvider(local.Name()))
	if err == nil {
		emitProviderSelected(ctx, local.Name(), r.Strategy)
	}
	return result, err
}

// transcribeParallel sends to local and cloud simultaneously, returns first result.
// If ReplaceOnBetter is enabled, waits briefly for a second result.
func (r *Router) transcribeParallel(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	type resultOrError struct {
		result *Result
		err    error
	}

	local := r.Local()
	results := make(chan resultOrError, 3)

	// Local
	if local != nil {
		go func() {
			result, err := local.Transcribe(ctx, audio, opts.ForProvider(local.Name()))
			if err == nil {
				emitProviderSelected(ctx, local.Name(), r.Strategy)
			}
			results <- resultOrError{result, err}
		}()
	}

	// Cloud (ordered fallback)
	go func() {
		result, err := r.transcribeCloud(ctx, audio, opts)
		results <- resultOrError{result, err}
	}()

	// Wait for first successful result
	expectedResults := 2
	if local == nil {
		expectedResults = 1
	}

	var firstResult *Result
	for i := 0; i < expectedResults; i++ {
		select {
		case res := <-results:
			if res.err == nil && firstResult == nil {
				firstResult = res.result
				if !r.ReplaceOnBetter {
					return firstResult, nil
				}
			}
		case <-time.After(15 * time.Second):
			if firstResult != nil {
				return firstResult, nil
			}
			return nil, fmt.Errorf("all providers timed out")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if firstResult == nil {
		return nil, fmt.Errorf("all providers failed")
	}
	return firstResult, nil
}

// AvailableProviders returns the names of configured providers.
func (r *Router) AvailableProviders() []string {
	local, cloud := r.snapshot()
	var names []string
	if local != nil {
		names = append(names, "local")
	}
	for _, p := range cloud {
		names = append(names, p.Name())
	}
	return names
}
