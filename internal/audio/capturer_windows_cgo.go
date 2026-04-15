//go:build windows && cgo

package audio

// #include <stdlib.h>
import "C"

import (
	"bytes"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/gen2brain/malgo"
)

const (
	// Pre-allocate for ~30s of audio to reduce GC pressure during recording.
	initialBufferSize = SampleRate * BytesPerSample * 30
)

func init() {
	if err := RegisterBackend(BackendWindowsWASAPIMalgo, newMalgoSession); err != nil {
		panic(err)
	}
}

// MalgoSession records audio via malgo using the Windows WASAPI backend.
type MalgoSession struct {
	cfg          Config
	ctx          *malgo.AllocatedContext
	device       *malgo.Device
	buffer       bytes.Buffer
	mu           sync.Mutex
	levelMu      sync.RWMutex
	levelHandler func(float64)
	pcmMu        sync.RWMutex
	pcmHandler   func([]byte)
	running      atomic.Bool
	events       chan Event
	eventsMu     sync.RWMutex
	eventsClosed bool
}

var _ Session = (*MalgoSession)(nil)

func newMalgoSession(cfg Config) (Session, error) {
	ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendWasapi}, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}

	s := &MalgoSession{
		cfg:    cfg,
		ctx:    ctx,
		events: make(chan Event, 8),
	}
	s.buffer.Grow(initialBufferSize)
	return s, nil
}

func (s *MalgoSession) Start() error {
	if s.running.Load() {
		return nil
	}

	s.mu.Lock()
	s.buffer.Reset()
	s.mu.Unlock()

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = uint32(s.cfg.Channels)
	deviceConfig.SampleRate = uint32(s.cfg.SampleRate)

	var releaseDeviceID func()
	if deviceID, ok, err := resolveCaptureDeviceID(s.cfg); err != nil {
		return err
	} else if ok {
		deviceIDPtr := deviceID.Pointer()
		deviceConfig.Capture.DeviceID = deviceIDPtr
		releaseDeviceID = func() {
			if deviceIDPtr != nil {
				C.free(unsafe.Pointer(deviceIDPtr))
			}
		}
	}

	onRecvFrames := func(outputSamples, inputSamples []byte, frameCount uint32) {
		s.mu.Lock()
		s.buffer.Write(inputSamples)
		s.mu.Unlock()

		level := PCMLevel(inputSamples)
		s.levelMu.RLock()
		levelHandler := s.levelHandler
		s.levelMu.RUnlock()
		if levelHandler != nil {
			levelHandler(level)
		}

		s.pcmMu.RLock()
		pcmHandler := s.pcmHandler
		s.pcmMu.RUnlock()
		if pcmHandler != nil && len(inputSamples) > 0 {
			// malgo reuses callback buffers, so forward a stable copy.
			pcmHandler(append([]byte(nil), inputSamples...))
		}
	}

	callbacks := malgo.DeviceCallbacks{
		Data: onRecvFrames,
		Stop: func() {
			s.emit(Event{
				Type:    EventStopped,
				Backend: BackendWindowsWASAPIMalgo,
				Message: "malgo device stopped",
			})
		},
	}
	device, err := malgo.InitDevice(s.ctx.Context, deviceConfig, callbacks)
	if err != nil {
		if releaseDeviceID != nil {
			releaseDeviceID()
		}
		return err
	}
	if releaseDeviceID != nil {
		defer releaseDeviceID()
	}

	if err := device.Start(); err != nil {
		device.Uninit()
		return err
	}

	s.device = device
	s.running.Store(true)
	s.emit(Event{
		Type:    EventStarted,
		Backend: BackendWindowsWASAPIMalgo,
		Message: "malgo capture started",
	})
	return nil
}

// Stop stops recording and returns the captured PCM data. Resets the buffer.
func (s *MalgoSession) Stop() ([]byte, error) {
	if !s.running.Load() {
		return nil, nil
	}
	s.running.Store(false)

	var stopErr error
	if s.device != nil {
		stopErr = s.device.Stop()
		s.device.Uninit()
		s.device = nil
	}

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

func (s *MalgoSession) Close() error {
	var closeErr error
	if s.running.Load() {
		_, closeErr = s.Stop()
	}
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
