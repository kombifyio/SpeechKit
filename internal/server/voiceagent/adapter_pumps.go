//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/coder/websocket"
)

func (a *Adapter) readPump(ctx context.Context, done chan<- struct{}) {
	defer func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	for {
		typ, data, err := a.Conn.Read(ctx)
		if err != nil {
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
				slog.Warn("voiceagent: send audio failed", "err", err)
				a.sendError(ctx, "audio_upstream_failed", err.Error())
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
					slog.Warn("voiceagent: audio-end upstream failed", "err", err)
				}
			case MsgText:
				var tx TextFrame
				if err := json.Unmarshal(data, &tx); err == nil {
					if err := a.Provider.SendText(tx.Text); err != nil {
						slog.Warn("voiceagent: text upstream failed", "err", err)
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
					slog.Warn("voiceagent: tool response upstream failed", "err", err)
					a.sendError(ctx, "tool_response_failed", err.Error())
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
	if a.replyActive.get() {
		a.suppressDownlink.set(true)
		if canceller, ok := a.Provider.(LiveResponseCanceller); ok {
			if err := canceller.CancelResponse(); err != nil {
				slog.Warn("voiceagent: provider response cancel failed; suppressing downlink until turn end",
					"session_id", a.Session.ID, "err", err)
			}
		}
	}
	fields := a.eventFrameFields(nil, EventInterrupted)
	fields.ProviderMetadata = map[string]any{"reason": "client_cancel"}
	a.sendJSON(ctx, InterruptedFrame{
		Type:             MsgInterrupted,
		EventFrameFields: fields,
	})
}

func (a *Adapter) writePump(ctx context.Context, done chan<- struct{}) {
	defer func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	for {
		msg, err := a.Provider.Receive(ctx)
		if err != nil {
			if ctx.Err() == nil {
				a.sendError(ctx, "provider_receive_failed", err.Error())
			}
			return
		}
		if msg == nil {
			continue
		}
		// Any provider-emitted message counts as activity for the idle
		// watchdog so a long-running TTS reply doesn't get cut off.
		a.idle.Reset()
		if len(msg.Audio) > 0 || msg.OutputTranscript != "" {
			a.replyActive.set(true)
		}
		if len(msg.Audio) > 0 {
			switch {
			case a.suppressDownlink.get():
				// Client cancelled the current reply: drop its remaining
				// downlink audio while the provider drains (or aborts, for
				// LiveResponseCanceller providers).
			case a.mediaBridge != nil:
				if err := a.mediaBridge.SendAudio(msg.Audio); err != nil {
					slog.Warn("voiceagent: send media bridge audio failed", "err", err)
					a.sendError(ctx, "audio_downstream_failed", err.Error())
					return
				}
			default:
				a.sendBinary(ctx, msg.Audio)
			}
		}
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
		if msg.Interrupted {
			a.sendJSON(ctx, InterruptedFrame{
				Type:             MsgInterrupted,
				EventFrameFields: a.eventFrameFields(msg, EventInterrupted),
			})
		}
		if msg.Done || msg.Interrupted {
			// Turn boundary: the (possibly cancelled) reply is over; the next
			// reply streams normally.
			a.replyActive.set(false)
			a.suppressDownlink.set(false)
		}
		if msg.GoAway {
			a.sendJSON(ctx, SessionEndFrame{
				Type:             MsgSessionEnd,
				EventFrameFields: a.eventFrameFields(msg, EventSessionEnd),
				Reason:           "go_away",
			})
			return
		}
		if eventType := standaloneEventType(msg); eventType != "" {
			a.sendJSON(ctx, EventFrame{
				Type:             MsgEvent,
				EventFrameFields: a.eventFrameFields(msg, eventType),
			})
		}
	}
}
