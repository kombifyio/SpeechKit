package speechkit

import (
	"fmt"
	"sync"
	"time"
)

// IdleObserver is implemented by SegmentCollectors that want to drive
// silence-based auto-stop. Returning the zero value tells the watcher
// "user is actively speaking; reset the timer." Returning a non-zero
// time tells the watcher "user has been silent since T."
type IdleObserver interface {
	IdleSince() time.Time
}

const defaultIdleWatchInterval = 1 * time.Second

const DefaultMinPCMBytes = 3200

// AudioRecorder is the hardware abstraction for microphone capture.
type AudioRecorder interface {
	Start() error
	Stop() ([]byte, error)
	SetPCMHandler(func([]byte))
}

// SegmentCollector accumulates real-time PCM frames and splits them into
// dictation segments when recording stops.
type SegmentCollector interface {
	FeedPCM([]byte) error
	CollectStopSegments(fullPCM []byte) ([]AudioSegment, error)
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
	Label       string
	Target      any
	Language    string
	QuickNote   bool
	QuickNoteID int64
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
	recordingMessage string
	minPCMBytes      int
	// fragmentSegments, when true, submits the VAD-derived audio segments as the
	// STT source (legacy parallel-segment path). When false (the default) the
	// full captured audio is transcribed as a single submission so speech the
	// crude RMS VAD mis-classified cannot be excised before STT — this is the
	// dropped-words fix; the segmenter still drives silence-based auto-stop.
	fragmentSegments bool
	// idleWatchInterval is how often the idle watcher polls the
	// IdleObserver. Defaults to 1s — overridable from tests.
	idleWatchInterval time.Duration

	mu            sync.Mutex
	recording     bool
	stopping      bool
	sessionID     uint64
	current       RecordingStartOptions
	collector     SegmentCollector
	idleWatcherCh chan struct{}
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
	c.current = opts
	c.collector = nil
	c.sessionID++
	sessionID = c.sessionID

	if c.segmenterFactory != nil {
		collector = c.segmenterFactory()
		c.collector = collector
	}
	c.mu.Unlock()

	if collector != nil {
		c.recorder.SetPCMHandler(func(pcm []byte) {
			c.mu.Lock()
			if c.sessionID != sessionID || !c.recording {
				c.mu.Unlock()
				return
			}
			activeCollector := c.collector
			c.mu.Unlock()
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
				c.recorder.SetPCMHandler(nil)
			}
		})
	} else {
		c.recorder.SetPCMHandler(nil)
	}

	if err := c.recorder.Start(); err != nil {
		c.mu.Lock()
		if c.sessionID == sessionID {
			c.recording = false
			c.collector = nil
		}
		c.mu.Unlock()
		c.recorder.SetPCMHandler(nil)
		c.onLog(fmt.Sprintf("Capture error: %v", err), "error")
		c.onState("idle", "")
		return err
	}

	c.onState("recording", c.recordingMessage)
	if opts.Label != "" {
		c.onLog(opts.Label, "info")
	}

	// Arm the silence-based auto-stop watcher when both pieces are
	// present: the host provided a timeout + callback AND the collector
	// can report idle time. Wake-word/hold-to-talk paths leave the
	// timeout at zero and skip the watcher entirely.
	if opts.IdleTimeout > 0 && opts.OnIdleTimeoutCallback != nil {
		if observer, ok := collector.(IdleObserver); ok {
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
	c.collector = nil
	c.mu.Unlock()

	c.recorder.SetPCMHandler(nil)
	pcm, stopErr := c.recorder.Stop()
	if stopErr != nil {
		c.onLog(fmt.Sprintf("Capture stop warning: %v", stopErr), "warn")
	}

	dur := PCMDurationSecs(pcm)
	c.onLog(fmt.Sprintf("%s: %.1fs audio", opts.Label, dur), "info")

	if len(pcm) < c.minPCMBytes {
		c.onLog("Too short, skipped", "error")
		c.onState("idle", "")
		c.collector = nil
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
				PCM:          segment.PCM,
				WAV:          PCMToWAV(segment.PCM),
				DurationSecs: PCMDurationSecs(segment.PCM),
				Language:     current.Language,
				Prefix:       prefix,
			})
		}
	}

	if err := c.submitter.Submit(TranscriptionJob{
		Submission: Submission{
			PCM:          pcm,
			WAV:          PCMToWAV(pcm),
			DurationSecs: dur,
			Language:     current.Language,
			QuickNote:    current.QuickNote,
			QuickNoteID:  current.QuickNoteID,
		},
		Segments: jobSegments,
		Target:   current.Target,
	}); err != nil {
		c.onLog(fmt.Sprintf("Queue error: %v", err), "error")
		c.onState("idle", "")
		return err
	}

	return nil
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
	c.mu.Unlock()

	c.recorder.SetPCMHandler(nil)
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
