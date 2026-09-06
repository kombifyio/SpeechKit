//go:build windows && cgo

package capture

// #include <stdlib.h>
import "C"

import (
	"bytes"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

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

func init() {
	if err := RegisterBackend(BackendWindowsWASAPIMalgo, newMalgoSession); err != nil {
		panic(err)
	}
}

// MalgoSession records audio via malgo using the Windows WASAPI backend.
type MalgoSession struct {
	cfg              Config
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

func newMalgoSession(cfg Config) (Session, error) {
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
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendWasapi}, malgo.ContextConfig{ThreadPriority: threadPriority}, nil)
	if err != nil {
		return nil, err
	}

	s := &MalgoSession{
		cfg:    cfg,
		ctx:    ctx,
		events: make(chan Event, 8),
	}
	s.buffer.Grow(initialBufferSize)
	if cfg.InputSource != InputSourceSystemLoopback && (cfg.KeepDeviceWarm || strings.TrimSpace(cfg.DeviceID) != "") {
		go s.warmUp()
	}
	return s, nil
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

// acquireDevice returns the initialised device for the endpoint, reusing
// the one kept warm since the last recording when it was opened for the
// same endpoint. With replace set, a warm device for another endpoint is
// closed and replaced; without it (the warm-up goroutine) an existing
// device is left alone whatever it was opened for, because it may be
// recording right now.
func (s *MalgoSession) acquireDevice(deviceType malgo.DeviceType, deviceID malgo.DeviceID, haveDeviceID bool, replace bool) (*malgo.Device, bool, error) {
	key := deviceKey(deviceType, deviceID, haveDeviceID)
	s.deviceMu.Lock()
	defer s.deviceMu.Unlock()
	if s.device != nil {
		if s.deviceKey == key || !replace {
			return s.device, true, nil
		}
		s.device.Uninit()
		s.device = nil
		s.deviceKey = ""
	}
	device, err := s.initDevice(deviceType, deviceID, haveDeviceID)
	if err != nil {
		return nil, false, err
	}
	s.device = device
	s.deviceKey = key
	return device, false, nil
}

// releaseDevice closes the initialised device, warm or not.
func (s *MalgoSession) releaseDevice() {
	s.deviceMu.Lock()
	defer s.deviceMu.Unlock()
	if s.device != nil {
		s.device.Uninit()
		s.device = nil
		s.deviceKey = ""
	}
}

func (s *MalgoSession) Start() error {
	if s.running.Load() {
		return nil
	}

	s.mu.Lock()
	s.buffer.Reset()
	s.mu.Unlock()
	s.stopRequested.Store(false)

	startAt := time.Now()
	deviceType := malgo.Capture
	if s.cfg.InputSource == InputSourceSystemLoopback {
		if err := ensureLoopbackOutputDeviceAvailable(s.cfg); err != nil {
			return err
		}
		deviceType = malgo.Loopback
	}

	resolveStart := time.Now()
	deviceID, haveDeviceID, fromCache, err := s.resolveInputDeviceID(deviceType)
	if err != nil {
		return err
	}
	resolveDur := time.Since(resolveStart)

	// Arm the frame dispatcher before the device can invoke callbacks.
	s.startFrameDispatch()

	openStart := time.Now()
	device, reused, err := s.acquireDevice(deviceType, deviceID, haveDeviceID, true)
	if err != nil && fromCache {
		// The cached endpoint id may be stale (device unplugged or Windows
		// re-enumerated it). Fall back to one fresh enumeration and retry
		// before giving up — this is the slow path the cache normally skips.
		s.invalidateResolvedCaptureDevice()
		slog.Warn("capture start with cached device id failed; re-enumerating",
			"err", err)
		deviceID, haveDeviceID, _, err = s.resolveInputDeviceID(deviceType)
		if err == nil {
			device, reused, err = s.acquireDevice(deviceType, deviceID, haveDeviceID, true)
		}
	}
	if err != nil {
		s.stopFrameDispatch()
		return err
	}
	openDur := time.Since(openStart)

	runStart := time.Now()
	if err := device.Start(); err != nil && reused {
		// A device kept warm can go stale (endpoint removed, format changed):
		// open a fresh one once before giving up.
		slog.Warn("warm capture device failed to start; reopening", "err", err)
		s.releaseDevice()
		device, reused, err = s.acquireDevice(deviceType, deviceID, haveDeviceID, true)
		if err == nil {
			err = device.Start()
		}
	} else if err != nil {
		s.releaseDevice()
		s.stopFrameDispatch()
		return err
	}
	if err != nil {
		s.releaseDevice()
		s.stopFrameDispatch()
		return err
	}
	runDur := time.Since(runStart)

	s.running.Store(true)

	totalDur := time.Since(startAt)
	timingArgs := []any{
		"resolve_ms", resolveDur.Milliseconds(),
		"open_ms", openDur.Milliseconds(),
		"start_ms", runDur.Milliseconds(),
		"total_ms", totalDur.Milliseconds(),
		"device_warm", reused,
		"device_id_cached", fromCache,
		"specific_device", haveDeviceID,
	}
	if totalDur > 250*time.Millisecond {
		slog.Info("capture start timing (slow)", timingArgs...)
	} else {
		slog.Debug("capture start timing", timingArgs...)
	}

	s.emit(Event{
		Type:    EventStarted,
		Backend: BackendWindowsWASAPIMalgo,
		Message: "malgo capture started",
	})
	return nil
}

// resolveInputDeviceID returns the endpoint to open. Capture-device
// resolution is cached per session because a fresh WASAPI enumeration costs
// hundreds of milliseconds and Start runs on the hotkey hot path; the cache
// is warmed at construction and invalidated when opening the device fails.
func (s *MalgoSession) resolveInputDeviceID(deviceType malgo.DeviceType) (malgo.DeviceID, bool, bool, error) {
	if deviceType == malgo.Loopback {
		id, ok, err := resolveOutputDeviceID(Config{
			Backend:  s.cfg.Backend,
			DeviceID: s.cfg.OutputDeviceID,
		})
		return id, ok, false, err
	}

	s.resolveMu.Lock()
	cachedHex, cached := s.resolvedHex, s.resolvedHexValid
	s.resolveMu.Unlock()

	hexID := cachedHex
	if !cached {
		fresh, err := resolveCaptureDeviceHex(s.cfg)
		if err != nil {
			return malgo.DeviceID{}, false, false, err
		}
		hexID = fresh
		s.resolveMu.Lock()
		s.resolvedHex = fresh
		s.resolvedHexValid = true
		s.resolveMu.Unlock()
	}

	id, ok, err := deviceIDFromHexString(hexID)
	return id, ok, cached, err
}

func (s *MalgoSession) invalidateResolvedCaptureDevice() {
	s.resolveMu.Lock()
	s.resolvedHex = ""
	s.resolvedHexValid = false
	s.resolveMu.Unlock()
}

// warmResolvedCaptureDevice pre-resolves the configured capture device off
// the hotkey path so the first Start after construction hits the cache.
func (s *MalgoSession) warmResolvedCaptureDevice() {
	hexID, err := resolveCaptureDeviceHex(s.cfg)
	if err != nil {
		slog.Debug("capture device pre-resolve failed", "err", err)
		return
	}
	s.resolveMu.Lock()
	if !s.resolvedHexValid {
		s.resolvedHex = hexID
		s.resolvedHexValid = true
	}
	s.resolveMu.Unlock()
}

// startFrameDispatch arms the per-session frame channel plus its drain
// and watchdog goroutines and returns the channel for the device
// callback to feed. Must run before the device starts delivering
// callbacks.
func (s *MalgoSession) startFrameDispatch() chan []byte {
	frames := make(chan []byte, frameDispatchDepth)
	drainDone := make(chan struct{})
	watchdogDone := make(chan struct{})

	s.dispatchMu.Lock()
	s.frames = frames
	s.drainDone = drainDone
	s.watchdogDone = watchdogDone
	s.dispatchMu.Unlock()
	s.frameSink.Store(&frames)
	s.overruns.Store(0)
	s.lastFrameNano.Store(time.Now().UnixNano())

	go s.drainFrames(frames, drainDone)
	go s.watchCaptureStall(watchdogDone)
	return frames
}

// drainFrames runs level computation and the PCM handlers off the
// WASAPI callback thread. Pinned to its own OS thread at ABOVE_NORMAL
// priority so foreign NORMAL-priority load cannot starve live
// segmentation while the audio thread stays essentially idle.
func (s *MalgoSession) drainFrames(frames <-chan []byte, done chan<- struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := setCurrentThreadPriority(threadPriorityAboveNormal); err != nil {
		slog.Debug("audio drain thread priority raise failed", "err", err)
	}
	defer close(done)

	for buf := range frames {
		level := audiopkg.PCMLevel(buf)
		s.levelMu.RLock()
		levelHandler := s.levelHandler
		s.levelMu.RUnlock()
		if levelHandler != nil {
			levelHandler(level)
		}

		s.pcmMu.RLock()
		pcmHandler := s.pcmHandler
		pooledHandler := s.pooledPCMHandler
		s.pcmMu.RUnlock()
		switch {
		case pooledHandler != nil:
			// Pool-aware path: hand the pooled buffer straight to the
			// consumer with an explicit release (typically 26x less heap
			// per frame; see framepool_bench_test.go).
			released := false
			pooledHandler(buf, func() {
				if released {
					return
				}
				released = true
				Put(buf)
			})
		case pcmHandler != nil:
			// Legacy path: handlers own the slice they receive, so
			// forward a stable copy and recycle the pooled buffer.
			pcmHandler(append([]byte(nil), buf...))
			Put(buf)
		default:
			Put(buf)
		}
	}
}

// watchCaptureStall emits one EventStalled per episode when the device
// claims to be running but frames stopped arriving (driver stall,
// device starvation). Detection only — restart semantics are the
// host's decision.
func (s *MalgoSession) watchCaptureStall(done <-chan struct{}) {
	ticker := time.NewTicker(stallCheckInterval)
	defer ticker.Stop()
	stalled := false
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if !s.running.Load() {
				stalled = false
				continue
			}
			since := time.Since(time.Unix(0, s.lastFrameNano.Load()))
			if since < stallWarnAfter {
				stalled = false
				continue
			}
			if stalled {
				continue
			}
			stalled = true
			slog.Warn("audio capture stalled — device running but no frames arriving",
				"since_ms", since.Milliseconds())
			s.emit(Event{
				Type:    EventStalled,
				Backend: BackendWindowsWASAPIMalgo,
				Message: fmt.Sprintf("no audio frames for %dms while device is running", since.Milliseconds()),
			})
		}
	}
}

// enqueueFrame copies one captured chunk into a pooled buffer and
// enqueues it for the drain goroutine. Never blocks: on a full channel
// the frame is dropped and counted — full capture is unaffected because
// the callback already wrote it to the session buffer.
func (s *MalgoSession) enqueueFrame(frames chan<- []byte, inputSamples []byte) {
	s.lastFrameNano.Store(time.Now().UnixNano())

	buf := Get()
	buf = append(buf, inputSamples...)
	select {
	case frames <- buf:
	default:
		Put(buf)
		if n := s.overruns.Add(1); n == 1 || n%100 == 0 {
			slog.Warn("audio frame dispatcher overrun — dropping level/VAD frames (full capture unaffected)",
				"dropped_frames", n)
			s.emit(Event{
				Type:    EventOverrun,
				Backend: BackendWindowsWASAPIMalgo,
				Message: fmt.Sprintf("frame dispatcher overrun (%d dropped)", n),
			})
		}
	}
}

// stopFrameDispatch tears down the dispatcher armed by
// startFrameDispatch. Must run after the device stopped delivering
// callbacks; waits (bounded) for queued frames to finish draining so
// the segmenter has seen everything the callback enqueued.
func (s *MalgoSession) stopFrameDispatch() {
	// Detach the sink first so a callback that still runs cannot reach a
	// channel about to close.
	s.frameSink.Store(nil)
	s.dispatchMu.Lock()
	frames := s.frames
	drainDone := s.drainDone
	watchdogDone := s.watchdogDone
	s.frames = nil
	s.drainDone = nil
	s.watchdogDone = nil
	s.dispatchMu.Unlock()

	if watchdogDone != nil {
		close(watchdogDone)
	}
	if frames != nil {
		close(frames)
		select {
		case <-drainDone:
		case <-time.After(drainStopTimeout):
			slog.Warn("audio frame drain did not complete before stop timeout")
		}
	}
	if n := s.overruns.Load(); n > 0 {
		slog.Warn("audio frame dispatcher dropped frames this session (full capture unaffected)",
			"dropped_frames", n)
	}
}

// initDevice opens the malgo device without starting it. The callbacks read
// the current frame sink per call, so the device outlives any one recording.
func (s *MalgoSession) initDevice(deviceType malgo.DeviceType, deviceID malgo.DeviceID, haveDeviceID bool) (*malgo.Device, error) {
	deviceConfig := malgo.DefaultDeviceConfig(deviceType)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = uint32(s.cfg.Channels)
	deviceConfig.SampleRate = uint32(s.cfg.SampleRate)
	if s.cfg.FrameSizeMs > 0 {
		// Honour the configured frame size; previously this knob was
		// plumbed through Config but silently ignored by the malgo backend.
		deviceConfig.PeriodSizeInMilliseconds = uint32(s.cfg.FrameSizeMs)
	}

	var releaseDeviceID func()
	if haveDeviceID {
		deviceIDPtr := deviceID.Pointer()
		deviceConfig.Capture.DeviceID = deviceIDPtr
		releaseDeviceID = func() {
			if deviceIDPtr != nil {
				C.free(unsafe.Pointer(deviceIDPtr))
			}
		}
	}

	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		// Keep the WASAPI callback minimal: an overrun here means the
		// driver glitches audio at the hardware level. The authoritative
		// full-capture write stays synchronous (a memcpy into a
		// pre-grown buffer, contended only by Stop) so dictation audio
		// can never be lost to a slow consumer; everything else moves to
		// the drain goroutine. malgo reuses its chunk after the callback
		// returns, so the pooled copy is mandatory either way.
		s.mu.Lock()
		s.buffer.Write(inputSamples)
		s.mu.Unlock()

		if len(inputSamples) == 0 {
			return
		}
		if sink := s.frameSink.Load(); sink != nil {
			s.enqueueFrame(*sink, inputSamples)
		}
	}

	callbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
		Stop: func() {
			requested := s.stopRequested.Load()
			message := "malgo device stopped"
			if requested {
				message = "malgo device stopped (requested)"
			}
			s.emit(Event{
				Type:      EventStopped,
				Backend:   BackendWindowsWASAPIMalgo,
				Message:   message,
				Requested: requested,
			})
		},
	}
	device, err := malgo.InitDevice(s.ctx.Context, deviceConfig, callbacks)
	if err != nil {
		if releaseDeviceID != nil {
			releaseDeviceID()
		}
		return nil, err
	}
	if releaseDeviceID != nil {
		defer releaseDeviceID()
	}

	return device, nil
}

// Stop stops recording and returns the captured PCM data. Resets the buffer.
func (s *MalgoSession) Stop() ([]byte, error) {
	if !s.running.Load() {
		return nil, nil
	}
	s.running.Store(false)

	var stopErr error
	s.deviceMu.Lock()
	device := s.device
	s.deviceMu.Unlock()
	if device != nil {
		s.stopRequested.Store(true)
		stopErr = device.Stop()
		if !s.cfg.KeepDeviceWarm {
			s.releaseDevice()
		}
	}

	// The device no longer delivers callbacks; flush the dispatcher so
	// the segmenter has processed every enqueued frame before the full
	// capture is returned.
	s.stopFrameDispatch()

	s.mu.Lock()
	defer s.mu.Unlock()
	pcm := make([]byte, s.buffer.Len())
	copy(pcm, s.buffer.Bytes())
	s.buffer.Reset()
	return pcm, stopErr
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
