package pipeline

import (
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// deadCaptureBackstopFloor is the minimum no-frames-at-all duration
// before the audio-anchored idle watcher force-stops a session. High
// enough that load-induced delivery stalls (sub-second to a few
// seconds) can never trigger it; a genuinely dead capture device still
// terminates the session.
const deadCaptureBackstopFloor = 30 * time.Second

// Start begins a new session: it installs the PCM handler feeding the
// collector built by the segmenter factory (and, with opts.ProviderStream, the
// provider-native dictation stream), opens the recorder, and arms the silence
// watcher when opts.IdleTimeout and opts.OnIdleTimeoutCallback are set and
// the collector reports idle time. A stream that fails to start is logged and
// the session falls back to full capture. It reports "recording" (and
// opts.Label) to the observer on success and returns the recorder's error,
// leaving the controller idle, when the device fails to open; a nil
// controller returns an error.
func (c *RecordingController) Start(opts speechkit.RecordingStartOptions) error {
	if c == nil {
		return fmt.Errorf("speechkit: recording controller not configured")
	}

	var (
		collector speechkit.SegmentCollector
		sessionID uint64
	)

	c.mu.Lock()
	c.recording = true
	if c.streamSegments {
		opts.StreamSegments = true
	}
	c.current = opts
	c.collector = nil
	c.streamedCount = 0
	c.streamSegmentSeq = 0
	c.streamPending = nil
	c.streamFlush = false
	c.sessionID++
	sessionID = c.sessionID

	if c.segmenterFactory != nil {
		collector = c.segmenterFactory()
		c.collector = collector
	}
	c.mu.Unlock()

	// Install the PCM handler before opening the microphone and before the
	// provider handshake. A live stream that is still dialing must not
	// delay capture: the hotkey loop dispatches Start synchronously, and a
	// hung websocket used to swallow the matching KeyUp so later presses
	// did nothing.
	if collector != nil || opts.ProviderStream {
		handlePCM := func(pcm []byte) {
			c.mu.Lock()
			if c.sessionID != sessionID || !c.recording {
				c.mu.Unlock()
				return
			}
			activeCollector := c.collector
			current := c.current
			nativeStream := c.nativeStream
			c.mu.Unlock()
			if nativeStream != nil {
				nativeStream.enqueuePCM(pcm, c)
			}
			if activeCollector == nil {
				return
			}
			if err := activeCollector.FeedPCM(pcm); err != nil {
				c.onLog(fmt.Sprintf("Dictation processor fallback: %v", err), "warn")
				c.mu.Lock()
				if c.sessionID == sessionID {
					c.collector = nil
				}
				c.mu.Unlock()
				c.clearPCMHandlers()
				return
			}
			if current.StreamSegments {
				c.drainAndSubmitReadySegments(sessionID, current, activeCollector)
			}
		}
		if pooled, ok := c.recorder.(PooledPCMRecorder); ok {
			// Pool-aware path: neither enqueuePCM nor FeedPCM retains
			// the frame (both copy), so the buffer can be released as
			// soon as the handler returns.
			c.recorder.SetPCMHandler(nil)
			pooled.SetPooledPCMHandler(func(buf []byte, release func()) {
				defer release()
				handlePCM(buf)
			})
		} else {
			c.recorder.SetPCMHandler(handlePCM)
		}
	} else {
		c.clearPCMHandlers()
	}

	recorderStartAt := c.clockNow()
	if err := c.recorder.Start(); err != nil {
		c.mu.Lock()
		nativeStream := c.nativeStream
		if c.sessionID == sessionID {
			c.recording = false
			c.collector = nil
			c.startedAt = time.Time{}
			c.nativeStream = nil
		}
		c.mu.Unlock()
		if nativeStream != nil {
			c.stopNativeDictationStream(nativeStream)
		}
		c.clearPCMHandlers()
		c.onLog(fmt.Sprintf("Capture error: %v", err), "error")
		c.onState("idle", "")
		return err
	}
	startedAt := c.clockNow()
	if openDur := startedAt.Sub(recorderStartAt); openDur > 300*time.Millisecond {
		// Audio spoken before the device opened is lost — make a slow open
		// visible so late-start regressions (e.g. device re-enumeration on
		// the hot path) show up in the log instead of as missing words.
		c.onLog(fmt.Sprintf("Capture device open took %dms — start of speech may be clipped", openDur.Milliseconds()), "warn")
	}
	c.mu.Lock()
	if c.sessionID == sessionID {
		c.startedAt = startedAt
	}
	c.mu.Unlock()

	c.onState("recording", c.recordingMessage)
	if opts.Label != "" {
		c.onLog(opts.Label, "info")
	}

	if opts.ProviderStream {
		nativeStream, err := c.startNativeDictationStream(sessionID, opts)
		if err != nil {
			c.onLog(fmt.Sprintf("Provider-stream dictation unavailable; falling back to full capture: %v", err), "warn")
		} else {
			adopted := false
			c.mu.Lock()
			if c.sessionID == sessionID && c.recording && !c.stopping {
				opts.StreamSegments = false
				c.current.StreamSegments = false
				c.nativeStream = nativeStream
				adopted = true
			}
			c.mu.Unlock()
			if adopted {
				c.onLog("Provider-stream dictation started", "info")
			} else {
				nativeStream.cancel()
				_ = nativeStream.stream.Close()
			}
		}
	}

	// Arm the silence-based auto-stop watcher when both pieces are
	// present: the host provided a timeout + callback AND the collector
	// can report idle time. Wake-word/hold-to-talk paths leave the
	// timeout at zero and skip the watcher entirely.
	if opts.IdleTimeout > 0 && opts.OnIdleTimeoutCallback != nil {
		switch observer := collector.(type) {
		case speechkit.AudioIdleObserver:
			c.startAudioIdleWatcher(sessionID, observer, opts.IdleTimeout, opts.OnIdleTimeoutCallback)
		case speechkit.IdleObserver:
			c.startIdleWatcher(sessionID, observer, opts.IdleTimeout, opts.OnIdleTimeoutCallback)
		}
	}

	return nil
}

// startIdleWatcher spawns a goroutine that polls observer.IdleSince()
// every idleWatchInterval and fires callback once the gap to time.Now
// exceeds timeout. The watcher exits when the session changes (Stop()
// closes the per-session channel) or when it has already fired.
func (c *RecordingController) startIdleWatcher(sessionID uint64, observer speechkit.IdleObserver, timeout time.Duration, callback func()) {
	done := make(chan struct{})
	c.mu.Lock()
	c.idleWatcherCh = done
	interval := c.idleWatchInterval
	c.mu.Unlock()
	if interval <= 0 {
		interval = defaultIdleWatchInterval
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				c.mu.Lock()
				stillActive := c.sessionID == sessionID && c.recording
				c.mu.Unlock()
				if !stillActive {
					return
				}
				idleSince := observer.IdleSince()
				if idleSince.IsZero() {
					// Speech in progress — skip this tick.
					continue
				}
				if time.Since(idleSince) >= timeout {
					c.onLog(fmt.Sprintf("Silence timeout reached (%.0fs) — auto-stopping dictate.", timeout.Seconds()), "info")
					// Mark watcher done before firing the callback so a
					// Stop() racing on the same channel does not deadlock.
					c.mu.Lock()
					if c.idleWatcherCh == done {
						c.idleWatcherCh = nil
					}
					c.mu.Unlock()
					callback()
					return
				}
			}
		}
	}()
}

// startAudioIdleWatcher is the audio-anchored variant of startIdleWatcher:
// it polls observer.IdleAudio() and fires callback once the *processed
// audio* has been silent for timeout. Because silence only accumulates
// while frames actually flow, a CPU-starvation delivery stall freezes the
// countdown instead of auto-stopping mid-dictation. A dead-capture
// backstop still terminates the session when no frames arrive at all for
// max(timeout, deadCaptureBackstopFloor).
func (c *RecordingController) startAudioIdleWatcher(sessionID uint64, observer speechkit.AudioIdleObserver, timeout time.Duration, callback func()) {
	done := make(chan struct{})
	c.mu.Lock()
	c.idleWatcherCh = done
	interval := c.idleWatchInterval
	c.mu.Unlock()
	if interval <= 0 {
		interval = defaultIdleWatchInterval
	}

	backstop := timeout
	if backstop < deadCaptureBackstopFloor {
		backstop = deadCaptureBackstopFloor
	}
	watcherStart := c.now()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				c.mu.Lock()
				stillActive := c.sessionID == sessionID && c.recording
				c.mu.Unlock()
				if !stillActive {
					return
				}
				silence, lastFrame := observer.IdleAudio()
				fire := false
				switch {
				case silence >= timeout:
					c.onLog(fmt.Sprintf("Silence timeout reached (%.0fs audio silence, limit %.0fs) — auto-stopping dictate.", silence.Seconds(), timeout.Seconds()), "info")
					fire = true
				case lastFrame.IsZero() && c.now().Sub(watcherStart) >= backstop:
					// Capture never delivered a single frame — dead device.
					c.onLog(fmt.Sprintf("Capture stalled — no audio frames since session start (%.0fs); stopping dictate.", backstop.Seconds()), "warning")
					fire = true
				case !lastFrame.IsZero() && c.now().Sub(lastFrame) >= backstop:
					c.onLog(fmt.Sprintf("Capture stalled — no audio frames for %.0fs; stopping dictate.", c.now().Sub(lastFrame).Seconds()), "warning")
					fire = true
				}
				if !fire {
					continue
				}
				// Mark watcher done before firing the callback so a
				// Stop() racing on the same channel does not deadlock.
				c.mu.Lock()
				if c.idleWatcherCh == done {
					c.idleWatcherCh = nil
				}
				c.mu.Unlock()
				callback()
				return
			}
		}
	}()
}
