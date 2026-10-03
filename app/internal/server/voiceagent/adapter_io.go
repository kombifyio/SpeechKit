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

	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// ── helpers ─────────────────────────────────────────────────────────────────

func (a *Adapter) sendJSON(ctx context.Context, v any) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	a.writeJSONLocked(ctx, v)
}

func (a *Adapter) writeJSONLocked(ctx context.Context, v any) {
	if a.closed.get() || a.terminal {
		return
	}
	if failure, ok := v.(ErrorFrame); ok && failure.Fatal {
		a.failureEndReason = "error"
		if failure.Code == "auth_expired" {
			a.failureEndReason = "authorization_expired"
		}
		// Hold the write lock through the error frame so Run's concurrent
		// terminal frame cannot overtake it. Cancel admission before any I/O.
		if a.stopAdmission != nil {
			a.stopAdmission()
		}
		ctx = context.WithoutCancel(ctx)
	}
	if end, ok := v.(SessionEndFrame); ok {
		// Final delivery outlives admission cancellation, but remains bounded
		// by the transport write timeout below.
		ctx = context.WithoutCancel(ctx)
		if a.failureEndReason != "" {
			end.Reason = a.failureEndReason
			v = end
		}
		a.terminal = true
	} else if ctx.Err() != nil {
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

func (a *Adapter) sendProviderError(ctx context.Context, fallback string, err error, fatal bool) {
	code := fallback
	var coded cascaded.CodedError
	if errors.As(err, &coded) {
		code = cascaded.SafeFailureCode(coded.Code())
	}
	a.sendErrorWithFatal(ctx, code, cascaded.FailureMessage(code), fatal)
}

// settleDuplexReplyLocked reuses the kernel's output-cadence policy. Native
// turn-based providers still require their actual Done before cancellation is
// released. All paired reply/suppression transitions share the write fence.
func (a *Adapter) settleDuplexReplyLocked(now time.Time) bool {
	if !a.continuousDuplex || a.replyActivityAt.IsZero() || now.Sub(a.replyActivityAt) < live.DefaultSpeakingSettleDelay {
		return false
	}
	a.replyActive.set(false)
	a.suppressDownlink.set(false)
	a.receiveEpoch++
	return true
}

func (a *Adapter) sendSessionEnd(ctx context.Context, reason string) {
	a.sendJSON(ctx, SessionEndFrame{
		Type:             MsgSessionEnd,
		EventFrameFields: a.eventFrameFields(nil, EventSessionEnd),
		Reason:           reason,
	})
}

func (a *Adapter) sendError(ctx context.Context, code, message string) {
	a.sendErrorWithFatal(ctx, code, message, false)
}

func (a *Adapter) sendFatalError(ctx context.Context, code, message string) {
	a.sendErrorWithFatal(ctx, code, message, true)
}

func (a *Adapter) sendErrorWithFatal(ctx context.Context, code, message string, fatal bool) {
	a.sendJSON(ctx, ErrorFrame{
		Type:        MsgError,
		Code:        code,
		Message:     message,
		Fatal:       fatal,
		Remediation: ErrorRemediation(code),
		RequestID:   middleware.RequestIDFromContext(ctx),
	})
}

func (a *Adapter) closeSocket() {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	a.closed.set(true)
	// The terminal application frame has already been written. Do not keep
	// provider cleanup or final accounting waiting for a peer close handshake.
	_ = a.Conn.CloseNow()
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
