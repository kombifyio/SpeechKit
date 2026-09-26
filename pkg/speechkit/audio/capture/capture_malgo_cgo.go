//go:build (windows || darwin) && cgo

package capture

import (
	"bytes"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gen2brain/malgo"

	audiopkg "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

const (
	// Pre-allocate for ~30s of audio to reduce GC pressure during recording.
	initialBufferSize = audiopkg.SampleRate * audiopkg.BytesPerSample * 30

	// frameDispatchDepth buffers ~2s of 32ms frames between the WASAPI
	// callback and the drain goroutine. Deep enough to absorb GC pauses
	// and scheduler hiccups under CPU load; overflow drops level/VAD
	// frames (counted), never full-capture audio.
	frameDispatchDepth = 64

	// stallCheckInterval / stallWarnAfter drive the capture watchdog:
	// when the device claims to be running but no frame arrived for
	// stallWarnAfter, one EventStalled per episode is emitted.
	stallCheckInterval = 2 * time.Second
	stallWarnAfter     = 3 * time.Second

	// drainStopTimeout bounds how long Stop waits for queued frames to
	// finish draining before abandoning them.
	drainStopTimeout = 2 * time.Second
)

// MalgoSession records audio via malgo on the platform backend named by
// platformMalgoBackend (WASAPI on Windows, CoreAudio on macOS).
type MalgoSession struct {
	cfg              Config
	pool             *FramePool
	ctx              *malgo.AllocatedContext
	device           *malgo.Device
	buffer           bytes.Buffer
	mu               sync.Mutex
	levelMu          sync.RWMutex
	levelHandler     func(float64)
	pcmMu            sync.RWMutex
	pcmHandler       func([]byte)
	pooledPCMHandler PooledPCMHandler
	running          atomic.Bool
	// stopRequested is raised by Stop before the device is stopped, so the
	// device's stop callback can tell a requested stop from an interruption.
	stopRequested atomic.Bool
	events        chan Event
	eventsMu      sync.RWMutex
	eventsClosed  bool

	// Frame dispatch: the WASAPI callback only copies each frame into a
	// pooled buffer and enqueues it; level computation and PCM handlers
	// run on a dedicated drain goroutine so slow consumers can never
	// overrun the audio thread. Guarded by dispatchMu; overruns and
	// lastFrameNano are atomics shared with the callback/watchdog.
	dispatchMu    sync.Mutex
	frames        chan []byte
	drainDone     chan struct{}
	watchdogDone  chan struct{}
	overruns      atomic.Uint64
	lastFrameNano atomic.Int64

	// resolvedHex caches the outcome of the expensive WASAPI capture-device
	// enumeration ("" = system default). Guarded by resolveMu; invalidated
	// when opening the cached endpoint fails.
	resolveMu        sync.Mutex
	resolvedHex      string
	resolvedHexValid bool

	// device is the initialised malgo device: running while a recording is
	// on, and — with cfg.KeepDeviceWarm — kept initialised but stopped
	// between recordings so the next Start skips WASAPI activation.
	// deviceKey names the endpoint and type it was opened for. Guarded by
	// deviceMu; the warm-up goroutine and Start both touch it.
	deviceMu  sync.Mutex
	deviceKey string
	// frameSink is the channel the device callback feeds, swapped by
	// startFrameDispatch and cleared by stopFrameDispatch, so one
	// long-lived device serves many recordings.
	frameSink atomic.Pointer[chan []byte]
}

// deviceKey identifies what a malgo device was opened for.
func deviceKey(deviceType malgo.DeviceType, deviceID malgo.DeviceID, haveDeviceID bool) string {
	if !haveDeviceID {
		return fmt.Sprintf("%d|default", deviceType)
	}
	return fmt.Sprintf("%d|%x", deviceType, deviceID)
}

var _ Session = (*MalgoSession)(nil)

// errSourceUnavailableOnPlatform reports an input source the platform seam
// cannot open in this build (see platformSupportsLoopback).
func errSourceUnavailableOnPlatform(source InputSource) error {
	return fmt.Errorf("%w: %q is not available on %s in this build (Meeting system audio lands in kombify-SpeechKit-mcos.16)", ErrUnsupportedSource, source, runtime.GOOS)
}

func newMalgoSession(cfg Config) (Session, error) {
	// Refuse system-audio sources the platform seam cannot serve before a
	// malgo context exists: miniaudio has no CoreAudio loopback, so on
	// macOS both land in kombify-SpeechKit-mcos.16.
	if !platformSupportsLoopback() {
		switch cfg.InputSource {
		case InputSourceSystemLoopback, InputSourceMicAndSystem:
			return nil, errSourceUnavailableOnPlatform(cfg.InputSource)
		}
	}
	if cfg.InputSource == InputSourceMicAndSystem {
		return nil, ErrUnsupportedSource
	}

	// TIME_CRITICAL is safe for this callback: it only memcpys into the
	// pre-grown capture buffer and enqueues a pooled frame (~1ms per
	// 32ms period), so it cannot starve the UI — but it survives being
	// scheduled against a fully loaded machine. malgo's own default is
	// THREAD_PRIORITY_HIGHEST ("highest" opt-out).
	threadPriority := malgo.ThreadPriorityRealtime
	if cfg.CaptureThreadPriority == "highest" {
		threadPriority = malgo.ThreadPriorityHighest
	}
	ctx, err := malgo.InitContext(platformMalgoContextBackends(), malgo.ContextConfig{ThreadPriority: threadPriority}, nil)
	if err != nil {
		return nil, err
	}

	pool := cfg.FramePool
	if pool == nil {
		pool = &DefaultFramePool
	}
	s := &MalgoSession{
		cfg:    cfg,
		pool:   pool,
		ctx:    ctx,
		events: make(chan Event, 8),
	}
	s.buffer.Grow(initialBufferSize)
	if cfg.InputSource != InputSourceSystemLoopback && (cfg.KeepDeviceWarm || strings.TrimSpace(cfg.DeviceID) != "") {
		go s.warmUp()
	}
	return s, nil
}

// framePool returns the pool this session leases frame buffers from:
// Config.FramePool as resolved by newMalgoSession, or DefaultFramePool for a
// session that was constructed directly (tests, embedders that build the
// struct themselves).
func (s *MalgoSession) framePool() *FramePool {
	if s.pool == nil {
		return &DefaultFramePool
	}
	return s.pool
}

// warmUp runs off the hotkey path right after construction: it resolves the
// configured capture device and, with KeepDeviceWarm, opens it so even the
// first recording after launch starts without WASAPI activation.
func (s *MalgoSession) warmUp() {
	if strings.TrimSpace(s.cfg.DeviceID) != "" {
		s.warmResolvedCaptureDevice()
	}
	if !s.cfg.KeepDeviceWarm {
		return
	}
	deviceID, haveDeviceID, _, err := s.resolveInputDeviceID(malgo.Capture)
	if err != nil {
		slog.Debug("capture device pre-open skipped: resolve failed", "err", err)
		return
	}
	openStart := time.Now()
	if _, reused, err := s.acquireDevice(malgo.Capture, deviceID, haveDeviceID, false); err != nil {
		slog.Debug("capture device pre-open failed", "err", err)
	} else if !reused {
		slog.Debug("capture device pre-opened", "open_ms", time.Since(openStart).Milliseconds())
	}
}

func (s *MalgoSession) IsRunning() bool {
	return s.running.Load()
}

func (s *MalgoSession) Events() <-chan Event {
	return s.events
}

func (s *MalgoSession) SetLevelHandler(handler func(float64)) {
	s.levelMu.Lock()
	defer s.levelMu.Unlock()
	s.levelHandler = handler
}

func (s *MalgoSession) SetPCMHandler(handler func([]byte)) {
	s.pcmMu.Lock()
	defer s.pcmMu.Unlock()
	s.pcmHandler = handler
}

// SetPooledPCMHandler installs the pool-aware variant of the PCM
// callback — see the Session interface contract for the release
// semantics. The malgo backend honours this on every captured frame;
// when both legacy and pooled handlers are non-nil, the pooled one
// wins and the legacy is skipped for that frame.
func (s *MalgoSession) SetPooledPCMHandler(handler PooledPCMHandler) {
	s.pcmMu.Lock()
	defer s.pcmMu.Unlock()
	s.pooledPCMHandler = handler
}

func (s *MalgoSession) Close() error {
	var closeErr error
	if s.running.Load() {
		_, closeErr = s.Stop()
	}
	// A device kept warm must go before its context.
	s.releaseDevice()
	if s.ctx != nil {
		if err := s.ctx.Uninit(); err != nil && closeErr == nil {
			closeErr = err
		}
		s.ctx.Free()
		s.ctx = nil
	}
	s.eventsMu.Lock()
	if !s.eventsClosed {
		close(s.events)
		s.eventsClosed = true
	}
	s.eventsMu.Unlock()
	return closeErr
}

func (s *MalgoSession) emit(event Event) {
	s.eventsMu.RLock()
	if s.eventsClosed {
		s.eventsMu.RUnlock()
		return
	}
	select {
	case s.events <- event:
	default:
	}
	s.eventsMu.RUnlock()
}
