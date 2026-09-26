package meeting

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

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
