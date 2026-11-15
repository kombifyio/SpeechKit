//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"
)

const (
	outputPCMBytesPerSecond = 24_000 * 2
	outputPCMFragmentBytes  = outputPCMBytesPerSecond / 10 // 100 ms
	outputPCMLead           = 200 * time.Millisecond
	// maxPendingOutputAhead is how much audio the pump may hold ahead of the
	// playhead before it reads no further provider event. Providers synthesize
	// faster than a device plays and their contract carries no flow-control
	// event, only a blocking Receive, so the pull sets the pace: a long answer
	// streams at the playhead instead of waiting whole in this process. The
	// caps below stay as the contract guard for a provider that hands over
	// more than one admitted message can hold.
	maxPendingOutputAhead      = 2 * time.Second
	maxPendingOutputAheadBytes = int(maxPendingOutputAhead/time.Second) * outputPCMBytesPerSecond
	maxPendingOutputBytes      = 4 << 20
	maxPendingControlBytes     = 128 << 10
	maxPendingOutputItems      = 512
)

type queuedProviderOutput struct {
	msg        *LiveMessage
	epoch      uint64
	bytes      int
	controls   int
	offset     int
	started    bool
	hadAudio   bool
	standalone string
}

// Only writePump owns this queue. It retains native batches while the pump
// paces them to the playhead, pulling another provider event only while the
// queue is under maxPendingOutputAheadBytes, and bounds both retained bytes
// and small-message count as a contract guard.
type providerOutputQueue struct {
	items               []*queuedProviderOutput
	bytes               int
	controls            int
	head                time.Time
	receivedReplyActive bool
}

// unplayedBytes is the audio the client has not been sent yet: the write pump
// pulls no further provider event while it holds the playback lead.
func (q *providerOutputQueue) unplayedBytes() int {
	unplayed := q.bytes
	if len(q.items) > 0 {
		// The head item's bytes count whole; only its already-relayed prefix
		// has reached the client.
		unplayed -= q.items[0].offset
	}
	return unplayed
}

func (q *providerOutputQueue) append(msg *LiveMessage, epoch uint64) bool {
	retained := len(msg.InputTranscript) + len(msg.OutputTranscript) +
		len(msg.InputSpeakerLabel) + len(msg.InputPersonID) + len(msg.InputDisplayName) +
		len(msg.ErrorCode) + len(msg.ErrorMessage) + len(msg.EventType)
	for _, event := range msg.EventTypes {
		retained += len(event)
	}
	metadata, err := json.Marshal(msg.ProviderMetadata)
	if err != nil {
		return false
	}
	calls, err := json.Marshal(msg.ToolCalls)
	if err != nil {
		return false
	}
	retained += len(metadata) + len(calls)
	if len(q.items) >= maxPendingOutputItems || len(msg.Audio) > maxPendingOutputBytes-q.bytes || retained > maxPendingControlBytes-q.controls {
		return false
	}
	owned := *msg
	// Cascaded chunks can be slices of a whole synthesized answer. Own only
	// the admitted bytes, rather than retaining that larger backing array.
	owned.Audio = append([]byte(nil), msg.Audio...)
	owned.EventTypes = inferServerEventTypes(msg)
	q.items = append(q.items, &queuedProviderOutput{
		msg: &owned, epoch: epoch, bytes: len(msg.Audio), controls: retained,
		hadAudio: len(msg.Audio) > 0, standalone: standaloneEventType(msg),
	})
	q.bytes += len(msg.Audio)
	q.controls += retained
	return true
}

func (q *providerOutputQueue) pop() {
	q.bytes -= q.items[0].bytes
	q.controls -= q.items[0].controls
	q.items[0] = nil
	q.items = q.items[1:]
}

func (q *providerOutputQueue) muteThrough(epoch uint64) {
	for _, item := range q.items {
		if item.epoch > epoch {
			continue
		}
		released := len(item.msg.Audio)
		item.msg.Audio = nil
		item.offset = 0
		item.bytes -= released
		q.bytes -= released
	}
	q.head = time.Time{}
}

func (q *providerOutputQueue) discardThrough(epoch uint64) {
	for len(q.items) > 0 && q.items[0].epoch <= epoch {
		q.pop()
	}
	q.head = time.Time{}
}

// The existing write fence protects cancellation epochs too. A queued
// fragment can never cross an interruption acknowledgement on the wire.
func (a *Adapter) writeOutputAudio(ctx context.Context, data []byte, epoch uint64) (sent, alive bool) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() || a.terminal || ctx.Err() != nil {
		return false, false
	}
	if epoch <= a.mutedThrough {
		return false, true
	}
	writeCtx, cancel := context.WithTimeout(ctx, transportWriteTimeout)
	defer cancel()
	if err := a.connWrite(writeCtx, websocket.MessageBinary, data); err != nil {
		return false, false
	}
	a.noteReplyAudio()
	if a.continuousDuplex {
		a.replyActivityAt = time.Now()
	}
	return true, true
}

// Relay one fragment without waiting for the whole answer. Provider controls
// can therefore preempt the next fragment in the existing write pump.
func (a *Adapter) relayQueuedOutput(ctx context.Context, q *providerOutputQueue) bool {
	item := q.items[0]
	if !item.started {
		a.writeMu.Lock()
		if item.hadAudio || item.msg.OutputTranscript != "" {
			a.outputEpoch = item.epoch
			a.replyActive.set(true)
		}
		item.started = true
		a.writeMu.Unlock()
	}
	if len(item.msg.Audio) > item.offset {
		if a.mediaBridge != nil {
			// Preserve the existing LiveKit PCM bridge and its SDK pacing;
			// WebSocket fragmentation applies only to the default transport.
			a.writeMu.Lock()
			muted := item.epoch <= a.mutedThrough
			a.writeMu.Unlock()
			if muted {
				q.muteThrough(item.epoch)
				return true
			}
			if err := a.mediaBridge.SendAudio(item.msg.Audio); err != nil {
				a.sendProviderError(ctx, "audio_downstream_failed", err, true)
				return false
			}
			item.offset = len(item.msg.Audio)
			return true
		}
		end := min(item.offset+outputPCMFragmentBytes, len(item.msg.Audio))
		sent, alive := a.writeOutputAudio(ctx, item.msg.Audio[item.offset:end], item.epoch)
		if !alive {
			return false
		}
		if sent {
			a.idle.Reset()
			now := time.Now()
			// The playhead advances by the audio's own duration, so a wake that
			// came late catches up instead of slowing the answer below the
			// device's pace. Only a debt past the lead the pump keeps for the
			// device (a dry gap between pieces, or a stalled pump) starts the
			// clock again from now, which bounds the catch-up burst.
			if q.head.IsZero() || q.head.Before(now.Add(-outputPCMLead)) {
				q.head = now
			}
			q.head = q.head.Add(time.Duration(end-item.offset) * time.Second / outputPCMBytesPerSecond)
			item.offset = end
		} else {
			q.muteThrough(item.epoch)
		}
		return true
	}
	a.forwardProviderOutput(ctx, item.msg, item.standalone)
	q.pop()
	return true
}
