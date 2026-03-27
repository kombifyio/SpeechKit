package router

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/internal/stt"
)

// Strategy defines the routing strategy.
type Strategy string

const (
	StrategyDynamic   Strategy = "dynamic"
	StrategyLocalOnly Strategy = "local-only"
	StrategyCloudOnly Strategy = "cloud-only"

	internetCacheTTL = 10 * time.Second
)

// Router selects the best STTProvider based on audio length, availability, and config.
type Router struct {
	mu          sync.RWMutex
	local       stt.STTProvider
	vps         stt.STTProvider
	huggingFace stt.STTProvider

	Strategy             Strategy
	PreferLocalUnderSecs float64
	ParallelCloud        bool
	ReplaceOnBetter      bool

	internetOnline atomic.Bool
	internetAt     atomic.Int64 // UnixNano of last check
}

// SetLocal sets the local provider (thread-safe).
func (r *Router) SetLocal(p stt.STTProvider) {
	r.mu.Lock()
	r.local = p
	r.mu.Unlock()
}

// SetVPS sets the VPS provider (thread-safe).
func (r *Router) SetVPS(p stt.STTProvider) {
	r.mu.Lock()
	r.vps = p
	r.mu.Unlock()
}

// SetHuggingFace sets the HuggingFace provider (thread-safe).
func (r *Router) SetHuggingFace(p stt.STTProvider) {
	r.mu.Lock()
	r.huggingFace = p
	r.mu.Unlock()
}

// Local returns the current local provider.
func (r *Router) Local() stt.STTProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.local
}

// VPS returns the current VPS provider.
func (r *Router) VPS() stt.STTProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.vps
}

// HuggingFace returns the current HuggingFace provider.
func (r *Router) HuggingFace() stt.STTProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.huggingFace
}

// providers returns a snapshot of all three providers under one lock acquisition.
func (r *Router) providers() (local, vps, hf stt.STTProvider) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.local, r.vps, r.huggingFace
}

// Route selects the appropriate provider(s) and returns the transcription result.
func (r *Router) Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	switch r.Strategy {
	case StrategyLocalOnly:
		return r.transcribeLocal(ctx, audio, opts)
	case StrategyCloudOnly:
		return r.transcribeCloud(ctx, audio, opts)
	default:
		return r.transcribeDynamic(ctx, audio, audioDurationSecs, opts)
	}
}

func (r *Router) transcribeDynamic(ctx context.Context, audio []byte, durationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	local, vps, hf := r.providers()
	online := r.checkInternet()
	cloudAvailable := online && (vps != nil || hf != nil)

	// Case 1: No internet -- local only
	if !online {
		if local != nil {
			log.Println("No internet, using local provider")
			return local.Transcribe(ctx, audio, opts)
		}
		return nil, fmt.Errorf("no internet and local provider not ready")
	}

	// Case 2: Local ready and short audio -- use local, optionally parallel cloud
	if local != nil && durationSecs < r.PreferLocalUnderSecs {
		if r.ParallelCloud && cloudAvailable {
			return r.transcribeParallel(ctx, audio, opts)
		}
		result, err := local.Transcribe(ctx, audio, opts)
		if err == nil {
			return result, nil
		}
		log.Printf("local transcribe failed: %v", err)
	}

	// Case 3: No local or long audio -- prefer cloud
	if cloudAvailable {
		result, err := r.transcribeCloud(ctx, audio, opts)
		if err == nil {
			return result, nil
		}
		log.Printf("cloud transcribe failed: %v", err)
	}

	// Case 4: Fallback to local
	if local != nil {
		return local.Transcribe(ctx, audio, opts)
	}

	return nil, fmt.Errorf("no STT provider available")
}

// checkInternet returns cached connectivity status, refreshing if stale.
func (r *Router) checkInternet() bool {
	now := time.Now().UnixNano()
	lastCheck := r.internetAt.Load()
	if now-lastCheck < int64(internetCacheTTL) {
		return r.internetOnline.Load()
	}

	online := probeInternet()
	r.internetOnline.Store(online)
	r.internetAt.Store(now)
	return online
}

// probeInternet does a quick TCP check to detect connectivity.
func probeInternet() bool {
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// transcribeCloud tries VPS first, then HuggingFace. Attempts Transcribe directly
// without a separate Health check to avoid double round-trips in the hot path.
func (r *Router) transcribeCloud(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	_, vps, hf := r.providers()

	if vps != nil {
		result, err := vps.Transcribe(ctx, audio, opts)
		if err == nil {
			return result, nil
		}
		log.Printf("vps transcribe failed: %v", err)
	}

	if hf != nil {
		result, err := hf.Transcribe(ctx, audio, opts)
		if err == nil {
			return result, nil
		}
		log.Printf("huggingface transcribe failed: %v", err)
	}

	return nil, fmt.Errorf("no cloud provider available")
}

func (r *Router) transcribeLocal(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	local := r.Local()
	if local == nil {
		return nil, fmt.Errorf("local provider not configured")
	}
	return local.Transcribe(ctx, audio, opts)
}

// transcribeParallel sends to local and cloud simultaneously, returns first result.
// If ReplaceOnBetter is enabled, waits briefly for a second result.
func (r *Router) transcribeParallel(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	type resultOrError struct {
		result *stt.Result
		err    error
	}

	local := r.Local()
	results := make(chan resultOrError, 3)

	// Local
	if local != nil {
		go func() {
			result, err := local.Transcribe(ctx, audio, opts)
			results <- resultOrError{result, err}
		}()
	}

	// Cloud (VPS preferred, HF fallback)
	go func() {
		result, err := r.transcribeCloud(ctx, audio, opts)
		results <- resultOrError{result, err}
	}()

	// Wait for first successful result
	expectedResults := 2
	if local == nil {
		expectedResults = 1
	}

	var firstResult *stt.Result
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
	local, vps, hf := r.providers()
	var providers []string
	if local != nil {
		providers = append(providers, "local")
	}
	if vps != nil {
		providers = append(providers, "vps")
	}
	if hf != nil {
		providers = append(providers, "huggingface")
	}
	return providers
}
