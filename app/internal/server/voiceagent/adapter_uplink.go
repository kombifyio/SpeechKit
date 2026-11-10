//go:build linux

package voiceagent

import (
	"errors"
	"sync"
	"time"
)

const (
	// transportWriteTimeout bounds one write on either leg of the session:
	// a client WebSocket frame or one provider audio/control send. A send
	// still blocked after it means that leg is stuck, not slow.
	transportWriteTimeout = 5 * time.Second
	// uplinkStallTimeout closes a session whose client stopped delivering
	// audio mid-input (a stalled device uplink). The general IdleTimeout
	// still governs; this bound only shortens the wait while the client was
	// streaming and the input turn has not been concluded.
	uplinkStallTimeout = 30 * time.Second
	// uplinkStreamingBytes is how much input audio (1 s of 16 kHz S16 mono)
	// must arrive since the last turn boundary before the uplink counts as
	// streaming. The few frames a half-duplex client still has in flight
	// when the server ends its turn therefore never arm the stall watch.
	uplinkStreamingBytes = 16_000 * 2
)

var errUpstreamSendStalled = errors.New("voiceagent: provider send exceeded the transport write timeout")

// boundedUpstreamSend runs one provider send with transportWriteTimeout.
// Provider send APIs take no context, so a stuck network write cannot be
// cancelled here; on timeout the caller ends the session and Run's
// closeProvider releases the blocked write, whose result is discarded.
func boundedUpstreamSend(send func() error) error {
	result := make(chan error, 1)
	go func() { result <- send() }()
	timer := time.NewTimer(transportWriteTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return errUpstreamSendStalled
	}
}

// uplinkWatch fires when an audio-streaming client goes silent before its
// input turn is concluded. It starts paused; streaming audio arms it and a
// turn boundary (audio_end, provider input-final, reply end or interruption)
// pauses it again.
type uplinkWatch struct {
	watchdog *idleWatchdog
	// mu orders the read pump's arm against the provider path's pause, so a
	// frame counted before a turn boundary never re-arms the watch after it.
	mu       sync.Mutex
	streamed int64
}

// newUplinkWatch derives its bound from the session idle timeout: disabled
// when the idle watchdog is disabled, never longer than it.
func newUplinkWatch(idleTimeout time.Duration) *uplinkWatch {
	timeout := time.Duration(0)
	if idleTimeout > 0 {
		timeout = min(uplinkStallTimeout, idleTimeout)
	}
	w := &uplinkWatch{watchdog: newIdleWatchdog(timeout)}
	w.watchdog.Pause()
	return w
}

func (w *uplinkWatch) audio(n int) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.streamed += int64(n)
	if w.streamed < uplinkStreamingBytes {
		return
	}
	// Resume arms a paused watch; Reset extends a running one. Each is a
	// no-op in the other state.
	w.watchdog.Resume()
	w.watchdog.Reset()
}

func (w *uplinkWatch) turnBoundary() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.streamed = 0
	w.watchdog.Pause()
}

func (w *uplinkWatch) fired() <-chan struct{} {
	if w == nil {
		return nil
	}
	return w.watchdog.Fired()
}

func (w *uplinkWatch) stop() {
	if w != nil {
		w.watchdog.Stop()
	}
}

// providerConcludesInput reports whether a provider event ends the client's
// current input turn, after which uplink silence is expected.
func providerConcludesInput(msg *LiveMessage) bool {
	return msg != nil && (msg.InputTranscriptDone || msg.Done || msg.Interrupted ||
		len(msg.Audio) > 0 || msg.OutputTranscript != "" || len(msg.ToolCalls) > 0)
}
