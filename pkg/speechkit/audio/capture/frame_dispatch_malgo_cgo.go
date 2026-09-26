//go:build (windows || darwin) && cgo

package capture

import (
	"fmt"
	"log/slog"
	"runtime"
	"time"

	audiopkg "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

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
				s.framePool().Put(buf)
			})
		case pcmHandler != nil:
			// Legacy path: handlers own the slice they receive, so
			// forward a stable copy and recycle the pooled buffer.
			pcmHandler(append([]byte(nil), buf...))
			s.framePool().Put(buf)
		default:
			s.framePool().Put(buf)
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
				Backend: platformMalgoBackend(),
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

	buf := s.framePool().Get()
	buf = append(buf, inputSamples...)
	select {
	case frames <- buf:
	default:
		s.framePool().Put(buf)
		if n := s.overruns.Add(1); n == 1 || n%100 == 0 {
			slog.Warn("audio frame dispatcher overrun — dropping level/VAD frames (full capture unaffected)",
				"dropped_frames", n)
			s.emit(Event{
				Type:    EventOverrun,
				Backend: platformMalgoBackend(),
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
