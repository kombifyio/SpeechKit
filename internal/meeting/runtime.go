// Package meeting owns the runtime behind Meeting 2.0 capture.
//
// A meeting is recorded from two sources at once: the microphone carries the
// local speaker and the Windows system loopback carries everyone else on the
// call. The two are deliberately never mixed into one stream — mixing would
// need clock-drift compensation and echo cancellation, while two independent
// pipelines give the same result plus a free speaker split, and interleave on a
// shared wall clock afterwards.
//
// The runtime owns the lifecycle of those pipelines and is the single source of
// truth for what capture is doing right now. Hosts subscribe to snapshots
// rather than inferring state from the outcome of the last command.
package meeting

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// Channels a meeting records from.
const (
	ChannelMicrophone = speechkit.CaptureChannelMicrophone
	ChannelSystem     = speechkit.CaptureChannelSystem
)

// State is the lifecycle of one meeting capture.
type State string

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
	Events() <-chan audio.Event
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

const (
	defaultDrainTimeout = 20 * time.Second
	defaultDrainPoll    = 100 * time.Millisecond
	// drainStallWindow is how long the end of a meeting waits with nothing
	// landing before it accepts that the outstanding segments are not coming.
	drainStallWindow = 8 * time.Second
)

// Runtime drives meeting capture. The zero value is not usable; call New.
type Runtime struct {
	newPipeline  PipelineFactory
	log          func(string, string)
	now          func() time.Time
	drainTimeout time.Duration
	drainPoll    time.Duration
	onEnded      func(int64)

	mu          sync.Mutex
	active      *meetingCapture
	subscribers map[int]chan Snapshot
	nextSubID   int
}

type meetingCapture struct {
	sessionID int64
	title     string
	epoch     time.Time
	state     State
	recording speechkit.RecordingStartOptions
	pipelines []Pipeline
	channels  map[string]*ChannelSnapshot
	watchStop context.CancelFunc
	pending   int
	degraded  bool
}

// New creates a meeting runtime.
func New(opts Options) *Runtime {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Log == nil {
		opts.Log = func(string, string) {}
	}
	if opts.DrainTimeout <= 0 {
		opts.DrainTimeout = defaultDrainTimeout
	}
	if opts.DrainPoll <= 0 {
		opts.DrainPoll = defaultDrainPoll
	}
	return &Runtime{
		newPipeline:  opts.NewPipeline,
		log:          opts.Log,
		now:          opts.Now,
		drainTimeout: opts.DrainTimeout,
		drainPoll:    opts.DrainPoll,
		onEnded:      opts.OnEnded,
		subscribers:  map[int]chan Snapshot{},
	}
}

// SetEndedHook installs the terminal hook after construction, for hosts whose
// hook needs the runtime it belongs to.
func (r *Runtime) SetEndedHook(hook func(sessionID int64)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEnded = hook
}

// SetPipelineFactory installs the factory after construction. Hosts need this
// because the pipelines report their in-flight transcription back to the very
// runtime they belong to, so one of the two has to exist first.
func (r *Runtime) SetPipelineFactory(factory PipelineFactory) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.newPipeline = factory
}

// ActiveSessionID returns the recording session being captured, or 0.
func (r *Runtime) ActiveSessionID() int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		return 0
	}
	return r.active.sessionID
}

// Snapshot returns the current runtime view. The zero SessionID with StateIdle
// means nothing is being recorded.
func (r *Runtime) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{State: StateIdle}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

// SnapshotFor returns the runtime view for one session, or false when that
// session is not the one being recorded.
func (r *Runtime) SnapshotFor(sessionID int64) (Snapshot, bool) {
	if r == nil {
		return Snapshot{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.sessionID != sessionID {
		return Snapshot{}, false
	}
	return r.snapshotLocked(), true
}

// Subscribe returns a channel of snapshots plus a function that unsubscribes.
// Sends are non-blocking: a subscriber that stops reading misses intermediate
// states rather than stalling capture.
func (r *Runtime) Subscribe() (<-chan Snapshot, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextSubID
	r.nextSubID++
	ch := make(chan Snapshot, 8)
	r.subscribers[id] = ch
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if existing, ok := r.subscribers[id]; ok {
			delete(r.subscribers, id)
			close(existing)
		}
	}
}

// Start opens the capture channels and begins recording.
func (r *Runtime) Start(ctx context.Context, opts StartOptions) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, ErrNoChannels
	}
	if opts.SessionID <= 0 {
		return Snapshot{}, fmt.Errorf("meeting: a recording session is required")
	}
	channels := opts.Channels
	if len(channels) == 0 {
		channels = []string{ChannelMicrophone, ChannelSystem}
	}

	r.mu.Lock()
	if r.active != nil && r.active.state != StateEnded {
		r.mu.Unlock()
		return Snapshot{}, ErrMeetingActive
	}
	newPipeline := r.newPipeline
	if newPipeline == nil {
		r.mu.Unlock()
		return Snapshot{}, ErrNoChannels
	}
	epoch := r.now()
	capture := &meetingCapture{
		sessionID: opts.SessionID,
		title:     opts.Title,
		epoch:     epoch,
		state:     StateStarting,
		recording: opts.Recording,
		channels:  map[string]*ChannelSnapshot{},
	}
	for _, channel := range channels {
		capture.channels[channel] = &ChannelSnapshot{Channel: channel, State: ChannelStateIdle}
	}
	r.active = capture
	r.mu.Unlock()

	opened := make([]Pipeline, 0, len(channels))
	for _, channel := range channels {
		pipeline, err := newPipeline(channel)
		if err != nil {
			r.markChannel(capture, channel, ChannelStateFailed, err.Error())
			r.log(fmt.Sprintf("Meeting capture: %s channel unavailable: %v", channel, err), "warn")
			continue
		}
		opened = append(opened, pipeline)
	}
	if len(opened) == 0 {
		r.finishStartFailure(capture)
		return Snapshot{}, ErrNoChannels
	}

	r.mu.Lock()
	capture.pipelines = opened
	watchCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	capture.watchStop = cancel
	r.mu.Unlock()

	for _, pipeline := range opened {
		go r.watchPipeline(watchCtx, capture, pipeline)
	}

	started := 0
	for _, pipeline := range opened {
		if err := pipeline.Start(r.recordingOptions(capture, pipeline.Channel())); err != nil {
			r.markChannel(capture, pipeline.Channel(), ChannelStateFailed, err.Error())
			r.log(fmt.Sprintf("Meeting capture: %s channel failed to start: %v", pipeline.Channel(), err), "warn")
			continue
		}
		r.markChannel(capture, pipeline.Channel(), ChannelStateRecording, "")
		started++
	}
	if started == 0 {
		r.finishStartFailure(capture)
		return Snapshot{}, ErrNoChannels
	}

	r.setState(capture, StateLive)
	r.log(fmt.Sprintf("Meeting capture started on %d channel(s)", started), "info")
	return r.Snapshot(), nil
}

// Pause stops capture on every channel but keeps the meeting and its shared
// timeline open, so Resume continues the same recording rather than a new one.
func (r *Runtime) Pause() (Snapshot, error) {
	capture, err := r.beginTransition(StatePaused, StateLive)
	if err != nil {
		return Snapshot{}, err
	}
	r.stopPipelines(capture, ChannelStatePaused)
	r.log("Meeting capture paused", "info")
	return r.Snapshot(), nil
}

// Resume restarts capture on the channels that are still healthy.
func (r *Runtime) Resume() (Snapshot, error) {
	capture, err := r.beginTransition(StateStarting, StatePaused)
	if err != nil {
		return Snapshot{}, err
	}
	resumed := 0
	for _, pipeline := range r.pipelinesOf(capture) {
		if err := pipeline.Start(r.recordingOptions(capture, pipeline.Channel())); err != nil {
			r.markChannel(capture, pipeline.Channel(), ChannelStateFailed, err.Error())
			r.log(fmt.Sprintf("Meeting capture: %s channel failed to resume: %v", pipeline.Channel(), err), "warn")
			continue
		}
		r.markChannel(capture, pipeline.Channel(), ChannelStateRecording, "")
		resumed++
	}
	if resumed == 0 {
		r.setState(capture, StatePaused)
		return r.Snapshot(), ErrNoChannels
	}
	r.setState(capture, StateLive)
	r.log(fmt.Sprintf("Meeting capture resumed on %d channel(s)", resumed), "info")
	return r.Snapshot(), nil
}

// Stop ends capture, waits for in-flight transcription to land, and closes the
// capture devices. The wait is bounded: a provider that never answers delays
// the meeting's end by DrainTimeout, it does not hang it.
func (r *Runtime) Stop(ctx context.Context) (Snapshot, error) {
	capture, err := r.beginTransition(StateFinalizing, StateLive, StatePaused)
	if err != nil {
		return Snapshot{}, err
	}
	r.stopPipelines(capture, ChannelStateIdle)
	r.waitForDrain(ctx, capture)
	r.closeCapture(capture)
	r.setState(capture, StateEnded)
	r.mu.Lock()
	degraded, pending := capture.degraded, capture.pending
	r.mu.Unlock()
	if degraded {
		r.log(fmt.Sprintf("Meeting capture finished with a partial transcript (%d unresolved segment(s))", pending), "warn")
	} else {
		r.log("Meeting capture finished", "info")
	}

	snapshot := r.Snapshot()
	r.mu.Lock()
	if r.active == capture {
		r.active = nil
	}
	onEnded := r.onEnded
	r.mu.Unlock()
	if onEnded != nil {
		onEnded(capture.sessionID)
	}
	return snapshot, nil
}

// NoteSegmentSubmitted records that a segment of this meeting entered the
// transcription queue, so Stop knows what it is waiting for.
func (r *Runtime) NoteSegmentSubmitted(sessionID int64) {
	r.adjustPending(sessionID, 1)
}

// NoteSegmentCommitted records that a submitted segment has been persisted.
func (r *Runtime) NoteSegmentCommitted(sessionID int64) {
	r.adjustPending(sessionID, -1)
}

func (r *Runtime) adjustPending(sessionID int64, delta int) {
	if r == nil || sessionID <= 0 {
		return
	}
	r.mu.Lock()
	if r.active == nil || r.active.sessionID != sessionID {
		r.mu.Unlock()
		return
	}
	r.active.pending += delta
	if r.active.pending < 0 {
		r.active.pending = 0
	}
	snapshot := r.snapshotLocked()
	subscribers := r.subscriberList()
	r.mu.Unlock()
	broadcast(subscribers, snapshot)
}

// beginTransition atomically moves the active capture from one of the allowed
// states into next. The check and the transition share one lock acquisition,
// so two concurrent commands cannot both pass the same state check: the second
// caller sees the state the first one already set and gets ErrNoMeeting
// instead of, say, finalizing a meeting twice.
func (r *Runtime) beginTransition(next State, allowed ...State) (*meetingCapture, error) {
	if r == nil {
		return nil, ErrNoMeeting
	}
	r.mu.Lock()
	if r.active == nil {
		r.mu.Unlock()
		return nil, ErrNoMeeting
	}
	capture := r.active
	permitted := false
	for _, state := range allowed {
		if capture.state == state {
			permitted = true
			break
		}
	}
	if !permitted {
		current := capture.state
		r.mu.Unlock()
		return nil, fmt.Errorf("%w: capture is %s", ErrNoMeeting, current)
	}
	capture.state = next
	snapshot := r.snapshotForLocked(capture)
	subscribers := r.subscriberList()
	r.mu.Unlock()
	broadcast(subscribers, snapshot)
	return capture, nil
}

// recordingOptions stamps the host's transcription settings with the identity
// this channel records under. Every channel of one meeting shares the epoch, so
// their transcripts land on a single timeline.
func (r *Runtime) recordingOptions(capture *meetingCapture, channel string) speechkit.RecordingStartOptions {
	opts := capture.recording
	opts.RecordingSessionID = capture.sessionID
	opts.CaptureChannel = channel
	opts.CaptureEpoch = capture.epoch
	if opts.Label == "" {
		opts.Label = captureLabel(capture.title, channel)
	}
	return opts
}

func captureLabel(title, channel string) string {
	if title == "" {
		title = "Meeting"
	}
	switch channel {
	case ChannelSystem:
		return title + " (system audio)"
	case ChannelMicrophone:
		return title + " (microphone)"
	default:
		return title
	}
}

func (r *Runtime) stopPipelines(capture *meetingCapture, state ChannelState) {
	for _, pipeline := range r.pipelinesOf(capture) {
		if err := pipeline.Stop(speechkit.RecordingStopOptions{
			Label: captureLabel(capture.title, pipeline.Channel()),
		}); err != nil {
			r.markChannel(capture, pipeline.Channel(), ChannelStateFailed, err.Error())
			r.log(fmt.Sprintf("Meeting capture: %s channel stop failed: %v", pipeline.Channel(), err), "warn")
			continue
		}
		r.markChannelIfHealthy(capture, pipeline.Channel(), state)
	}
}

// waitForDrain blocks until every submitted segment has been committed, the
// caller gives up, or waiting stops being worthwhile.
//
// Two bounds, because a segment can go missing rather than slow: a transcript
// the worker drops never reports back at all, and waiting the full timeout for
// it would stall the end of every meeting it happens in. So the wait also gives
// up once nothing has landed for drainStallWindow, while a provider that keeps
// delivering gets the whole timeout.
func (r *Runtime) waitForDrain(ctx context.Context, capture *meetingCapture) {
	deadline := r.now().Add(r.drainTimeout)
	lastProgress := r.now()
	previous := -1
	for {
		r.mu.Lock()
		pending := capture.pending
		r.mu.Unlock()
		if pending <= 0 {
			return
		}
		now := r.now()
		if pending != previous {
			previous = pending
			lastProgress = now
		}
		stalled := now.Sub(lastProgress) >= drainStallWindow
		if stalled || !now.Before(deadline) {
			reason := "still transcribing when the meeting ended"
			if stalled {
				reason = "never came back from transcription"
			}
			r.log(fmt.Sprintf("Meeting capture: %d segment(s) %s", pending, reason), "warn")
			r.markDrainDegraded(capture)
			return
		}
		select {
		case <-ctx.Done():
			r.markDrainDegraded(capture)
			return
		case <-time.After(r.drainPoll):
		}
	}
}

func (r *Runtime) markDrainDegraded(capture *meetingCapture) {
	r.mu.Lock()
	capture.degraded = capture.pending > 0
	r.mu.Unlock()
}

func (r *Runtime) closeCapture(capture *meetingCapture) {
	r.mu.Lock()
	stop := capture.watchStop
	capture.watchStop = nil
	pipelines := capture.pipelines
	capture.pipelines = nil
	r.mu.Unlock()

	if stop != nil {
		stop()
	}
	for _, pipeline := range pipelines {
		if err := pipeline.Close(); err != nil {
			r.log(fmt.Sprintf("Meeting capture: closing the %s channel failed: %v", pipeline.Channel(), err), "warn")
		}
	}
}

func (r *Runtime) finishStartFailure(capture *meetingCapture) {
	r.closeCapture(capture)
	r.setState(capture, StateEnded)
	r.mu.Lock()
	if r.active == capture {
		r.active = nil
	}
	r.mu.Unlock()
}

// watchPipeline folds capture-device trouble into channel state. A stalled or
// dead device degrades its own channel only: the meeting keeps recording
// whatever still works, which is the whole reason the channels are separate.
func (r *Runtime) watchPipeline(ctx context.Context, capture *meetingCapture, pipeline Pipeline) {
	events := pipeline.Events()
	if events == nil {
		return
	}
	channel := pipeline.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			switch event.Type {
			case audio.EventStalled:
				r.markChannelIfRecording(capture, channel, ChannelStateStalled, event.Message)
			case audio.EventError:
				message := event.Message
				if message == "" && event.Err != nil {
					message = event.Err.Error()
				}
				r.markChannel(capture, channel, ChannelStateFailed, message)
				r.log(fmt.Sprintf("Meeting capture: %s channel error: %s", channel, message), "error")
			case audio.EventStarted:
				r.markChannelIfRecording(capture, channel, ChannelStateRecording, "")
			}
		}
	}
}

func (r *Runtime) markChannel(capture *meetingCapture, channel string, state ChannelState, message string) {
	r.mu.Lock()
	entry, ok := capture.channels[channel]
	if !ok {
		entry = &ChannelSnapshot{Channel: channel}
		capture.channels[channel] = entry
	}
	entry.State = state
	entry.Message = message
	snapshot := r.snapshotForLocked(capture)
	subscribers := r.subscriberList()
	r.mu.Unlock()
	broadcast(subscribers, snapshot)
}

// markChannelIfHealthy leaves a failed channel failed. A stop that succeeds on
// a channel whose device already died must not advertise it as ready again.
func (r *Runtime) markChannelIfHealthy(capture *meetingCapture, channel string, state ChannelState) {
	r.mu.Lock()
	entry, ok := capture.channels[channel]
	healthy := ok && entry.State != ChannelStateFailed
	if healthy {
		entry.State = state
		entry.Message = ""
	}
	snapshot := r.snapshotForLocked(capture)
	subscribers := r.subscriberList()
	r.mu.Unlock()
	if healthy {
		broadcast(subscribers, snapshot)
	}
}

// markChannelIfRecording applies device-level events only to a channel that is
// supposed to be capturing, so a late event cannot revive a paused meeting.
func (r *Runtime) markChannelIfRecording(capture *meetingCapture, channel string, state ChannelState, message string) {
	r.mu.Lock()
	entry, ok := capture.channels[channel]
	applies := ok && (entry.State == ChannelStateRecording || entry.State == ChannelStateStalled)
	if applies {
		entry.State = state
		entry.Message = message
	}
	snapshot := r.snapshotForLocked(capture)
	subscribers := r.subscriberList()
	r.mu.Unlock()
	if applies {
		broadcast(subscribers, snapshot)
	}
}

func (r *Runtime) setState(capture *meetingCapture, state State) {
	r.mu.Lock()
	capture.state = state
	snapshot := r.snapshotForLocked(capture)
	subscribers := r.subscriberList()
	r.mu.Unlock()
	broadcast(subscribers, snapshot)
}

func (r *Runtime) pipelinesOf(capture *meetingCapture) []Pipeline {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Pipeline(nil), capture.pipelines...)
}

func (r *Runtime) snapshotLocked() Snapshot {
	if r.active == nil {
		return Snapshot{State: StateIdle, Channels: []ChannelSnapshot{}}
	}
	return r.snapshotForLocked(r.active)
}

func (r *Runtime) snapshotForLocked(capture *meetingCapture) Snapshot {
	channels := make([]ChannelSnapshot, 0, len(capture.channels))
	for _, entry := range capture.channels {
		channels = append(channels, *entry)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].Channel < channels[j].Channel })
	return Snapshot{
		SessionID:       capture.sessionID,
		State:           capture.state,
		StartedAt:       capture.epoch,
		Channels:        channels,
		PendingSegments: capture.pending,
		Degraded:        capture.degraded,
	}
}

func (r *Runtime) subscriberList() []chan Snapshot {
	out := make([]chan Snapshot, 0, len(r.subscribers))
	for _, ch := range r.subscribers {
		out = append(out, ch)
	}
	return out
}

func broadcast(subscribers []chan Snapshot, snapshot Snapshot) {
	for _, ch := range subscribers {
		select {
		case ch <- snapshot:
		default:
		}
	}
}
