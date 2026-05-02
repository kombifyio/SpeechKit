//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Adapter bridges a live WebSocket conversation to the Framework kernel's
// voiceagent session. One Adapter instance handles exactly one session,
// which matches the manager's concurrency model.
//
// The adapter owns two long-lived goroutines:
//
//	readPump:  WebSocket → kernel (control + audio frames from client)
//	writePump: kernel → WebSocket (audio + transcript + tool-call frames)
//
// When either pump errors or the provider reports session_end, the adapter
// closes the socket, calls OnClose, and returns from Run.
type Adapter struct {
	Session  *ManagedSession
	Conn     *websocket.Conn
	Provider LiveProviderAdapter
	Persona  PersonaResolver
	// IdleTimeout terminates a session whose readPump and writePump have
	// both been silent for the duration. Zero disables the server-side
	// idle watchdog. Defaults to 15 minutes when set by the WS handler.
	IdleTimeout time.Duration
	// OnClose runs after both pumps have returned. Typically removes the
	// session from the manager.
	OnClose func()

	writeMu sync.Mutex
	closed  atomicBool
	idle    *idleWatchdog
	flow    *SequenceRunner
}

// Run blocks until the session ends. The first frame from the client MUST be
// a StartFrame; if it isn't, the adapter closes with an error.
func (a *Adapter) Run(parent context.Context) {
	defer a.closeSocket(websocket.StatusNormalClosure, "done")
	if a.OnClose != nil {
		defer a.OnClose()
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	start, err := a.waitForStart(ctx)
	if err != nil {
		a.sendError("start_required", err.Error())
		return
	}
	cfg, err := a.Persona.Resolve(start)
	if err != nil {
		a.sendError("persona_unresolved", err.Error())
		return
	}
	if err := a.Provider.Connect(ctx, cfg); err != nil {
		a.sendError("provider_connect_failed", err.Error())
		return
	}
	defer func() {
		if err := a.Provider.Close(); err != nil {
			slog.Debug("voiceagent: provider close", "err", err)
		}
	}()

	a.sendJSON(StateFrame{Type: MsgState, State: "listening"})
	if stepResolver, ok := a.Persona.(StepResolver); ok {
		a.flow = NewSequenceRunner(start, cfg, stepResolver)
	} else {
		a.flow = NewSequenceRunner(start, cfg, nil)
	}
	if entered := a.flow.InitialEnteredFrame(); entered != nil {
		a.sendJSON(*entered)
	}

	a.idle = newIdleWatchdog(a.IdleTimeout)
	defer a.idle.Stop()

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.readPump(ctx, done) }()
	go func() { defer wg.Done(); a.writePump(ctx, done) }()

	select {
	case <-done:
		// One of the pumps returned: a client disconnect, provider EOF,
		// GoAway, or explicit MsgStop. The other pump exits naturally
		// when ctx cancels below.
	case <-a.idle.Fired():
		slog.Info("voiceagent: session idle timeout reached; closing",
			"session_id", a.Session.ID,
			"timeout", a.IdleTimeout,
		)
		a.sendJSON(SessionEndFrame{Type: MsgSessionEnd, Reason: "idle"})
	}
	cancel()
	wg.Wait()
}

func (a *Adapter) waitForStart(ctx context.Context) (StartFrame, error) {
	// Tight deadline so clients that never send `start` don't park a slot.
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	typ, data, err := a.Conn.Read(readCtx)
	if err != nil {
		return StartFrame{}, err
	}
	if typ != websocket.MessageText {
		return StartFrame{}, errors.New("first frame must be text 'start'")
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return StartFrame{}, err
	}
	if env.Type != MsgStart {
		return StartFrame{}, errors.New("first frame must be 'start'")
	}
	var start StartFrame
	if err := json.Unmarshal(data, &start); err != nil {
		return StartFrame{}, err
	}
	return start, nil
}

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
			if err := a.Provider.SendAudio(data); err != nil {
				slog.Warn("voiceagent: send audio failed", "err", err)
				a.sendError("audio_upstream_failed", err.Error())
				return
			}
		case websocket.MessageText:
			var env envelope
			if err := json.Unmarshal(data, &env); err != nil {
				a.sendError("invalid_frame", err.Error())
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
					a.sendError("invalid_frame", err.Error())
					continue
				}
				responder, ok := a.Provider.(LiveToolResponder)
				if !ok {
					a.sendError("tool_response_unsupported", "provider does not accept tool responses")
					continue
				}
				if err := responder.SendToolResponse(tr); err != nil {
					slog.Warn("voiceagent: tool response upstream failed", "err", err)
					a.sendError("tool_response_failed", err.Error())
				}
			case MsgPing:
				a.sendJSON(PongFrame{Type: MsgPong})
			case MsgStop:
				a.sendJSON(SessionEndFrame{Type: MsgSessionEnd, Reason: "client"})
				return
			case MsgAdvanceStep:
				var advance AdvanceStepFrame
				if err := json.Unmarshal(data, &advance); err != nil {
					a.sendError("invalid_frame", err.Error())
					continue
				}
				if advance.Reason == "" {
					advance.Reason = "client"
				}
				if err := a.advanceWorkflowStep(ctx, advance); err != nil {
					slog.Warn("voiceagent: advance_step failed", "err", err)
					a.sendError("advance_step_failed", err.Error())
				}
			default:
				// Unknown frames are tolerated (forward-compat); we log
				// and ignore so a newer client doesn't break older servers.
				slog.Debug("voiceagent: unknown client frame", "type", env.Type)
			}
		}
	}
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
				a.sendError("provider_receive_failed", err.Error())
			}
			return
		}
		if msg == nil {
			continue
		}
		// Any provider-emitted message counts as activity for the idle
		// watchdog so a long-running TTS reply doesn't get cut off.
		a.idle.Reset()
		if len(msg.Audio) > 0 {
			a.sendBinary(msg.Audio)
		}
		if msg.InputTranscript != "" {
			a.sendJSON(TranscriptFrame{Type: MsgInputTranscript, Text: msg.InputTranscript, Done: msg.InputTranscriptDone})
			if msg.InputTranscriptDone {
				a.recordUserTurn(ctx)
			}
		}
		if msg.OutputTranscript != "" {
			a.sendJSON(TranscriptFrame{Type: MsgOutputTranscript, Text: msg.OutputTranscript, Done: msg.OutputTranscriptDone})
		}
		for _, call := range msg.ToolCalls {
			a.sendJSON(ToolCallFrame{Type: MsgToolCall, ID: call.ID, Name: call.Name, Args: call.Args})
		}
		if msg.Interrupted {
			a.sendJSON(InterruptedFrame{Type: MsgInterrupted})
		}
		if msg.GoAway {
			a.sendJSON(SessionEndFrame{Type: MsgSessionEnd, Reason: "go_away"})
			return
		}
	}
}

func (a *Adapter) recordUserTurn(ctx context.Context) {
	if a.flow == nil || !a.flow.RecordUserTurn() {
		return
	}
	if err := a.advanceWorkflowStep(ctx, AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "max_turns"}); err != nil {
		slog.Warn("voiceagent: max-turn workflow advance failed", "err", err)
		a.sendError("advance_step_failed", err.Error())
	}
}

func (a *Adapter) advanceWorkflowStep(ctx context.Context, frame AdvanceStepFrame) error {
	if a.flow == nil || !a.flow.Active() {
		return nil
	}
	transition, err := a.flow.Advance(ctx, frame)
	if err != nil {
		return err
	}
	if transition.Completed != nil {
		a.sendJSON(*transition.Completed)
	}
	if transition.SequenceCompleted {
		current := a.flow.Current()
		a.sendJSON(sequenceCompletedFrame(current, frame.Reason))
		return nil
	}
	if transition.Entered != nil {
		if err := a.applyInstructionUpdate(ctx, transition.NextConfig); err != nil {
			slog.Warn("voiceagent: workflow instruction update failed", "err", err)
			a.sendError("instruction_update_failed", err.Error())
		}
		a.sendJSON(*transition.Entered)
	}
	return nil
}

func (a *Adapter) applyInstructionUpdate(ctx context.Context, cfg LiveConfigFrame) error {
	if updater, ok := a.Provider.(LiveInstructionUpdater); ok {
		return updater.UpdateInstructions(ctx, cfg)
	}
	text := RenderHostInstructionUpdate(cfg)
	if text == "" {
		return nil
	}
	return a.Provider.SendText(text)
}

func RenderHostInstructionUpdate(cfg LiveConfigFrame) string {
	var b strings.Builder
	if strings.TrimSpace(cfg.StepID) == "" && strings.TrimSpace(cfg.SystemPrompt) == "" {
		return ""
	}
	b.WriteString("Host instruction update: the active Voice Agent workflow step has changed.")
	if cfg.SequenceID != "" || cfg.StepID != "" {
		b.WriteString("\nSequence: ")
		b.WriteString(cfg.SequenceID)
		b.WriteString("\nStep: ")
		b.WriteString(cfg.StepID)
	}
	if strings.TrimSpace(cfg.StepInstruction) != "" {
		b.WriteString("\nStep instruction:\n")
		b.WriteString(strings.TrimSpace(cfg.StepInstruction))
	}
	if strings.TrimSpace(cfg.StepExitCriteria) != "" {
		b.WriteString("\nExit criteria:\n")
		b.WriteString(strings.TrimSpace(cfg.StepExitCriteria))
	}
	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		b.WriteString("\nComposed behavior prompt:\n")
		b.WriteString(strings.TrimSpace(cfg.SystemPrompt))
	}
	return b.String()
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (a *Adapter) sendJSON(v any) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Warn("voiceagent: marshal frame", "err", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Conn.Write(ctx, websocket.MessageText, data); err != nil {
		slog.Debug("voiceagent: write text failed", "err", err)
	}
}

func (a *Adapter) sendBinary(data []byte) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Conn.Write(ctx, websocket.MessageBinary, data); err != nil {
		slog.Debug("voiceagent: write binary failed", "err", err)
	}
}

func (a *Adapter) sendError(code, message string) {
	a.sendJSON(ErrorFrame{Type: MsgError, Code: code, Message: message})
}

func (a *Adapter) closeSocket(status websocket.StatusCode, reason string) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	a.closed.set(true)
	_ = a.Conn.Close(status, reason)
}

// atomicBool is a tiny wrapper; avoids pulling sync/atomic aliases into every
// test file.
type atomicBool struct {
	mu  sync.Mutex
	val bool
}

func (a *atomicBool) get() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.val
}
func (a *atomicBool) set(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.val = v
}
