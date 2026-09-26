package wakeword

import (
	"errors"
	"fmt"
	"sync"
)

// Engine is a loaded keyword-spotting model. The root package ships no
// engine: an engine package registers its EngineFactory with RegisterEngine
// (pkg/speechkit/wakeword/sherpa does so on import) and NewDetector picks it
// up. Pipeline drives the Engine through EngineStream and owns every
// engine-neutral behaviour itself: PCM conversion, debounce, cooldown, pause
// and the reported detection probability.
type Engine interface {
	// NewStream opens one streaming decode session against the model.
	NewStream() (EngineStream, error)
	// Close releases the model. It must be safe to call more than once.
	Close() error
}

// EngineStream is one streaming keyword-spotting session over an Engine.
// Pipeline serialises every call on a stream, so implementations need no
// locking of their own.
type EngineStream interface {
	// AcceptWaveform appends mono float32 samples in [-1, 1] at SampleRate.
	AcceptWaveform(samples []float32)
	// IsReady reports whether enough audio is buffered for another Decode.
	IsReady() bool
	// Decode runs one decode step and returns the spotted keyword, or ""
	// when the step produced none.
	Decode() string
	// Reset clears the decoder state after a detection or on Pipeline.Reset.
	Reset()
	// Close releases the stream. It must be safe to call more than once.
	Close() error
}

// EngineFactory loads an Engine for cfg. When it runs, NewDetector has
// already checked that the model asset paths exist and materialised
// cfg.KeywordsFile as a BPE-tokenised keywords file.
type EngineFactory func(cfg DetectorConfig) (Engine, error)

var (
	engineMu    sync.RWMutex
	engines     = map[string]EngineFactory{}
	engineOrder []string
)

// RegisterEngine makes factory available to NewDetector under name. The
// first registered engine is the default; DetectorConfig.Engine selects
// another by name. An empty name, a nil factory, or a name registered twice
// is an error.
func RegisterEngine(name string, factory EngineFactory) error {
	if name == "" {
		return errors.New("wakeword: RegisterEngine: empty engine name")
	}
	if factory == nil {
		return fmt.Errorf("wakeword: RegisterEngine: nil factory for %q", name)
	}

	engineMu.Lock()
	defer engineMu.Unlock()
	if _, exists := engines[name]; exists {
		return fmt.Errorf("wakeword: RegisterEngine: engine %q already registered", name)
	}
	engines[name] = factory
	engineOrder = append(engineOrder, name)
	return nil
}

// lookupEngine resolves the factory for name, or the default (first
// registered) engine when name is empty.
func lookupEngine(name string) (EngineFactory, error) {
	engineMu.RLock()
	defer engineMu.RUnlock()
	if name == "" {
		if len(engineOrder) == 0 {
			return nil, ErrEngineUnavailable
		}
		return engines[engineOrder[0]], nil
	}
	factory, ok := engines[name]
	if !ok {
		return nil, fmt.Errorf("%w: engine %q", ErrEngineUnavailable, name)
	}
	return factory, nil
}

func unregisterEngineForTest(name string) {
	engineMu.Lock()
	defer engineMu.Unlock()
	delete(engines, name)
	for i, n := range engineOrder {
		if n == name {
			engineOrder = append(engineOrder[:i], engineOrder[i+1:]...)
			break
		}
	}
}
