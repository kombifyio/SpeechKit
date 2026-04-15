package audio

import (
	"errors"
	"fmt"
	"sync"
)

type Backend string

const (
	BackendAuto               Backend = "auto"
	BackendWindowsWASAPIMalgo Backend = "windows-wasapi-malgo"
	BackendWindowsWASAPINative Backend = "windows-wasapi-native"
)

var (
	ErrUnsupportedBackend = errors.New("unsupported audio backend")
	ErrBackendUnavailable = errors.New("audio backend unavailable in this build")
)

type EventType string

const (
	EventStarted EventType = "started"
	EventStopped EventType = "stopped"
	EventWarning EventType = "warning"
	EventError   EventType = "error"
)

type Event struct {
	Type    EventType
	Backend Backend
	Message string
	Err     error
}

type Config struct {
	Backend     Backend
	DeviceID    string
	SampleRate  int
	Channels    int
	FrameSizeMs int
	LatencyHint string
}

// Session records microphone PCM and exposes both level and live-audio callbacks.
type Session interface {
	Start() error
	Stop() ([]byte, error)
	IsRunning() bool
	Events() <-chan Event
	SetLevelHandler(func(float64))
	SetPCMHandler(func([]byte))
	Close() error
}

// Capturer is kept as an alias while the app migrates to the session terminology.
type Capturer = Session

type Factory func(Config) (Session, error)

var (
	registryMu sync.RWMutex
	registry   = map[Backend]Factory{}
)

func RegisterBackend(name Backend, factory Factory) error {
	if name == "" || name == BackendAuto {
		return fmt.Errorf("%w: invalid backend name %q", ErrUnsupportedBackend, name)
	}
	if factory == nil {
		return fmt.Errorf("%w: nil factory for %q", ErrUnsupportedBackend, name)
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		return fmt.Errorf("%w: backend %q already registered", ErrUnsupportedBackend, name)
	}
	registry[name] = factory
	return nil
}

func unregisterBackendForTest(name Backend) {
	registryMu.Lock()
	defer registryMu.Unlock()
	delete(registry, name)
}

func Open(cfg Config) (Session, error) {
	cfg = normalizeConfig(cfg)
	if cfg.Backend == "" {
		return nil, fmt.Errorf("%w: no default backend for this build", ErrBackendUnavailable)
	}

	registryMu.RLock()
	factory, ok := registry[cfg.Backend]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedBackend, cfg.Backend)
	}

	session, err := factory(cfg)
	if err != nil {
		if errors.Is(err, ErrUnsupportedBackend) || errors.Is(err, ErrBackendUnavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("init audio backend %q: %w", cfg.Backend, err)
	}

	return session, nil
}

func NewCapturer() (Capturer, error) {
	return Open(Config{})
}

func NewCapturerWithConfig(cfg Config) (Capturer, error) {
	return Open(cfg)
}

func normalizeConfig(cfg Config) Config {
	if cfg.Backend == "" || cfg.Backend == BackendAuto {
		cfg.Backend = defaultBackend()
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = SampleRate
	}
	if cfg.Channels <= 0 {
		cfg.Channels = Channels
	}
	if cfg.FrameSizeMs <= 0 {
		cfg.FrameSizeMs = 32
	}
	return cfg
}

func defaultBackend() Backend {
	return BackendWindowsWASAPIMalgo
}
