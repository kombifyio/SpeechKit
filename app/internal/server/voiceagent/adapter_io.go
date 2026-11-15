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
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
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
		if failure.Code == "voice_budget_exhausted" {
			a.failureEndReason = "voice_budget_exhausted"
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
	writeCtx, cancel := context.WithTimeout(ctx, transportWriteTimeout)
	defer cancel()
	if err := a.connWrite(writeCtx, websocket.MessageText, data); err != nil {
		slog.Debug("voiceagent: write text failed", "err", err)
	}
}

func (a *Adapter) sendProviderError(ctx context.Context, fallback string, err error, fatal bool) {
	code := fallback
	var coded cascaded.CodedError
	if errors.As(err, &coded) {
		code = cascaded.SafeFailureCode(coded.Code())
	}
	if !a.dialog.providerEnded.Load() {
		a.noteProviderFailure(code)
		if fatal && (code == "audio_upstream_failed" || a.dialog.userTurns.Load() > a.dialog.answered.Load()) {
			// The person is speaking or waiting for an answer: say why it stops.
			a.speakFailureNotice(ctx)
		}
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

// failureNoticeMaxPlay bounds how long a failure notice holds the session's
// end while the client plays it.
const failureNoticeMaxPlay = 6 * time.Second

// speakFailureNotice speaks one short neutral line to the client before a
// fatal provider failure ends a started conversation, so the person hears that
// the answer will not come instead of silence. It waits while the client plays
// the line, since the error frame that follows stops playback. Best effort and
// once per session: without a notice voice (Handler NoticeTTS), on a LiveKit
// transport or when synthesis fails, nothing is spoken.
func (a *Adapter) speakFailureNotice(ctx context.Context) {
	if a.NoticeTTS == nil || a.mediaBridge != nil || !a.dialog.startAccepted.Load() || !a.noticeSpoken.CompareAndSwap(false, true) {
		return
	}
	ctx = context.WithoutCancel(ctx)
	synthCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	result, err := a.NoticeTTS.Synthesize(synthCtx, failureNoticeFor(a.locale), tts.SynthesizeOpts{Locale: a.locale})
	cancel()
	if err != nil || result == nil || len(result.Audio) == 0 || len(result.Audio)%2 != 0 {
		return
	}
	started := time.Now()
	const frame = DownlinkSampleRate * 2 / 10 // 100 ms of 24 kHz S16LE mono
	for off := 0; off < len(result.Audio); off += frame {
		a.writeMu.Lock()
		if a.closed.get() || a.terminal {
			a.writeMu.Unlock()
			return
		}
		writeCtx, cancelWrite := context.WithTimeout(ctx, transportWriteTimeout)
		err := a.connWrite(writeCtx, websocket.MessageBinary, result.Audio[off:min(off+frame, len(result.Audio))])
		cancelWrite()
		a.writeMu.Unlock()
		if err != nil {
			return
		}
	}
	playing := time.Duration(len(result.Audio)) * time.Second / (DownlinkSampleRate * 2)
	timer := time.NewTimer(min(time.Until(started.Add(playing)), failureNoticeMaxPlay))
	defer timer.Stop()
	<-timer.C
}

// failureNoticeFor is the neutral line a failure notice speaks, in the
// session's language (de, es, otherwise English).
func failureNoticeFor(locale string) string {
	switch primaryLanguage(locale) {
	case "de":
		return "Die Sprachverbindung ist gerade abgebrochen. Bitte versuch es gleich noch einmal."
	case "es":
		return "La conexión de voz se ha interrumpido. Inténtalo de nuevo en un momento."
	default:
		return "The voice connection just dropped. Please try again in a moment."
	}
}

func (a *Adapter) sendSessionEnd(ctx context.Context, reason string) {
	a.logSessionEnd(reason)
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
		Class:       string(cascaded.FailureClass(code)),
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
