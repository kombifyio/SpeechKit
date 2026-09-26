package meeting

import (
	"context"
	"fmt"

	capturepkg "github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"
)

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
			case capturepkg.EventStalled:
				r.markChannelIfRecording(capture, channel, ChannelStateStalled, event.Message)
			case capturepkg.EventError:
				message := event.Message
				if message == "" && event.Err != nil {
					message = event.Err.Error()
				}
				r.markChannel(capture, channel, ChannelStateFailed, message)
				r.log(fmt.Sprintf("Meeting capture: %s channel error: %s", channel, message), "error")
			case capturepkg.EventStarted:
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
