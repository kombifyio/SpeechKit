//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
			case MsgPing:
				a.sendJSON(PongFrame{Type: MsgPong})
			case MsgStop:
				a.sendJSON(SessionEndFrame{Type: MsgSessionEnd, Reason: "client"})
				return
			case MsgAdvanceStep:
				// M5 will hook the persona resolver here to update the
				// live system prompt with the next sequence step. For
				// M4 the frame is accepted and echoed as a
				// sequence_step transition so clients can prototype UIs.
				a.sendJSON(SequenceStepFrame{Type: MsgSequenceStep, StepID: "", Status: "completed"})
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
		}
		if msg.OutputTranscript != "" {
			a.sendJSON(TranscriptFrame{Type: MsgOutputTranscript, Text: msg.OutputTranscript, Done: msg.OutputTranscriptDone})
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
