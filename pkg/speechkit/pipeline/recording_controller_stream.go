package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/telemetry"

	speechkitaudio "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// providerStreamDialTimeout bounds the native live-dictation handshake so a
// hung websocket cannot freeze the desktop hotkey loop. The microphone must
// already be open by then; if the handshake misses this window, Start falls
// back to transcribing the full capture.
var providerStreamDialTimeout = 800 * time.Millisecond

func (c *RecordingController) startNativeDictationStream(sessionID uint64, opts speechkit.RecordingStartOptions) (*dictationStreamRuntime, error) {
	c.mu.Lock()
	provider := c.streamProvider
	sink := c.streamSink
	c.mu.Unlock()
	if provider == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	c.mu.Lock()
	startedAt := c.startedAt
	c.mu.Unlock()
	streamEpoch := captureEpoch(opts, startedAt)
	if sink == nil {
		return nil, fmt.Errorf("event sink not configured")
	}
	sink = wrapLiveCommitSink(sink, opts.LiveCommitMode)
	parent := opts.Context
	if parent == nil {
		parent = context.Background()
	}
	streamCtx, cancel := context.WithCancel(parent)
	streamOpts := opts.DictationStreamOptions
	streamOpts.SessionID = sessionID
	if streamOpts.Language == "" {
		streamOpts.Language = opts.Language
	}
	streamOpts.InterimResults = true
	dialTimeout := providerStreamDialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 800 * time.Millisecond
	}
	dialCtx, dialCancel := context.WithTimeout(streamCtx, dialTimeout)
	stream, err := provider.StartDictationStream(dialCtx, streamOpts, speaker.AudioFormat{
		Encoding:     speaker.AudioEncodingPCM16,
		SampleRateHz: 16000,
		Channels:     1,
	})
	dialCancel()
	if err != nil {
		cancel()
		return nil, err
	}
	runtime := &dictationStreamRuntime{
		sessionID: sessionID,
		stream:    stream,
		sink:      sink,
		sinkOpts: speechkit.DictationStreamSinkOptions{
			Target:             opts.Target,
			QuickNote:          opts.QuickNote,
			QuickNoteID:        opts.QuickNoteID,
			Language:           opts.Language,
			RecordingSessionID: opts.RecordingSessionID,
			CaptureChannel:     opts.CaptureChannel,
			CaptureEpoch:       streamEpoch,
		},
		ctx:          streamCtx,
		cancel:       cancel,
		pcm:          make(chan []byte, 64),
		senderDone:   make(chan struct{}),
		receiverDone: make(chan struct{}),
	}
	go runtime.sendLoop(c)
	go runtime.receiveLoop(c)
	return runtime, nil
}

func (r *dictationStreamRuntime) enqueuePCM(pcm []byte, controller *RecordingController) {
	if r == nil || len(pcm) == 0 {
		return
	}
	frame := append([]byte(nil), pcm...)
	r.inputMu.Lock()
	defer r.inputMu.Unlock()
	if r.closed {
		return
	}
	if r.failed.Load() {
		return
	}
	select {
	case r.pcm <- frame:
	default:
		dropped := r.droppedPCM.Add(1)
		// Do not resume after a hole: later word timestamps would no longer
		// locate the missing speech. Stop recovers from the last committed
		// word using the recorder's complete audio.
		r.failed.Store(true)
		r.cancel()
		if dropped == 1 || dropped%100 == 0 {
			controller.onLog(fmt.Sprintf("Provider-stream PCM queue full; dropped %d frame(s)", dropped), "warn")
			telemetry.RecordOutcome(r.ctx, telemetry.OutcomePCMQueueDrop, errors.New("pcm queue full"),
				telemetry.Int64Attr("dropped_frames", dropped),
			)
		}
	}
}

func (r *dictationStreamRuntime) closeInput() {
	if r == nil {
		return
	}
	r.inputMu.Lock()
	defer r.inputMu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	close(r.pcm)
}

func (r *dictationStreamRuntime) sendLoop(controller *RecordingController) {
	defer close(r.senderDone)
	for pcm := range r.pcm {
		if r.ctx.Err() != nil {
			r.failed.Store(true)
			return
		}
		if err := r.stream.SendPCM(r.ctx, pcm); err != nil {
			r.failed.Store(true)
			if r.ctx.Err() == nil {
				controller.onLog(fmt.Sprintf("Provider-stream send error: %v", err), "error")
				r.failed.Store(true)
			}
			r.cancel()
			return
		}
	}
}

func (r *dictationStreamRuntime) receiveLoop(controller *RecordingController) {
	defer close(r.receiverDone)
	for {
		event, err := r.stream.Receive(r.ctx)
		if err != nil {
			switch {
			case r.ctx.Err() != nil, errors.Is(err, context.Canceled):
				r.failed.Store(true)
				return
			case errors.Is(err, io.EOF):
				if !r.ending.Load() {
					r.failed.Store(true)
					controller.onLog("Provider-stream ended before capture stopped; retaining audio for recovery", "warn")
				}
				return
			default:
				controller.onLog(fmt.Sprintf("Provider-stream receive error: %v", err), "error")
				r.failed.Store(true)
				r.cancel()
				return
			}
		}
		if event.SessionID == 0 {
			event.SessionID = r.sessionID
		}
		if event.SegmentID == 0 {
			if event.Sequence > 0 {
				event.SegmentID = uint64(event.Sequence)
			} else {
				event.SegmentID = r.eventSeq.Add(1)
			}
		}
		if event.ProviderItemID == "" && event.IsFinal {
			event.ProviderItemID = fmt.Sprintf("stream:%d:%d", event.SessionID, event.SegmentID)
		}
		if strings.TrimSpace(event.Text) == "" && len(event.Words) == 0 {
			continue
		}
		if err := r.sink.HandleDictationStreamEvent(r.ctx, event, r.sinkOpts); err != nil {
			controller.onLog(fmt.Sprintf("Provider-stream transcript error: %v", err), "error")
			r.failed.Store(true)
			r.cancel()
			return
		}
		if event.IsFinal {
			r.finalCount.Add(1)
			for _, word := range event.Words {
				if word.EndMs > r.committedEndMs.Load() {
					r.committedEndMs.Store(word.EndMs)
				}
			}
		}
	}
}

// uncommittedTail returns the part of the full capture the stream never
// committed: everything after the last committed word. The stream starts at
// capture offset zero, and stops on its first queue loss. Unknown capture
// gaps require a full replay instead of trusting stream-relative timestamps.
func (r *dictationStreamRuntime) uncommittedTail(pcm []byte) (tail []byte, offsetMs int64, ok bool) {
	endMs := r.committedEndMs.Load()
	if endMs <= 0 || r.replayFull.Load() {
		return nil, 0, false
	}
	from := int(endMs) * pcmBytesPerMs
	from -= from % speechkitaudio.BytesPerSample
	if from >= len(pcm) {
		return nil, int64(len(pcm) / pcmBytesPerMs), true
	}
	return pcm[from:], int64(from / pcmBytesPerMs), true
}

const pcmBytesPerMs = speechkitaudio.SampleRate * speechkitaudio.Channels * speechkitaudio.BytesPerSample / 1000

// uncommittedStreamAudio returns the capture Stop still owes a stream that
// committed finals but did not finish cleanly, and where it begins. It returns
// nil only when no uncommitted audio remains. Missing timings require a full
// replay: omitting or energy-gating that audio can remove short negations.
func (c *RecordingController) uncommittedStreamAudio(r *dictationStreamRuntime, pcm []byte, finals int64) ([]byte, int64) {
	tail, offsetMs, bounded := r.uncommittedTail(pcm)
	if !bounded {
		tail, offsetMs = pcm, 0
	}
	if len(tail) == 0 {
		c.onLog(fmt.Sprintf("Provider-stream ended early after %d committed segment(s); no uncommitted audio remains", finals), "info")
		return nil, 0
	}
	c.onLog(fmt.Sprintf("Provider-stream ended early after %d committed segment(s); transcribing the remaining %.1fs of capture", finals, speechkit.PCMDurationSecs(tail)), "warn")
	return tail, offsetMs
}

// stopNativeDictationStream drains and closes a stream. cancelled drops what
// a session-mode sink still holds: a cancelled hold-to-talk capture must not
// insert its text. Other grouping modes flush as before, because their
// grouped finals belong to text the user already saw being inserted.
func (c *RecordingController) stopNativeDictationStream(runtime *dictationStreamRuntime, cancelled bool) int64 {
	if runtime == nil {
		return 0
	}
	runtime.closeInput()
	if !waitForChannel(runtime.senderDone, 3*time.Second) {
		c.onLog("Provider-stream sender did not drain before finalize; cancelling stream", "warn")
		runtime.cutShort.Store(true)
		runtime.cancel()
	}
	runtime.ending.Store(true)
	finalizeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := runtime.stream.Finalize(finalizeCtx); err != nil {
		c.onLog(fmt.Sprintf("Provider-stream finalize warning: %v", err), "warn")
		runtime.cutShort.Store(true)
	}
	cancel()
	if !waitForChannel(runtime.receiverDone, 5*time.Second) {
		c.onLog("Provider-stream receiver did not finish after finalize; cancelling stream", "warn")
		runtime.cutShort.Store(true)
		runtime.cancel()
	}
	grouped, sessionHeld := runtime.sink.(*liveCommitSink)
	sessionHeld = sessionHeld && grouped.policy.Mode == LiveCommitSession
	if sessionHeld && (cancelled || runtime.failed.Load() || runtime.cutShort.Load()) {
		grouped.discard()
		if !cancelled {
			runtime.replayFull.Store(true)
		}
	} else if flusher, ok := runtime.sink.(LiveCommitFlusher); ok {
		flushErr := flusher.FlushLiveCommit(context.WithoutCancel(runtime.ctx))
		if flushErr != nil {
			c.onLog(fmt.Sprintf("Provider-stream live-commit flush warning: %v", flushErr), "warn")
			runtime.failed.Store(true)
			if sessionHeld {
				runtime.replayFull.Store(true)
			}
		}
	}
	runtime.cancel()
	_ = runtime.stream.Close()
	// A cancelled receiver may still be returning from its last read; let it
	// exit so the counts below are final.
	waitForChannel(runtime.receiverDone, time.Second)
	return runtime.finalCount.Load()
}

// finishNativeDictationStream stops a session the user ended and tells the
// sink, so output it held back while the microphone was open is delivered
// now (see [speechkit.DictationStreamSessionEnder]). Cancel stops a stream
// without this step.
func (c *RecordingController) finishNativeDictationStream(runtime *dictationStreamRuntime, pcm []byte) int64 {
	finals := c.stopNativeDictationStream(runtime, false)
	if runtime == nil {
		return finals
	}
	if len(pcm) > 0 && finals > 0 {
		if recorder, ok := runtime.sink.(speechkit.DictationStreamRecordingSink); ok {
			recorder.CompleteDictationStreamRecording(context.WithoutCancel(runtime.ctx), runtime.sessionID, speechkit.Submission{
				PCM: pcm, WAV: speechkit.PCMToWAV(pcm), DurationSecs: speechkit.PCMDurationSecs(pcm),
				Language: runtime.sinkOpts.Language, SessionID: runtime.sessionID,
			}, runtime.sinkOpts)
		}
	}
	if ender, ok := runtime.sink.(speechkit.DictationStreamSessionEnder); ok {
		ender.EndDictationStreamSession(context.WithoutCancel(runtime.ctx), runtime.sessionID, runtime.sinkOpts)
	}
	return finals
}

func waitForChannel(done <-chan struct{}, timeout time.Duration) bool {
	if done == nil {
		return true
	}
	if timeout <= 0 {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
