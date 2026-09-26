package pipeline

import (
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"

	speechkitaudio "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

// shortNoSpeechGateSecs bounds the junk-capture gate: only captures shorter
// than this AND with all audio below the noise floor are dropped before STT
// submission. Kept short so no legitimate one-word utterance is at risk.
const shortNoSpeechGateSecs = 2.0

// noiseFloorRMS is the normalised RMS ([0,1] against int16 full scale) below
// which a capture is considered to contain no audio at all. Room silence on
// desktop mics sits around 0.005; real speech sits an order of magnitude
// higher.
const noiseFloorRMS = 0.003

// Stop ends the active session and queues its audio for transcription. After
// the optional opts.TailDelay it closes the recorder and drops captures that
// are too short, silent, or implausibly long for the wall-clock window (stale
// device buffer). A provider-native stream that produced finals completes the
// session itself; otherwise one full-capture job is submitted (VAD segments
// when fragmenting or streaming is enabled) with opts.Label in the log. It
// returns the submitter's error and reports "processing" or "idle" to the
// observer. A no-op while nothing is recording or a stop is in progress.
func (c *RecordingController) Stop(opts speechkit.RecordingStopOptions) error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	if !c.recording || c.stopping {
		c.mu.Unlock()
		return nil
	}
	c.stopping = true
	sessionID := c.sessionID
	// Tear down any active idle watcher so it does not fire a stale
	// callback against the next session. Closing the channel signals
	// the goroutine to exit on its next select iteration.
	if c.idleWatcherCh != nil {
		close(c.idleWatcherCh)
		c.idleWatcherCh = nil
	}
	c.mu.Unlock()
	defer c.clearStopping()

	if opts.TailDelay > 0 {
		time.Sleep(opts.TailDelay)
	}

	c.mu.Lock()
	if c.sessionID != sessionID {
		c.mu.Unlock()
		return nil
	}
	c.recording = false
	current := c.current
	collector := c.collector
	startedAt := c.startedAt
	streamSegments := current.StreamSegments
	streamedCount := c.streamedCount
	nativeStream := c.nativeStream
	c.collector = nil
	c.startedAt = time.Time{}
	c.nativeStream = nil
	c.mu.Unlock()

	c.clearPCMHandlers()
	pcm, stopErr := c.recorder.Stop()
	if stopErr != nil {
		c.onLog(fmt.Sprintf("Capture stop warning: %v", stopErr), "warn")
	}

	dur := speechkit.PCMDurationSecs(pcm)
	c.onLog(fmt.Sprintf("%s: %.1fs audio", opts.Label, dur), "info")
	wallDuration := c.clockNow().Sub(startedAt)
	if isStaleCapturedAudio(dur, wallDuration) {
		if nativeStream != nil {
			if finals := c.stopNativeDictationStream(nativeStream); finals > 0 {
				c.onLog(fmt.Sprintf("Provider-stream dictation finalized with %d committed segment(s)", finals), "info")
				return nil
			}
		}
		c.onLog(fmt.Sprintf("Captured audio discarded: %.1fs audio exceeds the %.1fs wall-clock recording window; stale microphone buffer suspected", dur, wallDuration.Seconds()), "warn")
		c.onState("idle", "")
		return nil
	}

	if len(pcm) < c.minPCMBytes {
		if nativeStream != nil {
			if finals := c.stopNativeDictationStream(nativeStream); finals > 0 {
				c.onLog(fmt.Sprintf("Provider-stream dictation finalized with %d committed segment(s)", finals), "info")
				return nil
			}
		}
		c.onLog("Too short, skipped", "error")
		c.onState("idle", "")
		c.collector = nil
		return nil
	}

	// Junk-capture gate: a very short capture whose audio never rises above
	// the noise floor is key chatter or an accidental tap, not dictation.
	// Submitting it anyway costs a full provider roundtrip (and, with a
	// single worker, delays every real job queued behind it). The check is
	// pure signal energy — deliberately NOT the VAD verdict, so the crude
	// level VAD can never discard a capture that contains actual audio.
	// Provider-stream sessions are exempt: their finalization below owns
	// the speech/no-speech verdict.
	if nativeStream == nil && dur < shortNoSpeechGateSecs && speechkitaudio.PCMLevel(pcm) < noiseFloorRMS {
		c.onLog(fmt.Sprintf("No audio above noise floor in %.1fs capture, skipped", dur), "info")
		c.onState("idle", "")
		return nil
	}

	segments := FallbackDictationSegments(pcm)
	if collector != nil {
		collected, err := collector.CollectStopSegments(pcm)
		if err != nil {
			c.onLog(fmt.Sprintf("Dictation processor fallback: %v", err), "warn")
		} else {
			segments = collected
		}
	}

	// Excision diagnostic: compare the full captured duration against what the
	// VAD-segment path would have sent to STT. A large positive delta means the
	// segmenter was dropping real speech — the dropped-words root cause.
	var segTotalSecs float64
	for _, segment := range segments {
		segTotalSecs += speechkit.PCMDurationSecs(segment.PCM)
	}
	c.onLog(fmt.Sprintf("dictation audio: full=%.1fs vs %d VAD-segments totalling %.1fs (delta=%.1fs)", dur, len(segments), segTotalSecs, dur-segTotalSecs), "info")

	if nativeStream != nil {
		finals := c.stopNativeDictationStream(nativeStream)
		if finals > 0 {
			c.onLog(fmt.Sprintf("Provider-stream dictation finalized with %d committed segment(s)", finals), "info")
			// Capture has stopped; leave "recording" immediately so the overlay
			// does not sit on a phantom take while the last finals settle.
			c.onState("processing", "")
			return nil
		}
		c.onLog("Provider-stream dictation produced no final transcript; falling back to full capture", "warn")
		streamSegments = false
	}

	if streamSegments {
		c.queueStreamSegments(sessionID, segments)
		submitted, err := c.flushPendingStreamSegments(sessionID, current, "remaining dictation segment", c.clockNow().Add(5*time.Second))
		if err != nil {
			c.onLog(fmt.Sprintf("Queue error: %v", err), "error")
			c.onState("idle", "")
			return err
		}
		if submitted == 0 {
			if streamedCount > 0 {
				c.onLog("No remaining dictation tail after streamed segments", "info")
				c.onState("processing", "")
				return nil
			}
			c.onLog("No speech segments detected, skipped", "error")
			c.onState("idle", "")
			return nil
		}
		c.onState("processing", "")
		return nil
	}

	// Default (fragmentSegments=false): transcribe the FULL captured audio as a
	// single submission (job.Segments stays nil, so EffectiveSegments()
	// falls back to the full-PCM Submission). This eliminates every VAD-excision
	// word-loss path — onset clipping, low-energy word gating, short trailing
	// word drop, and multi-segment all-or-nothing failure — at no latency cost
	// for normal dictation (it was a single Deepgram call anyway).
	var jobSegments []speechkit.Submission
	if c.fragmentSegments {
		if len(segments) == 0 {
			c.onLog("No speech segments detected, skipped", "error")
			c.onState("idle", "")
			return nil
		}
		jobSegments = make([]speechkit.Submission, 0, len(segments))
		for _, segment := range segments {
			prefix := ""
			if segment.Paragraph {
				prefix = "\n\n"
			}
			jobSegments = append(jobSegments, speechkit.Submission{
				PCM:                segment.PCM,
				WAV:                speechkit.PCMToWAV(segment.PCM),
				DurationSecs:       speechkit.PCMDurationSecs(segment.PCM),
				Language:           current.Language,
				Prefix:             prefix,
				RecordingSessionID: current.RecordingSessionID,
				CaptureChannel:     current.CaptureChannel,
			})
		}
	}

	capturedStartMs := elapsedCaptureMs(captureEpoch(current, startedAt), startedAt)
	if err := c.submitter.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{
			PCM:                pcm,
			WAV:                speechkit.PCMToWAV(pcm),
			DurationSecs:       dur,
			Language:           current.Language,
			QuickNote:          current.QuickNote,
			QuickNoteID:        current.QuickNoteID,
			RecordingSessionID: current.RecordingSessionID,
			CaptureChannel:     current.CaptureChannel,
			CapturedStartMs:    capturedStartMs,
			CapturedEndMs:      capturedStartMs + int64(dur*1000),
		},
		Segments: jobSegments,
		Target:   current.Target,
	}); err != nil {
		c.onLog(fmt.Sprintf("Queue error: %v", err), "error")
		c.onState("idle", "")
		return err
	}

	// Capture has physically stopped and the job is queued. Without this the
	// UI keeps showing "recording" until the worker dequeues the job — which
	// can lag by seconds when earlier jobs are still in flight.
	c.onState("processing", "")

	return nil
}

// Cancel stops the active recorder and discards the captured audio. Hosts use
// this when the user switches modes mid-capture; submitting the old buffer
// would deliver stale speech through the newly selected mode.
func (c *RecordingController) Cancel(opts speechkit.RecordingCancelOptions) error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	if !c.recording {
		c.mu.Unlock()
		return nil
	}
	if c.stopping {
		c.mu.Unlock()
		return fmt.Errorf("speechkit: recording is already stopping")
	}
	c.stopping = true
	sessionID := c.sessionID
	if c.idleWatcherCh != nil {
		close(c.idleWatcherCh)
		c.idleWatcherCh = nil
	}
	c.mu.Unlock()
	defer c.clearStopping()

	c.mu.Lock()
	if c.sessionID != sessionID {
		c.mu.Unlock()
		return nil
	}
	c.recording = false
	c.current = speechkit.RecordingStartOptions{}
	c.collector = nil
	c.startedAt = time.Time{}
	c.streamPending = nil
	c.streamFlush = false
	nativeStream := c.nativeStream
	c.nativeStream = nil
	c.mu.Unlock()
	if nativeStream != nil {
		c.stopNativeDictationStream(nativeStream)
	}

	c.clearPCMHandlers()
	_, stopErr := c.recorder.Stop()
	if stopErr != nil {
		c.onLog(fmt.Sprintf("Capture cancel warning: %v", stopErr), "warn")
	}

	label := opts.Label
	if label == "" {
		label = "Capture discarded"
	}
	c.onLog(label, "info")
	c.onState("idle", "")
	return stopErr
}
