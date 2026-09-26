package meeting

import "sort"

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
