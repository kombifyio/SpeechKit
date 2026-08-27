package speechkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"

	speechkitaudio "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// IdleObserver is implemented by SegmentCollectors that want to drive
// silence-based auto-stop. Returning the zero value tells the watcher
// "user is actively speaking; reset the timer." Returning a non-zero
// time tells the watcher "user has been silent since T."
type IdleObserver interface {
	IdleSince() time.Time
}

// AudioIdleObserver is implemented by SegmentCollectors that can report
// silence in audio time: the cumulative duration of processed silent
// frames since the last detected speech, plus the wall-clock time of the
// most recently processed frame. When a collector satisfies this
// interface it is preferred over [IdleObserver], because audio-anchored
// silence is immune to CPU-starvation stalls — when frame delivery
// stalls, the silence counter freezes instead of counting wall-clock
// seconds and auto-stopping mid-dictation.
type AudioIdleObserver interface {
	IdleAudio() (silence time.Duration, lastFrame time.Time)
}

const defaultIdleWatchInterval = 1 * time.Second

// deadCaptureBackstopFloor is the minimum no-frames-at-all duration
// before the audio-anchored idle watcher force-stops a session. High
// enough that load-induced delivery stalls (sub-second to a few
// seconds) can never trigger it; a genuinely dead capture device still
// terminates the session.
const deadCaptureBackstopFloor = 30 * time.Second

const DefaultMinPCMBytes = 3200

// Capture channels name the audio source behind a recording. A host that
// records one session from several sources at once — meeting capture takes the
// microphone and the system loopback in parallel — labels each controller so
// the resulting transcripts stay attributable.
const (
	CaptureChannelMicrophone = "mic"
	CaptureChannelSystem     = "system"
)

const (
	staleCaptureMinDuration = 30 * time.Second
	staleCaptureGrace       = 5 * time.Second
	staleCaptureMaxRatio    = 4.0
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

// AudioRecorder is the hardware abstraction for microphone capture.
type AudioRecorder interface {
	Start() error
	Stop() ([]byte, error)
	SetPCMHandler(func([]byte))
}

// PooledPCMRecorder is optionally implemented by AudioRecorders whose
// backend leases per-frame buffers from a pool instead of allocating a
// fresh copy per frame (~33 allocations/sec during capture). When the
// recorder satisfies this interface the controller installs the
// pool-aware handler and releases each buffer as soon as the frame has
// been fed to the collector/stream — the controller never retains a
// frame. Structurally matches internal/audio's SetPooledPCMHandler.
type PooledPCMRecorder interface {
	SetPooledPCMHandler(func(buf []byte, release func()))
}

// SegmentCollector accumulates real-time PCM frames and splits them into
// dictation segments when recording stops.
type SegmentCollector interface {
	FeedPCM([]byte) error
	CollectStopSegments(fullPCM []byte) ([]AudioSegment, error)
}

// ReadySegmentCollector is implemented by collectors that can hand completed
// pause-bounded segments to the transcription queue before recording stops.
type ReadySegmentCollector interface {
	SegmentCollector
	DrainReadySegments() []AudioSegment
}

type SegmentCollectorFactory func() SegmentCollector

// JobSubmitter accepts a [TranscriptionJob] for async processing.
type JobSubmitter interface {
	Submit(TranscriptionJob) error
}

type RecordingObserver interface {
	OnState(status, text string)
	OnLog(message, kind string)
}

type RecordingStartOptions struct {
	// Context scopes provider-native streaming sessions. When nil,
	// context.Background is used.
	Context     context.Context
	Label       string
	Target      any
	Language    string
	QuickNote   bool
	QuickNoteID int64
	// RecordingSessionID links final transcript commits to a persisted
	// long-running dictation or meeting session owned by the host.
	RecordingSessionID int64
	// CaptureChannel names the audio source this controller records, so a host
	// running several controllers over one recording session — meeting capture
	// records the microphone and the system loopback at the same time — can tell
	// the resulting transcripts apart. See CaptureChannel*.
	CaptureChannel string
	// CaptureEpoch is the wall clock that transcript timestamps are measured
	// from. Hosts recording one session across several controllers pass the same
	// epoch to all of them so the transcripts interleave on a single timeline.
	// Zero means "start of this recording".
	CaptureEpoch time.Time
	// StreamSegments enables live-ish dictation for this recording session.
	// Completed pause-bounded segments are queued before Stop(); Stop() then
	// flushes only pending/remaining tail segments. Leave false for Assist and
	// Voice Agent fallback capture, where a single full turn is the safer unit.
	StreamSegments bool
	// ProviderStream enables provider-native realtime dictation when the host
	// configured a DictationStreamProvider and DictationStreamSink. If stream
	// startup fails, the controller keeps using StreamSegments/full-capture
	// fallback behavior.
	ProviderStream bool
	// DictationStreamOptions are passed to the native provider stream. SessionID
	// and Language are filled from the active recording when left empty.
	DictationStreamOptions DictationStreamOptions
	// LiveCommitMode groups provider-finals before field injection.
	// Empty keeps immediate commit (tests and hosts that do not opt in).
	// Desktop dictation defaults to LiveCommitPassage.
	LiveCommitMode string
	// IdleTimeout, when greater than zero AND the underlying collector
	// implements [IdleObserver], arms a watcher that calls
	// OnIdleTimeoutCallback once the user has been silent for this long.
	// Zero (default) disables the watcher — typical for hold-to-talk
	// hotkey sessions that already terminate on KeyUp.
	IdleTimeout time.Duration
	// OnIdleTimeoutCallback fires once if IdleTimeout elapses without
	// observed speech. Wired by the host to dispatch a Stop command so
	// the dictate session ends after a silence window. The watcher
	// guarantees at-most-one invocation per Start() call.
	OnIdleTimeoutCallback func()
}

type RecordingStopOptions struct {
	Label string
	// TailDelay keeps the recorder physically open for a short grace period
	// after a stop request. Hold-to-talk callers use this to avoid clipping
	// final syllables when the shortcut is released a few milliseconds early.
	TailDelay time.Duration
}

// RecordingCancelOptions controls cancellation of an active recording without
// submitting captured audio. It is for host-level interruptions such as a mode
// switch, where the old buffer must be discarded rather than transcribed.
type RecordingCancelOptions struct {
	Label string
}

// RecordingController manages the start/stop lifecycle of a single recording
// session and hands audio segments to the submission queue.
type RecordingController struct {
	recorder         AudioRecorder
	submitter        JobSubmitter
	observer         RecordingObserver
	segmenterFactory SegmentCollectorFactory
	streamProvider   DictationStreamProvider
	streamSink       DictationStreamSink
	recordingMessage string
	minPCMBytes      int
	// fragmentSegments, when true, submits the VAD-derived audio segments as the
	// STT source (legacy parallel-segment path). When false (the default) the
	// full captured audio is transcribed as a single submission so speech the
	// crude RMS VAD mis-classified cannot be excised before STT — this is the
	// dropped-words fix; the segmenter still drives silence-based auto-stop.
	fragmentSegments bool
	// streamSegments, when true, submits completed VAD-derived segments as soon
	// as a sufficiently long utterance is closed by a pause. Stop() then submits
	// only the remaining tail. This is opt-in so public framework users keep the
	// safer full-capture default unless their host explicitly wants live-ish
	// dictation behavior.
	streamSegments bool
	// idleWatchInterval is how often the idle watcher polls the
	// IdleObserver. Defaults to 1s — overridable from tests.
	idleWatchInterval time.Duration
	now               func() time.Time

	mu               sync.Mutex
	recording        bool
	stopping         bool
	sessionID        uint64
	startedAt        time.Time
	current          RecordingStartOptions
	collector        SegmentCollector
	idleWatcherCh    chan struct{}
	streamedCount    int
	streamSegmentSeq uint64
	streamPending    []AudioSegment
	streamFlush      bool
	nativeStream     *dictationStreamRuntime
}

type dictationStreamRuntime struct {
	sessionID uint64
	stream    DictationStream
	sink      DictationStreamSink
	sinkOpts  DictationStreamSinkOptions
	ctx       context.Context
	cancel    context.CancelFunc

	inputMu sync.Mutex
	closed  bool
	pcm     chan []byte

	senderDone   chan struct{}
	receiverDone chan struct{}
	eventSeq     atomic.Uint64
	finalCount   atomic.Int64
	droppedPCM   atomic.Int64
}

func NewRecordingController(recorder AudioRecorder, submitter JobSubmitter, observer RecordingObserver, segmenterFactory SegmentCollectorFactory) *RecordingController {
	return &RecordingController{
		recorder:          recorder,
		submitter:         submitter,
		observer:          observer,
		segmenterFactory:  segmenterFactory,
		recordingMessage:  "Speak now",
		minPCMBytes:       DefaultMinPCMBytes,
		idleWatchInterval: defaultIdleWatchInterval,
		now:               time.Now,
	}
}

// SetFragmentSegments controls whether Stop() submits the VAD-derived segments
// as the STT source (true) or the full captured audio as a single submission
// (false, default). The segmenter still drives silence-based auto-stop either
// way; this only changes what audio is sent to transcription.
func (c *RecordingController) SetFragmentSegments(enabled bool) {
	if c == nil {
		return
	}
	c.fragmentSegments = enabled
}

// SetStreamSegments controls whether completed pause-bounded segments are
// submitted during recording instead of waiting until Stop(). The default is
// false. This is intended for live-ish dictation surfaces; hosts that need the
// strongest protection against VAD excision should keep the full-capture
// default.
func (c *RecordingController) SetStreamSegments(enabled bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamSegments = enabled
}

// SetDictationStream configures the optional provider-native live dictation
// path. Hosts can leave this unset to keep the public full-capture default.
func (c *RecordingController) SetDictationStream(provider DictationStreamProvider, sink DictationStreamSink) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.streamProvider = provider
	c.streamSink = sink
}

// SetIdleWatchInterval overrides the polling interval used by the
// silence-based auto-stop watcher. Tests use this to keep the unit
// tests fast (e.g. 5ms polling). Production should never touch this.
func (c *RecordingController) SetIdleWatchInterval(d time.Duration) {
	if c == nil || d <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.idleWatchInterval = d
}

func (c *RecordingController) IsRecording() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recording || c.stopping
}

// IsCapturing reports whether the microphone is physically open. Unlike
// [IsRecording] it excludes the stop/drain window, so hosts can
// distinguish "user is still dictating" (suppress post-capture UI
// states) from "capture ended, transcription in flight" (terminal
// states must display).
func (c *RecordingController) IsCapturing() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recording
}

// clearPCMHandlers detaches both the legacy and (when supported) the
// pool-aware PCM handler from the recorder.
func (c *RecordingController) clearPCMHandlers() {
	c.recorder.SetPCMHandler(nil)
	if pooled, ok := c.recorder.(PooledPCMRecorder); ok {
		pooled.SetPooledPCMHandler(nil)
	}
}

func (c *RecordingController) Start(opts RecordingStartOptions) error {
	if c == nil {
		return fmt.Errorf("speechkit: recording controller not configured")
	}

	var (
		collector SegmentCollector
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

	if opts.ProviderStream {
		nativeStream, err := c.startNativeDictationStream(sessionID, opts)
		if err != nil {
			c.onLog(fmt.Sprintf("Provider-stream dictation unavailable; falling back to segment-batch: %v", err), "warn")
		} else {
			c.mu.Lock()
			if c.sessionID == sessionID && c.recording {
				opts.StreamSegments = false
				c.current.StreamSegments = false
				c.nativeStream = nativeStream
			} else {
				nativeStream.cancel()
				_ = nativeStream.stream.Close()
			}
			c.mu.Unlock()
		}
	}

	c.mu.Lock()
	hasNativeStream := c.sessionID == sessionID && c.nativeStream != nil
	c.mu.Unlock()
	if collector != nil || hasNativeStream {
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

	// Arm the silence-based auto-stop watcher when both pieces are
	// present: the host provided a timeout + callback AND the collector
	// can report idle time. Wake-word/hold-to-talk paths leave the
	// timeout at zero and skip the watcher entirely.
	if opts.IdleTimeout > 0 && opts.OnIdleTimeoutCallback != nil {
		switch observer := collector.(type) {
		case AudioIdleObserver:
			c.startAudioIdleWatcher(sessionID, observer, opts.IdleTimeout, opts.OnIdleTimeoutCallback)
		case IdleObserver:
			c.startIdleWatcher(sessionID, observer, opts.IdleTimeout, opts.OnIdleTimeoutCallback)
		}
	}

	return nil
}

// startIdleWatcher spawns a goroutine that polls observer.IdleSince()
// every idleWatchInterval and fires callback once the gap to time.Now
// exceeds timeout. The watcher exits when the session changes (Stop()
// closes the per-session channel) or when it has already fired.
func (c *RecordingController) startIdleWatcher(sessionID uint64, observer IdleObserver, timeout time.Duration, callback func()) {
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
func (c *RecordingController) startAudioIdleWatcher(sessionID uint64, observer AudioIdleObserver, timeout time.Duration, callback func()) {
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

func (c *RecordingController) Stop(opts RecordingStopOptions) error {
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

	dur := PCMDurationSecs(pcm)
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
		segTotalSecs += PCMDurationSecs(segment.PCM)
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
	// single submission (job.Segments stays nil, so transcriptionSegments()
	// falls back to the full-PCM Submission). This eliminates every VAD-excision
	// word-loss path — onset clipping, low-energy word gating, short trailing
	// word drop, and multi-segment all-or-nothing failure — at no latency cost
	// for normal dictation (it was a single Deepgram call anyway).
	var jobSegments []Submission
	if c.fragmentSegments {
		if len(segments) == 0 {
			c.onLog("No speech segments detected, skipped", "error")
			c.onState("idle", "")
			return nil
		}
		jobSegments = make([]Submission, 0, len(segments))
		for _, segment := range segments {
			prefix := ""
			if segment.Paragraph {
				prefix = "\n\n"
			}
			jobSegments = append(jobSegments, Submission{
				PCM:                segment.PCM,
				WAV:                PCMToWAV(segment.PCM),
				DurationSecs:       PCMDurationSecs(segment.PCM),
				Language:           current.Language,
				Prefix:             prefix,
				RecordingSessionID: current.RecordingSessionID,
				CaptureChannel:     current.CaptureChannel,
			})
		}
	}

	capturedStartMs := elapsedCaptureMs(captureEpoch(current, startedAt), startedAt)
	if err := c.submitter.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:                pcm,
			WAV:                PCMToWAV(pcm),
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

func (c *RecordingController) drainAndSubmitReadySegments(sessionID uint64, current RecordingStartOptions, collector SegmentCollector) {
	readyCollector, ok := collector.(ReadySegmentCollector)
	if !ok {
		return
	}
	segments := readyCollector.DrainReadySegments()
	c.queueStreamSegments(sessionID, segments)
	if _, err := c.flushPendingStreamSegments(sessionID, current, "dictation segment", time.Time{}); err != nil {
		if errors.Is(err, ErrWorkerQueueFull) {
			c.onLog("STT queue busy; dictation segment retained for retry", "warn")
			return
		}
		c.onLog(fmt.Sprintf("Queue error: %v", err), "error")
	}
}

func (c *RecordingController) queueStreamSegments(sessionID uint64, segments []AudioSegment) {
	if c == nil || len(segments) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID != sessionID {
		return
	}
	c.streamPending = append(c.streamPending, cloneAudioSegments(segments)...)
}

func (c *RecordingController) flushPendingStreamSegments(sessionID uint64, current RecordingStartOptions, label string, retryUntil time.Time) (int, error) {
	if c == nil {
		return 0, nil
	}
	for {
		c.mu.Lock()
		if !c.streamFlush {
			c.streamFlush = true
			c.mu.Unlock()
			break
		}
		c.mu.Unlock()
		if retryUntil.IsZero() || !c.clockNow().Before(retryUntil) {
			return 0, nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer func() {
		c.mu.Lock()
		c.streamFlush = false
		c.mu.Unlock()
	}()

	c.mu.Lock()
	epoch := captureEpoch(current, c.startedAt)
	c.mu.Unlock()

	submitted := 0
	for {
		c.mu.Lock()
		if c.sessionID != sessionID {
			c.streamPending = nil
			c.mu.Unlock()
			return submitted, nil
		}
		if len(c.streamPending) == 0 {
			c.mu.Unlock()
			return submitted, nil
		}
		segment := cloneAudioSegments(c.streamPending[:1])[0]
		c.mu.Unlock()

		if len(segment.PCM) < c.minPCMBytes {
			c.dropFirstPendingStreamSegment(sessionID)
			continue
		}

		submission := submissionFromAudioSegment(segment, current, epoch, c.clockNow())
		sessionActive := true
		c.mu.Lock()
		if c.sessionID == sessionID {
			c.streamSegmentSeq++
			submission.SessionID = sessionID
			submission.SegmentID = c.streamSegmentSeq
			submission.SegmentFinal = true
		} else {
			sessionActive = false
		}
		c.mu.Unlock()
		if !sessionActive {
			return submitted, nil
		}
		if err := c.submitter.Submit(TranscriptionJob{
			Submission: submission,
			Target:     current.Target,
		}); err != nil {
			if errors.Is(err, ErrWorkerQueueFull) && !retryUntil.IsZero() && c.clockNow().Before(retryUntil) {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			return submitted, err
		}
		c.dropFirstPendingStreamSegment(sessionID)
		submitted++
		c.mu.Lock()
		if c.sessionID == sessionID {
			c.streamedCount++
		}
		c.mu.Unlock()
		c.onLog(fmt.Sprintf("Queued %s: %.1fs audio", label, submission.DurationSecs), "info")
	}
}

func (c *RecordingController) dropFirstPendingStreamSegment(sessionID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID != sessionID || len(c.streamPending) == 0 {
		return
	}
	c.streamPending[0].PCM = nil
	c.streamPending = c.streamPending[1:]
	if len(c.streamPending) == 0 {
		c.streamPending = nil
	}
}

// submissionFromAudioSegment converts one pause-bounded segment into a
// submission. capturedEnd is the wall clock the segment was closed at, which is
// the best available anchor for a VAD segment: its audio ends there, so the
// timeline offsets run backwards from it by the segment duration.
func submissionFromAudioSegment(segment AudioSegment, current RecordingStartOptions, epoch, capturedEnd time.Time) Submission {
	prefix := ""
	if segment.Paragraph {
		prefix = "\n\n"
	}
	durationSecs := segment.Duration.Seconds()
	if durationSecs <= 0 {
		durationSecs = PCMDurationSecs(segment.PCM)
	}
	endMs := elapsedCaptureMs(epoch, capturedEnd)
	startMs := endMs - int64(durationSecs*1000)
	if startMs < 0 {
		startMs = 0
	}
	return Submission{
		PCM:                append([]byte(nil), segment.PCM...),
		DurationSecs:       durationSecs,
		Language:           current.Language,
		Prefix:             prefix,
		QuickNote:          current.QuickNote,
		QuickNoteID:        current.QuickNoteID,
		RecordingSessionID: current.RecordingSessionID,
		CaptureChannel:     current.CaptureChannel,
		CapturedStartMs:    startMs,
		CapturedEndMs:      endMs,
	}
}

// captureEpoch resolves the wall clock a session's transcript timeline is
// measured from, defaulting to the start of this recording.
func captureEpoch(current RecordingStartOptions, startedAt time.Time) time.Time {
	if !current.CaptureEpoch.IsZero() {
		return current.CaptureEpoch
	}
	return startedAt
}

// elapsedCaptureMs places at on the timeline that starts at epoch. A zero epoch
// or timestamp means no timeline was requested, which keeps every offset at 0.
func elapsedCaptureMs(epoch, at time.Time) int64 {
	if epoch.IsZero() || at.IsZero() {
		return 0
	}
	ms := at.Sub(epoch).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

// Cancel stops the active recorder and discards the captured audio. Hosts use
// this when the user switches modes mid-capture; submitting the old buffer
// would deliver stale speech through the newly selected mode.
func (c *RecordingController) Cancel(opts RecordingCancelOptions) error {
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
	c.current = RecordingStartOptions{}
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

func (c *RecordingController) clockNow() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}
	return time.Now()
}

func isStaleCapturedAudio(capturedSecs float64, wall time.Duration) bool {
	if capturedSecs <= staleCaptureMinDuration.Seconds() || wall <= 0 {
		return false
	}
	maxAllowed := wall.Seconds() * staleCaptureMaxRatio
	if minAllowed := wall.Seconds() + staleCaptureGrace.Seconds(); maxAllowed < minAllowed {
		maxAllowed = minAllowed
	}
	return capturedSecs > maxAllowed
}

func (c *RecordingController) clearStopping() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopping = false
}

func (c *RecordingController) onState(status, text string) {
	if c.observer != nil {
		c.observer.OnState(status, text)
	}
}

func (c *RecordingController) onLog(message, kind string) {
	if c.observer != nil {
		c.observer.OnLog(message, kind)
	}
}

func (c *RecordingController) startNativeDictationStream(sessionID uint64, opts RecordingStartOptions) (*dictationStreamRuntime, error) {
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
	stream, err := provider.StartDictationStream(streamCtx, streamOpts, speaker.AudioFormat{
		Encoding:     speaker.AudioEncodingPCM16,
		SampleRateHz: 16000,
		Channels:     1,
	})
	if err != nil {
		cancel()
		return nil, err
	}
	runtime := &dictationStreamRuntime{
		sessionID: sessionID,
		stream:    stream,
		sink:      sink,
		sinkOpts: DictationStreamSinkOptions{
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
	c.onLog("Provider-stream dictation started", "info")
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
			RecordOutcome(r.ctx, OutcomePCMQueueDrop, errors.New("pcm queue full"),
				attribute.Int64("dropped_frames", dropped),
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
