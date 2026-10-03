//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

func (a *Adapter) readPump(ctx context.Context, done chan<- struct{}) {
	defer func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	for {
		// Canceling coder/websocket's Read context closes the socket immediately.
		// Keep transport reads alive until Run has written the terminal frame;
		// the session context still gates every admitted upstream operation.
		typ, data, err := a.Conn.Read(context.WithoutCancel(ctx))
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		a.idle.Reset()
		switch typ {
		case websocket.MessageBinary:
			if a.mediaTransport == MediaTransportLiveKit {
				a.sendError(ctx, "audio_transport_mismatch", "binary audio frames are disabled when media_transport=livekit")
				continue
			}
			if err := a.Provider.SendAudio(data); err != nil {
				a.sendProviderError(ctx, "audio_upstream_failed", err, true)
				return
			}
		case websocket.MessageText:
			var env envelope
			if err := json.Unmarshal(data, &env); err != nil {
				a.sendError(ctx, "invalid_frame", err.Error())
				continue
			}
			switch env.Type {
			case MsgAudioEnd:
				if err := a.Provider.SendAudioStreamEnd(); err != nil {
					a.sendProviderError(ctx, "audio_upstream_failed", err, true)
					return
				}
			case MsgText:
				var tx TextFrame
				if err := json.Unmarshal(data, &tx); err == nil {
					if err := a.Provider.SendText(tx.Text); err != nil {
						a.sendProviderError(ctx, "text_upstream_failed", err, true)
						return
					}
				}
			case MsgToolResponse:
				var tr ToolResponseFrame
				if err := json.Unmarshal(data, &tr); err != nil {
					a.sendError(ctx, "invalid_frame", err.Error())
					continue
				}
				responder, ok := a.Provider.(LiveToolResponder)
				if !ok {
					a.sendError(ctx, "tool_response_unsupported", "provider does not accept tool responses")
					continue
				}
				if err := responder.SendToolResponse(tr); err != nil {
					a.sendProviderError(ctx, "tool_response_failed", err, false)
				}
			case MsgCancel:
				a.handleCancel(ctx)
			case MsgPing:
				a.sendJSON(ctx, PongFrame{Type: MsgPong})
			case MsgStop:
				a.sendJSON(ctx, SessionEndFrame{
					Type:             MsgSessionEnd,
					EventFrameFields: a.eventFrameFields(nil, EventSessionEnd),
					Reason:           "client",
				})
				return
			case MsgAdvanceStep:
				var advance AdvanceStepFrame
				if err := json.Unmarshal(data, &advance); err != nil {
					a.sendError(ctx, "invalid_frame", err.Error())
					continue
				}
				if advance.Reason == "" {
					advance.Reason = "client"
				}
				if err := a.advanceWorkflowStep(ctx, advance); err != nil {
					slog.Warn("voiceagent: advance_step failed", "err", err)
					a.sendError(ctx, "advance_step_failed", err.Error())
				}
			default:
				// Unknown frames are tolerated (forward-compat); we log
				// and ignore so a newer client doesn't break older servers.
				slog.Debug("voiceagent: unknown client frame", "type", env.Type)
			}
		}
	}
}

// handleCancel implements the client `cancel` frame (see MsgCancel):
// idempotently stop relaying the CURRENT agent reply's downlink audio, ask
// the provider to abort generation where its protocol supports it, and always
// ack with an `interrupted` event frame so client playback state converges.
// A cancel while nothing is streaming is a pure ack — suppression stays
// unarmed so the next reply is unaffected.
func (a *Adapter) handleCancel(ctx context.Context) {
	a.writeMu.Lock()
	a.settleDuplexReplyLocked(time.Now())
	active := a.replyActive.get()
	if active {
		a.suppressDownlink.set(true)
		a.mutedThrough = max(a.mutedThrough, a.outputEpoch)
	}
	fields := a.eventFrameFields(nil, EventInterrupted)
	fields.ProviderMetadata = map[string]any{"reason": "client_cancel"}
	a.writeJSONLocked(ctx, InterruptedFrame{
		Type:             MsgInterrupted,
		EventFrameFields: fields,
	})
	a.writeMu.Unlock()
	if active {
		if canceller, ok := a.Provider.(LiveResponseCanceller); ok {
			if err := canceller.CancelResponse(); err != nil {
				slog.Warn("voiceagent: provider response cancel failed; suppressing downlink until turn end",
					"session_id", a.Session.ID)
			}
		}
	}
}

type providerReceiveResult struct {
	msg *LiveMessage
	err error
}

// Receive has one owner. The existing Adapter interprets every provider event;
// this capacity-one handoff only separates network reads from PCM scheduling.
func (a *Adapter) providerReadPump(ctx context.Context, results chan<- providerReceiveResult) {
	for {
		if ctx.Err() != nil {
			return
		}
		msg, err := a.Provider.Receive(ctx)
		select {
		case results <- providerReceiveResult{msg: msg, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func (a *Adapter) writePump(ctx context.Context, done chan<- struct{}, results <-chan providerReceiveResult) {
	var pending providerOutputQueue
	defer func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		// Prioritize an already-received control over another PCM fragment.
		select {
		case result := <-results:
			if !a.admitProviderOutput(ctx, result, &pending) {
				return
			}
			continue
		default:
		}
		a.writeMu.Lock()
		muted := a.mutedThrough
		a.writeMu.Unlock()
		if len(pending.items) > 0 && pending.items[0].epoch <= muted && len(pending.items[0].msg.Audio) > 0 {
			pending.muteThrough(muted)
		}
		var tick <-chan time.Time
		var timer *time.Timer
		if len(pending.items) > 0 {
			item := pending.items[0]
			// A zero playhead is immediately writable. time.Until(zero)
			// saturates at MinInt64; subtracting the lead would wrap positive.
			var wait time.Duration
			if !pending.head.IsZero() {
				wait = time.Until(pending.head) - outputPCMLead
			}
			if len(item.msg.Audio) <= item.offset || wait <= 0 || a.mediaBridge != nil {
				if !a.relayQueuedOutput(ctx, &pending) {
					return
				}
				continue
			}
			// Client cancel uses the independent write fence; provider results wake
			// this select immediately instead of waiting for an audio deadline.
			timer = time.NewTimer(min(wait, 20*time.Millisecond))
			tick = timer.C
		}
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case result := <-results:
			if timer != nil {
				timer.Stop()
			}
			if !a.admitProviderOutput(ctx, result, &pending) {
				return
			}
		case <-tick:
		}
	}
}

func (a *Adapter) admitProviderOutput(ctx context.Context, result providerReceiveResult, pending *providerOutputQueue) bool {
	msg := result.msg
	if result.err != nil {
		if ctx.Err() == nil {
			a.sendProviderError(ctx, "provider_receive_failed", result.err, true)
		}
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if msg == nil {
		return true
	}
	a.idle.Reset()
	a.writeMu.Lock()
	if !msg.Interrupted && (len(msg.Audio) > 0 || msg.OutputTranscript != "") {
		pending.receivedReplyActive = true
		if a.settleDuplexReplyLocked(time.Now()) {
			pending.head = time.Time{}
		}
		if a.continuousDuplex {
			a.replyActivityAt = time.Now()
		}
		if !a.replyActive.get() {
			// Cancellation also applies before the first admitted fragment is
			// audible; scheduling must not turn a queued reply into an idle ack.
			a.replyActive.set(true)
			a.outputEpoch = a.receiveEpoch
		}
	}
	epoch := a.receiveEpoch
	interruptEpoch := epoch
	if !pending.receivedReplyActive && interruptEpoch > 0 {
		// Speech-start while idle must not mute the next provider reply. A
		// previously received Done may still have an older paced tail to flush.
		interruptEpoch--
	}
	if msg.Done || msg.ErrorCode != "" {
		pending.receivedReplyActive = false
		a.receiveEpoch++
	}
	a.writeMu.Unlock()
	if msg.ErrorCode != "" {
		// A failed reply has no queued tool effects or stale Done left to
		// execute after the error has already been delivered.
		pending.discardThrough(epoch)
		a.writeMu.Lock()
		a.mutedThrough = max(a.mutedThrough, epoch)
		a.replyActive.set(false)
		a.suppressDownlink.set(false)
		a.writeMu.Unlock()
		code := cascaded.SafeFailureCode(msg.ErrorCode)
		fatal := code == "auth_expired" || code == "auth_required" || code == "permission_denied" || code == "capability_lease_denied" || code == "internal_panic"
		a.sendErrorWithFatal(ctx, code, cascaded.FailureMessage(code), fatal)
		if fatal {
			if code == "auth_expired" {
				a.sendSessionEnd(ctx, "authorization_expired")
			}
			return false
		}
		return true
	}
	if msg.GoAway {
		a.sendSessionEnd(ctx, "go_away")
		return false
	}
	if msg.Interrupted {
		pending.muteThrough(interruptEpoch)
		a.writeMu.Lock()
		a.mutedThrough = max(a.mutedThrough, interruptEpoch)
		if a.replyActive.get() {
			a.suppressDownlink.set(true)
		}
		a.writeJSONLocked(ctx, InterruptedFrame{Type: MsgInterrupted, EventFrameFields: a.eventFrameFields(msg, EventInterrupted)})
		a.writeMu.Unlock()
		interrupted := *msg
		interrupted.Audio = nil
		msg = &interrupted
	}
	if len(msg.Audio)%2 != 0 || len(msg.Audio) > maxPendingOutputBytes {
		a.sendFatalError(ctx, "audio_downstream_failed", "The voice provider returned invalid audio.")
		return false
	}
	if !pending.append(msg, epoch) {
		// Fail rather than block Receive behind output backpressure or silently
		// discard PCM. The existing terminal fence seals all queued output.
		a.sendFatalError(ctx, "audio_downstream_failed", "The voice provider exceeded the output buffer limit.")
		return false
	}
	return true
}

// Nonterminal fields follow their audio, including the real ordered Done.
// Interrupted/error/GoAway are interpreted immediately at receipt instead.
func (a *Adapter) forwardProviderOutput(ctx context.Context, msg *LiveMessage, standalone string) {
	if msg.InputTranscript != "" {
		eventType := EventInputPartial
		if msg.InputTranscriptDone {
			eventType = EventInputFinal
		}
		a.sendJSON(ctx, TranscriptFrame{
			Type:              MsgInputTranscript,
			EventFrameFields:  a.eventFrameFields(msg, eventType),
			Text:              msg.InputTranscript,
			Done:              msg.InputTranscriptDone,
			SpeakerLabel:      msg.InputSpeakerLabel,
			PersonID:          msg.InputPersonID,
			DisplayName:       msg.InputDisplayName,
			SpeakerConfidence: msg.InputSpeakerConfidence,
		})
		if msg.InputTranscriptDone {
			a.recordUserTurn(ctx)
		}
	}
	if msg.OutputTranscript != "" {
		a.sendJSON(ctx, TranscriptFrame{
			Type:             MsgOutputTranscript,
			EventFrameFields: a.eventFrameFields(msg, EventOutputText),
			Text:             msg.OutputTranscript,
			Done:             msg.OutputTranscriptDone,
		})
	}
	for _, call := range msg.ToolCalls {
		if _, claimed := a.bridgeTools[call.Name]; claimed {
			// Server-executed tool: the client still receives the
			// tool_call frame for UI transparency (tagged
			// execution=server) but must not answer it — the adapter
			// resolves it against the tool router asynchronously.
			a.dispatchBridgeToolCall(ctx, msg, call)
			continue
		}
		a.sendJSON(ctx, ToolCallFrame{
			Type:             MsgToolCall,
			EventFrameFields: a.eventFrameFields(msg, EventToolCall),
			ID:               call.ID,
			Name:             call.Name,
			Args:             call.Args,
		})
	}
	if msg.Done {
		a.writeMu.Lock()
		a.replyActive.set(false)
		a.suppressDownlink.set(false)
		a.writeMu.Unlock()
	}
	if standalone != "" {
		a.sendJSON(ctx, EventFrame{Type: MsgEvent, EventFrameFields: a.eventFrameFields(msg, standalone)})
	}
}
