package capture

import (
	"errors"
	"fmt"
	"sync"

	audiopkg "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

// Backend names a registered capture implementation. Backends register
// themselves with [RegisterBackend]; [Open] resolves [BackendAuto] to the
// platform default.
type Backend string

// Built-in backend names. Which of them a binary can actually open depends
// on the target and on cgo: builds without a native backend answer every
// Open with [ErrBackendUnavailable].
const (
	// BackendAuto selects the platform default backend at Open time.
	BackendAuto Backend = "auto"
	// BackendWindowsWASAPIMalgo is the Windows capture backend: a malgo
	// (miniaudio) session on WASAPI, supporting microphone and loopback.
	BackendWindowsWASAPIMalgo Backend = "windows-wasapi-malgo"
	// BackendWindowsWASAPINative is reserved for a direct WASAPI backend;
	// no factory is registered for it in this build.
	BackendWindowsWASAPINative Backend = "windows-wasapi-native"
	// BackendDarwinCoreAudioMalgo is the macOS capture backend: the same
	// malgo session as BackendWindowsWASAPIMalgo, opened on CoreAudio.
	// System loopback is not available on it until the process-tap slice
	// (kombify-SpeechKit-mcos.16).
	BackendDarwinCoreAudioMalgo Backend = "darwin-coreaudio-malgo"
)

// InputSource selects what a session records. The zero value means
// [InputSourceMicrophone].
type InputSource string

// Input sources. A backend that cannot serve a source rejects Open with
// [ErrUnsupportedSource] instead of silently recording something else.
const (
	// InputSourceMicrophone records the selected (or default) capture device.
	InputSourceMicrophone InputSource = "microphone"
	// InputSourceSystemLoopback records what the selected output device
	// plays (WASAPI loopback); unavailable on macOS in this build.
	InputSourceSystemLoopback InputSource = "system_loopback"
	// InputSourceMicAndSystem is the mixed source Meeting Mode asks for;
	// no backend serves it yet, so Open returns ErrUnsupportedSource.
	InputSourceMicAndSystem InputSource = "mic_and_system"
)

// Sentinel errors returned by [Open], [ListCaptureDevices] and
// [ListOutputDevices]; callers match them with errors.Is.
var (
	// ErrUnsupportedBackend means the requested Backend has no factory.
	ErrUnsupportedBackend = errors.New("unsupported audio backend")
	// ErrBackendUnavailable means this build has no native backend at all
	// (no cgo, or an unsupported platform).
	ErrBackendUnavailable = errors.New("audio backend unavailable in this build")
	// ErrUnsupportedSource means the backend cannot record the InputSource.
	ErrUnsupportedSource = errors.New("unsupported audio input source")
	// ErrOutputDeviceUnavailable means the loopback output device could not
	// be found or opened.
	ErrOutputDeviceUnavailable = errors.New("audio output device unavailable")
)

// EventType classifies the lifecycle and health events a [Session] emits
// on [Session.Events].
type EventType string

// Event types. Started/Stopped bracket the device stream; Warning and
// Error carry a backend diagnostic; Overrun and Stalled are health signals.
const (
	// EventStarted is emitted once the device stream delivers audio.
	EventStarted EventType = "started"
	// EventStopped is emitted when the device stream ends; see
	// [Event.Requested] to tell a Stop from an interruption.
	EventStopped EventType = "stopped"
	// EventWarning carries a non-fatal backend diagnostic in Message or Err.
	EventWarning EventType = "warning"
	// EventError carries a backend failure in Err; the session may be unusable.
	EventError EventType = "error"
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

// Event is one lifecycle or health notification from a [Session].
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

// Config selects the backend, input source, device and framing of a
// capture session. Zero fields take the defaults described per field;
// [Open] fills them in before handing the config to the backend.
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
	// KeepDeviceWarm keeps the capture device initialised between
	// recordings and opens it ahead of the first one, so Start only has to
	// start a stream that already exists. Opening a WASAPI capture device
	// was measured at about 800 ms per recording (2026-09-05), which is
	// the delay between the hotkey and the first captured word. An
	// initialised device that is not started holds no stream and uses no
	// CPU. Ignored by backends that open nothing.
	KeepDeviceWarm bool
	// OnDeviceRebound, when set, is called after DeviceID was not found but
	// the device was recovered via DeviceName (USB/UAC re-enumeration). The
	// host can persist newID so the next start matches by ID again. It is
	// invoked from the backend's device-resolution path, so it must return
	// promptly and must not call back into the session.
	OnDeviceRebound func(oldID, newID, name string)
	// FramePool is the buffer pool the session leases per-frame buffers
	// from for [Session.SetPooledPCMHandler] and its internal frame
	// dispatch. nil selects [DefaultFramePool], which is shared by every
	// session in the process; hosts that run several capture sessions
	// with different frame sizes, or that want isolated pool statistics,
	// pass their own.
	FramePool *FramePool
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
	// per-frame buffer from the session's [FramePool] ([Config.FramePool],
	// [DefaultFramePool] when unset) instead of allocating fresh, and
	// invokes the handler with a release closure. The handler MUST call release exactly once
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

// Factory constructs a [Session] for a normalised [Config]; backends
// register one per [Backend] name via [RegisterBackend].
type Factory func(Config) (Session, error)

var (
	registryMu sync.RWMutex
	registry   = map[Backend]Factory{}
)

// RegisterBackend makes factory available under name. It rejects an empty
// or [BackendAuto] name, a nil factory, and a name that is already
// registered, all with [ErrUnsupportedBackend]. Native backends register
// themselves at init; hosts register their own to replace or add one.
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

// Open normalises cfg (defaults for backend, source, 16 kHz mono, 32 ms
// frames, realtime thread priority) and constructs a session from the
// registered backend. It returns [ErrBackendUnavailable] when the build
// has no default backend, [ErrUnsupportedBackend] for an unknown name,
// and wraps any other backend failure.
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

// NewCapturer opens a session with the default [Config].
func NewCapturer() (Capturer, error) {
	return Open(Config{})
}

// NewCapturerWithConfig is [Open] under the older capturer name.
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
