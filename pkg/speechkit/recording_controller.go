package speechkit

import (
	"context"
	"time"
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

const DefaultMinPCMBytes = 3200

// Capture channels name the audio source behind a recording. A host that
// records one session from several sources at once — meeting capture takes the
// microphone and the system loopback in parallel — labels each controller so
// the resulting transcripts stay attributable.
const (
	CaptureChannelMicrophone = "mic"
	CaptureChannelSystem     = "system"
)

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
