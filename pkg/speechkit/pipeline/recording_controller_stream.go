package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"

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
	// The native stream is opened before the recorder reports its start time,
	// so "now" is the closest anchor available for a host that did not pass an
	// explicit epoch. The two are milliseconds apart.
	streamEpoch := captureEpoch(opts, c.clockNow())
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
	select {
	case r.pcm <- frame:
	default:
		dropped := r.droppedPCM.Add(1)
		if dropped == 1 || dropped%100 == 0 {
			controller.onLog(fmt.Sprintf("Provider-stream PCM queue full; dropped %d frame(s)", dropped), "warn")
			speechkit.RecordOutcome(r.ctx, speechkit.OutcomePCMQueueDrop, errors.New("pcm queue full"),
				speechkit.Int64Attr("dropped_frames", dropped),
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
		if err := r.stream.SendPCM(r.ctx, pcm); err != nil {
			if r.ctx.Err() == nil {
				controller.onLog(fmt.Sprintf("Provider-stream send error: %v", err), "error")
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
			case r.ctx.Err() != nil, errors.Is(err, context.Canceled), errors.Is(err, io.EOF):
				return
			default:
				controller.onLog(fmt.Sprintf("Provider-stream receive error: %v", err), "error")
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
			continue
		}
		if event.IsFinal {
			r.finalCount.Add(1)
		}
	}
}

func (c *RecordingController) stopNativeDictationStream(runtime *dictationStreamRuntime) int64 {
	if runtime == nil {
		return 0
	}
	runtime.closeInput()
	if !waitForChannel(runtime.senderDone, 3*time.Second) {
		c.onLog("Provider-stream sender did not drain before finalize; cancelling stream", "warn")
		runtime.cancel()
	}
	finalizeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := runtime.stream.Finalize(finalizeCtx); err != nil && finalizeCtx.Err() == nil {
		c.onLog(fmt.Sprintf("Provider-stream finalize warning: %v", err), "warn")
	}
	cancel()
	if !waitForChannel(runtime.receiverDone, 5*time.Second) {
		c.onLog("Provider-stream receiver did not finish after finalize; cancelling stream", "warn")
		runtime.cancel()
	}
	if flusher, ok := runtime.sink.(LiveCommitFlusher); ok {
		if err := flusher.FlushLiveCommit(context.Background()); err != nil {
			c.onLog(fmt.Sprintf("Provider-stream live-commit flush warning: %v", err), "warn")
		}
	}
	runtime.cancel()
	_ = runtime.stream.Close()
	return runtime.finalCount.Load()
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
