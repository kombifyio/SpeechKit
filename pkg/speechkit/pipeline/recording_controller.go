package pipeline

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

const defaultIdleWatchInterval = 1 * time.Second

const (
	staleCaptureMinDuration = 30 * time.Second
	staleCaptureGrace       = 5 * time.Second
	staleCaptureMaxRatio    = 4.0
)

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

// ReadySegmentCollector is implemented by collectors that can hand completed
// pause-bounded segments to the transcription queue before recording stops.
type ReadySegmentCollector interface {
	speechkit.SegmentCollector
	DrainReadySegments() []speechkit.AudioSegment
}

// RecordingController manages the start/stop lifecycle of a single recording
// session and hands audio segments to the submission queue.
type RecordingController struct {
	recorder         speechkit.AudioRecorder
	submitter        speechkit.JobSubmitter
	observer         speechkit.RecordingObserver
	segmenterFactory speechkit.SegmentCollectorFactory
	streamProvider   speechkit.DictationStreamProvider
	streamSink       speechkit.DictationStreamSink
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
	current          speechkit.RecordingStartOptions
	collector        speechkit.SegmentCollector
	idleWatcherCh    chan struct{}
	streamedCount    int
	streamSegmentSeq uint64
	streamPending    []speechkit.AudioSegment
	streamFlush      bool
	nativeStream     *dictationStreamRuntime
}

type dictationStreamRuntime struct {
	sessionID uint64
	stream    speechkit.DictationStream
	sink      speechkit.DictationStreamSink
	sinkOpts  speechkit.DictationStreamSinkOptions
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

// NewRecordingController wires a controller to its recorder, the queue that
// receives finished jobs, an optional observer for status and log lines, and
// an optional factory that builds a fresh [speechkit.SegmentCollector] for
// every Start. Defaults: full-capture submission (no fragmenting, streaming or
// provider stream), the [speechkit.DefaultMinPCMBytes] minimum, and a
// one-second idle poll.
func NewRecordingController(recorder speechkit.AudioRecorder, submitter speechkit.JobSubmitter, observer speechkit.RecordingObserver, segmenterFactory speechkit.SegmentCollectorFactory) *RecordingController {
	return &RecordingController{
		recorder:          recorder,
		submitter:         submitter,
		observer:          observer,
		segmenterFactory:  segmenterFactory,
		recordingMessage:  "Speak now",
		minPCMBytes:       speechkit.DefaultMinPCMBytes,
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
func (c *RecordingController) SetDictationStream(provider speechkit.DictationStreamProvider, sink speechkit.DictationStreamSink) {
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

// IsRecording reports whether a session is active or still stopping (tail
// delay and submission); see [RecordingController.IsCapturing] for the
// microphone-open state alone. A nil controller reports false.
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
