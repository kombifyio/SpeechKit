//go:build windows && cgo

package audio

/*
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
)

func init() {
	streamPlayerFactory = newMalgoStreamPlayer
}

type malgoStreamPlayer struct {
	deviceID string

	mu     sync.Mutex
	ctx    *malgo.AllocatedContext
	device *malgo.Device
	pipe   *streamPipe
	active bool
	cancel context.CancelFunc
	doneCh chan struct{}
}

func newMalgoStreamPlayer(outputDeviceID string) (streamPlaybackBackend, error) {
	return &malgoStreamPlayer{deviceID: strings.TrimSpace(outputDeviceID)}, nil
}

func (sp *malgoStreamPlayer) Start(ctx context.Context) {
	sp.StopAndDrain()
	if ctx == nil {
		return
	}

	playCtx, cancel := context.WithCancel(ctx)
	pipe := newStreamPipe()

	backends, err := malgoBackendsForConfig(Config{Backend: BackendWindowsWASAPIMalgo})
	if err != nil {
		cancel()
		slog.Error("voice agent output backend", "err", err)
		return
	}
	malgoCtx, err := malgo.InitContext(backends, malgo.ContextConfig{}, nil)
	if err != nil {
		cancel()
		slog.Error("voice agent output context", "err", err)
		return
	}

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	deviceConfig.Playback.Format = malgo.FormatS16
	deviceConfig.Playback.Channels = uint32(voiceAgentOutputChannels)
	deviceConfig.SampleRate = uint32(voiceAgentOutputSampleRate)

	var releaseDeviceID func()
	if deviceID, ok, err := resolveOutputDeviceID(Config{
		Backend:  BackendWindowsWASAPIMalgo,
		DeviceID: sp.deviceID,
	}); err != nil {
		slog.Warn("voice agent output device", "device_id", sp.deviceID, "err", err)
	} else if ok {
		deviceIDPtr := deviceID.Pointer()
		deviceConfig.Playback.DeviceID = deviceIDPtr
		releaseDeviceID = func() {
			if deviceIDPtr != nil {
				C.free(unsafe.Pointer(deviceIDPtr))
			}
		}
	}

	callbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, _ []byte, _ uint32) {
			for i := range outputSamples {
				outputSamples[i] = 0
			}

			sp.mu.Lock()
			active := sp.active
			currentPipe := sp.pipe
			sp.mu.Unlock()

			if active && currentPipe != nil {
				currentPipe.ReadAvailable(outputSamples)
			}
		},
	}

	device, err := malgo.InitDevice(malgoCtx.Context, deviceConfig, callbacks)
	if releaseDeviceID != nil {
		releaseDeviceID()
	}
	if err != nil {
		cancel()
		_ = malgoCtx.Uninit()
		malgoCtx.Free()
		slog.Error("voice agent output device init", "err", err)
		return
	}
	if err := device.Start(); err != nil {
		cancel()
		device.Uninit()
		_ = malgoCtx.Uninit()
		malgoCtx.Free()
		slog.Error("voice agent output start", "err", err)
		return
	}

	doneCh := make(chan struct{})
	sp.mu.Lock()
	sp.ctx = malgoCtx
	sp.device = device
	sp.pipe = pipe
	sp.cancel = cancel
	sp.active = true
	sp.doneCh = doneCh
	sp.mu.Unlock()

	go func() {
		<-playCtx.Done()
		sp.shutdown()
		close(doneCh)
	}()
}

func (sp *malgoStreamPlayer) WriteChunk(chunk []byte) {
	sp.mu.Lock()
	pipe := sp.pipe
	active := sp.active
	sp.mu.Unlock()
	if !active || pipe == nil || len(chunk) == 0 {
		return
	}
	pipe.Write(chunk)
}

func (sp *malgoStreamPlayer) StopAndDrain() {
	sp.mu.Lock()
	cancel := sp.cancel
	doneCh := sp.doneCh
	active := sp.active
	sp.mu.Unlock()

	if !active {
		return
	}
	if cancel != nil {
		cancel()
	}
	if doneCh != nil {
		<-doneCh
	}
}

func (sp *malgoStreamPlayer) IsActive() bool {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.active
}

func (sp *malgoStreamPlayer) Close() {
	sp.StopAndDrain()
}

func (sp *malgoStreamPlayer) shutdown() {
	sp.mu.Lock()
	device := sp.device
	malgoCtx := sp.ctx
	pipe := sp.pipe
	sp.device = nil
	sp.ctx = nil
	sp.pipe = nil
	sp.cancel = nil
	sp.active = false
	sp.doneCh = nil
	sp.mu.Unlock()

	if pipe != nil {
		pipe.Close()
	}
	if device != nil {
		if err := device.Stop(); err != nil {
			slog.Warn("voice agent output stop", "err", err)
		}
		device.Uninit()
	}
	if malgoCtx != nil {
		if err := malgoCtx.Uninit(); err != nil {
			slog.Warn("voice agent output context uninit", "err", err)
		}
		malgoCtx.Free()
	}
}
