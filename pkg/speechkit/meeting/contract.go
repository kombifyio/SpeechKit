package meeting

import (
	"errors"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	capturepkg "github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"
)

// Channels a meeting records from.
const (
	ChannelMicrophone = speechkit.CaptureChannelMicrophone
	ChannelSystem     = speechkit.CaptureChannelSystem
)

// State is the lifecycle of one meeting capture.
type State string

// Meeting states in lifecycle order: idle before capture, starting while the
// channels open (also during Resume), live while recording, paused after
// [Runtime.Pause], finalizing while [Runtime.Stop] drains in-flight
// transcription, and ended afterwards. [Snapshot.Active] counts starting
// through finalizing as running.
const (
	StateIdle       State = "idle"
	StateStarting   State = "starting"
	StateLive       State = "live"
	StatePaused     State = "paused"
	StateFinalizing State = "finalizing"
	StateEnded      State = "ended"
)

// ChannelState is the health of one capture channel. It is orthogonal to the
// meeting state: a meeting stays live when one of its two channels dies.
type ChannelState string

// Channel states: idle before capture and after Stop, recording while frames
// flow, paused after [Runtime.Pause], stalled when the device stopped
// delivering frames, and "error" (ChannelStateFailed) when the channel could
// not be opened or resumed or reported a device error. A failed channel stays
// down while the meeting continues on the other one.
const (
	ChannelStateIdle      ChannelState = "idle"
	ChannelStateRecording ChannelState = "recording"
	ChannelStatePaused    ChannelState = "paused"
	ChannelStateStalled   ChannelState = "stalled"
	ChannelStateFailed    ChannelState = "error"
)

var (
	// ErrMeetingActive is returned when a second meeting is started while one
	// is still running. Capture devices and the transcription worker are shared,
	// so meetings are deliberately exclusive.
	ErrMeetingActive = errors.New("meeting: a meeting is already being recorded")
	// ErrNoMeeting is returned by commands that need a running meeting.
	ErrNoMeeting = errors.New("meeting: no meeting is being recorded")
	// ErrNoChannels is returned when not a single capture channel could be
	// opened, which makes the meeting pointless rather than degraded.
	ErrNoChannels = errors.New("meeting: no capture channel could be opened")
)

// Pipeline is one capture channel: an audio session and the controller that
// turns its audio into transcripts. The host builds these, because device
// selection, provider wiring and the shared transcription worker are its
// concerns; the runtime only drives their lifecycle.
type Pipeline interface {
	// Channel names the capture source, one of the Channel* constants.
	Channel() string
	Start(opts speechkit.RecordingStartOptions) error
	Stop(opts speechkit.RecordingStopOptions) error
	// Events surfaces capture-device trouble (stalls, unplugs, driver errors)
	// so the runtime can mark this channel degraded without killing the other.
	Events() <-chan capturepkg.Event
	Close() error
}

// PipelineFactory opens the pipeline for one channel. Returning an error marks
// that channel unavailable; the meeting still runs on whatever opened.
type PipelineFactory func(channel string) (Pipeline, error)

// StartOptions describe the meeting to record.
type StartOptions struct {
	// SessionID is the persisted recording session this capture belongs to.
	SessionID int64
	// Title is used for host-visible capture labels only.
	Title    string
	Language string
	// Channels to record. Defaults to microphone plus system loopback.
	Channels []string
	// Recording carries the transcription settings the host resolved for this
	// meeting (provider stream, streaming segments, ...). The runtime fills in
	// the session, channel and epoch fields per channel.
	Recording speechkit.RecordingStartOptions
}

// Snapshot is the runtime's view of capture right now.
type Snapshot struct {
	SessionID       int64             `json:"sessionId"`
	State           State             `json:"state"`
	StartedAt       time.Time         `json:"startedAt,omitempty"`
	Channels        []ChannelSnapshot `json:"channels"`
	PendingSegments int               `json:"pendingSegments"`
	// Degraded is true when capture ended with accepted transcript segments
	// unresolved. PendingSegments reports how much transcript may be missing.
	Degraded bool `json:"degraded"`
}

// ChannelSnapshot is the runtime's view of one capture channel.
type ChannelSnapshot struct {
	Channel string       `json:"channel"`
	State   ChannelState `json:"state"`
	Message string       `json:"message,omitempty"`
}

// Active reports whether this snapshot describes capture that is still running.
func (s Snapshot) Active() bool {
	switch s.State {
	case StateStarting, StateLive, StatePaused, StateFinalizing:
		return true
	default:
		return false
	}
}

// Options configure a Runtime.
type Options struct {
	NewPipeline PipelineFactory
	// Log receives host-visible progress lines; kind is "info", "warn" or
	// "error", matching the desktop log levels.
	Log func(message, kind string)
	// Now is overridable for tests.
	Now func() time.Time
	// DrainTimeout bounds how long Stop waits for in-flight transcription to
	// land before the meeting is finished anyway.
	DrainTimeout time.Duration
	// DrainPoll is how often the drain wait re-checks. Tests shorten it.
	DrainPoll time.Duration
	// OnEnded fires once a meeting has finished and its transcript drain has
	// either settled or been marked degraded.
	//
	// It is a direct call rather than a snapshot subscription because finishing
	// a meeting — recording that it ended, writing it up — must not be
	// best-effort: subscribers can miss a broadcast, and a meeting that ends
	// without being recorded as ended looks to the user like it is still
	// running. It runs on the caller's goroutine, so slow work belongs in one
	// of its own.
	OnEnded func(sessionID int64)
}
