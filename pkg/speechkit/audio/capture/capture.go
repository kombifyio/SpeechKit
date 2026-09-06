package capture

import (
	"errors"
	"fmt"
	"sync"

	audiopkg "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

type Backend string

const (
	BackendAuto                Backend = "auto"
	BackendWindowsWASAPIMalgo  Backend = "windows-wasapi-malgo"
	BackendWindowsWASAPINative Backend = "windows-wasapi-native"
)

type InputSource string

const (
	InputSourceMicrophone     InputSource = "microphone"
	InputSourceSystemLoopback InputSource = "system_loopback"
	InputSourceMicAndSystem   InputSource = "mic_and_system"
)

var (
	ErrUnsupportedBackend      = errors.New("unsupported audio backend")
	ErrBackendUnavailable      = errors.New("audio backend unavailable in this build")
	ErrUnsupportedSource       = errors.New("unsupported audio input source")
	ErrOutputDeviceUnavailable = errors.New("audio output device unavailable")
)

type EventType string

const (
	EventStarted EventType = "started"
	EventStopped EventType = "stopped"
	EventWarning EventType = "warning"
	EventError   EventType = "error"
	// EventOverrun signals that the frame dispatcher dropped captured
	// frames because the consumer (level/VAD handlers) could not keep
	// up. The authoritative full-capture buffer is unaffected — only
	// live level/segmentation frames were lost.
	EventOverrun EventType = "overrun"
	// EventStalled signals that the capture device stopped delivering
	// frames while it claims to be running (driver stall, device
	// starvation). Emitted once per stall episode.
	EventStalled EventType = "stalled"
)

type Event struct {
	Type    EventType
	Backend Backend
	Message string
	Err     error
	// Requested is set on an EventStopped that the session's own Stop caused.
	// Only a stop nobody asked for — unplugged device, exclusive-mode grab,
	// format change — is an interruption a host should report.
	Requested bool
}

type Config struct {
	Backend     Backend
	InputSource InputSource
	DeviceID    string
	// DeviceName is the human-readable name of the capture device. USB/UAC
	// devices can re-enumerate with a new WASAPI endpoint ID after a replug
	// or firmware update; when DeviceID no longer matches, the capturer
	// falls back to matching by this name.
	DeviceName     string
	OutputDeviceID string
	SampleRate     int
	Channels       int
	FrameSizeMs    int
	LatencyHint    string
	// CaptureThreadPriority selects the OS priority of the backend's
	// audio worker thread: "realtime" (default, TIME_CRITICAL — safe
	// because the callback only memcpys and enqueues) or "highest"
	// (malgo's own default). Ignored by backends without native thread
	// control.
	CaptureThreadPriority string
}

// Session records microphone PCM and exposes both level and live-audio callbacks.
type Session interface {
	Start() error
	Stop() ([]byte, error)
	IsRunning() bool
	Events() <-chan Event
	SetLevelHandler(func(float64))
	SetPCMHandler(func([]byte))
	// SetPooledPCMHandler installs the pool-aware variant of the PCM
	// callback. When set (non-nil), the capture backend leases the
	// per-frame buffer from this package's package-level FramePool
	// instead of allocating fresh, and invokes the handler with a
	// release closure. The handler MUST call release exactly once
	// before returning OR before retaining any reference to the
	// slice. Forgetting to release leaks one pool slot per frame but
	// does not corrupt data.
	//
	// When both SetPCMHandler and SetPooledPCMHandler are set, the
	// pool-aware variant wins — the legacy handler is not invoked
	// for that frame, so callers that adopt the pooled API should
	// also unset the legacy one to avoid surprise.
	//
	// Backends not yet wired to honour the pool MAY no-op this
	// setter; the legacy SetPCMHandler path remains the canonical
	// contract for all existing callers.
	SetPooledPCMHandler(PooledPCMHandler)
	Close() error
}

// PooledPCMHandler receives one captured PCM frame with explicit
// buffer-ownership semantics. The release closure MUST be invoked
// exactly once when the handler is done with buf — either before
// returning, or asynchronously once any retained reference is
// released. The buffer MUST NOT be read or written after release.
//
// See [FramePool] for the underlying lifecycle. The
// optimisation only matters for sustained capture (~33 callbacks/sec
// per session); short-lived recording paths can stay on the legacy
// SetPCMHandler API without ceremony.
//
// Declared as a type alias (not a defined type) so implementations of
// [Session] structurally satisfy interfaces declared outside this
// package (e.g. pkg/speechkit's PooledPCMRecorder) without importing
// this package.
type PooledPCMHandler = func(buf []byte, release func())

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
	if cfg.InputSource == "" {
		cfg.InputSource = InputSourceMicrophone
	}
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = audiopkg.SampleRate
	}
	if cfg.Channels <= 0 {
		cfg.Channels = audiopkg.Channels
	}
	if cfg.FrameSizeMs <= 0 {
		cfg.FrameSizeMs = 32
	}
	if cfg.CaptureThreadPriority == "" {
		cfg.CaptureThreadPriority = "realtime"
	}
	return cfg
}

func defaultBackend() Backend {
	return BackendWindowsWASAPIMalgo
}
