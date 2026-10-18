//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
)

func (a *Adapter) configureConversation(ctx context.Context, start StartFrame, cfg *LiveConfigFrame) error {
	if start.ConversationControl == nil {
		return nil
	}
	control := start.ConversationControl
	if control.Version != 1 || control.Mode != "oneshot" || !a.conversationBindingAllowed(cfg) ||
		cfg.AISessionID == "" || cfg.VoiceSessionID == "" {
		return errors.New("voiceagent: conversation control unavailable")
	}
	session, err := a2a.NewConversationSession(a2a.ConversationCapability{
		Version: 1, Mode: "oneshot", VoiceSessionID: cfg.VoiceSessionID,
		ConversationSessionID: cfg.AISessionID, OneShotSessionID: cfg.AISessionID,
	}, func(turnCtx context.Context, receipt a2a.VoiceModeReceipt) error {
		return a.sendConversationReceipt(turnCtx, receipt)
	})
	if err != nil {
		return err
	}
	cfg.Conversation = session
	a.conversation = session
	return nil
}

type voiceModeController interface {
	SetVoiceMode(context.Context, string, string) error
}

func (p *registeredNativeProvider) SetVoiceMode(ctx context.Context, mode, requestID string) error {
	p.mu.Lock()
	agent, closed := p.conversationAgent, p.closed
	p.mu.Unlock()
	if agent == nil || closed {
		return a2a.ErrConversationUnsupported
	}
	return agent.SetVoiceMode(ctx, mode, requestID)
}

func (p *RegisteredAgentProvider) SetVoiceMode(ctx context.Context, mode, requestID string) error {
	p.mu.Lock()
	agent, active := p.conversationAgent, p.inner != nil
	p.mu.Unlock()
	if agent == nil || !active {
		return a2a.ErrConversationUnsupported
	}
	return agent.SetVoiceMode(ctx, mode, requestID)
}

func (a *Adapter) requestConversationMode(ctx context.Context, frame ConversationModeFrame) {
	controller, ok := a.Provider.(voiceModeController)
	if a.conversation == nil || !ok || frame.Version != 1 || (frame.Mode != "oneshot" && frame.Mode != "conversation") ||
		!validConversationRequestID(frame.RequestID) {
		a.sendError(ctx, "conversation_control_unsupported", "This voice session cannot accept the requested mode change.")
		return
	}
	select {
	case a.toolSem <- struct{}{}:
	default:
		a.sendError(ctx, "conversation_control_busy", "A voice action is already running.")
		return
	}
	a.toolWG.Add(1)
	go func() {
		defer a.toolWG.Done()
		defer func() { <-a.toolSem }()
		if err := controller.SetVoiceMode(ctx, frame.Mode, frame.RequestID); err != nil {
			if errors.Is(err, a2a.ErrConversationUnsupported) {
				a.sendError(ctx, "conversation_control_unsupported", "This voice session cannot accept the requested mode change.")
				return
			}
			if errors.Is(err, a2a.ErrConversationBusy) {
				a.sendError(ctx, "conversation_control_busy", "The current voice turn is still running. No mode change was dispatched.")
				return
			}
			a.sendError(ctx, "conversation_control_failed", "The conversation mode change was not confirmed. Do not replay an uncertain action.")
		}
	}()
}

// Unlike informational frames, this acknowledgement is a tool effect. Failed
// delivery must abort the answer instead of claiming that the device changed.
func (a *Adapter) sendConversationReceipt(ctx context.Context, receipt a2a.VoiceModeReceipt) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() || a.terminal {
		return errors.New("voiceagent: conversation control closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	frame := EventFrame{Type: MsgEvent, EventFrameFields: EventFrameFields{EventType: "conversation_mode"}, Conversation: &receipt}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return a.Conn.Write(writeCtx, websocket.MessageText, data)
}

func validConversationRequestID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.') {
			return false
		}
	}
	return true
}
