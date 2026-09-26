//go:build (windows || darwin) && cgo

package capture

// #include <stdlib.h>
import "C"

import (
	"log/slog"
	"time"
	"unsafe"

	"github.com/gen2brain/malgo"
)

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
		Backend: platformMalgoBackend(),
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
				Backend:   platformMalgoBackend(),
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
