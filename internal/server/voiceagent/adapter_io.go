//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

// ── helpers ─────────────────────────────────────────────────────────────────

func (a *Adapter) sendJSON(ctx context.Context, v any) {
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
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.Conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		slog.Debug("voiceagent: write text failed", "err", err)
	}
}

func (a *Adapter) sendBinary(ctx context.Context, data []byte) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.Conn.Write(writeCtx, websocket.MessageBinary, data); err != nil {
		slog.Debug("voiceagent: write binary failed", "err", err)
	}
}

func (a *Adapter) sendError(ctx context.Context, code, message string) {
	a.sendJSON(ctx, ErrorFrame{
		Type:        MsgError,
		Code:        code,
		Message:     message,
		Remediation: ErrorRemediation(code),
		RequestID:   middleware.RequestIDFromContext(ctx),
	})
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
