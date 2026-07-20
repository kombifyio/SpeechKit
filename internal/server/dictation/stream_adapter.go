//go:build linux

package dictation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// segmentFlushTimeout bounds how long a finalize waits for the provider to
// flush its buffered tail before the segment is force-closed. Deepgram's
// Finalize keeps the connection open, so the drain needs a deadline.
const segmentFlushTimeout = 3 * time.Second

// providerFinalizeTimeout bounds the Finalize control write itself.
const providerFinalizeTimeout = 5 * time.Second

// startWaitTimeout is how long a `start` frame waits for the previous
// segment's receive loop to wind down before the server reports
// segment_active. Covers the benign race where a client sends the next start
// the instant segment_done arrives.
const startWaitTimeout = 2 * time.Second

// StreamAdapter bridges one streaming-Dictation WebSocket to the STT
// router's provider-native dictation streams. One adapter handles exactly
// one session; a session carries any number of sequential segments (one
// provider stream per `start`), which is how a keyboard reuses a warm
// socket across mic presses.
type StreamAdapter struct {
	Session *wssession.ManagedSession
	Conn    *websocket.Conn
	Router  StreamRouter
	// IdleTimeout terminates a session without any client- or provider-side
	// activity. Zero disables the watchdog.
	IdleTimeout time.Duration
	// MaxDuration terminates a session after this wall-clock duration. Zero
	// disables the hard cap.
	MaxDuration time.Duration
	// MaxStreamAudio caps cumulative uploaded audio (decoded duration)
	// across all segments of this session. Zero disables the budget.
	MaxStreamAudio time.Duration
	// OnClose runs after the read pump has returned. Typically removes the
	// session from the manager.
	OnClose func()

	writeMu sync.Mutex
	closed  bool
	idle    *wssession.IdleWatchdog

	// audioBudget is the remaining PCM-byte budget derived from
	// MaxStreamAudio and the active segment's format. Guarded by the read
	// pump (single goroutine).
	audioBytesSent int64
}

// streamSegment tracks one active provider stream.
type streamSegment struct {
	id         uint64
	stream     speechkit.DictationStream
	cancel     context.CancelFunc
	done       chan struct{} // closed when the receive loop exits
	finalizing atomic.Bool
	// bytesPerSecond of the negotiated PCM format, for the audio budget.
	bytesPerSecond int64

	// flushTimer is armed by the read pump (finalize) and stopped by the
	// receive goroutine's teardown — two goroutines, hence the mutex. Once
	// timerStopped latches, arming becomes a no-op so a finalize racing the
	// segment teardown cannot leave a stray timer behind.
	timerMu      sync.Mutex
	flushTimer   *time.Timer
	timerStopped bool
}

// armFlushTimer bounds the post-finalize drain: after d, the segment context
// is cancelled so Receive unblocks even when the provider has nothing
// buffered. No-op when the segment already tore down.
func (s *streamSegment) armFlushTimer(d time.Duration) {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	if s.timerStopped || s.flushTimer != nil {
		return
	}
	s.flushTimer = time.AfterFunc(d, s.cancel)
}

// stopFlushTimer permanently disarms the drain deadline.
func (s *streamSegment) stopFlushTimer() {
	s.timerMu.Lock()
	defer s.timerMu.Unlock()
	s.timerStopped = true
	if s.flushTimer != nil {
		s.flushTimer.Stop()
	}
}

// Run blocks until the session ends. The first frame from the client MUST be
// a `start` frame (enforced inside the read pump).
func (a *StreamAdapter) Run(parent context.Context) {
	defer a.closeSocket(websocket.StatusNormalClosure, "done")
	if a.OnClose != nil {
		defer a.OnClose()
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	a.idle = wssession.NewIdleWatchdog(a.IdleTimeout)
	defer a.idle.Stop()
	var maxDuration <-chan time.Time
	if a.MaxDuration > 0 {
		maxTimer := time.NewTimer(a.MaxDuration)
		defer maxTimer.Stop()
		maxDuration = maxTimer.C
	}

	done := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); defer a.guardPump(done); a.readPump(ctx, done) }()

	select {
	case <-done:
		// Read pump returned: client disconnect, stop frame, or audio-budget
		// exhaustion. Segment teardown already happened inside the pump.
	case <-a.idle.Fired():
		slog.Info("dictation stream: session idle timeout reached; closing",
			"session_id", a.Session.ID,
			"timeout", a.IdleTimeout,
		)
		a.sendJSON(ctx, StreamSessionEndFrame{Type: StreamMsgSessionEnd, Reason: StreamEndReasonIdle})
	case <-maxDuration:
		slog.Info("dictation stream: session max duration reached; closing",
			"session_id", a.Session.ID,
			"max_duration", a.MaxDuration,
		)
		a.sendJSON(ctx, StreamSessionEndFrame{Type: StreamMsgSessionEnd, Reason: StreamEndReasonMaxDuration})
	}
	cancel()
	wg.Wait()
}

// guardPump converts a panic in the read pump into a clean session teardown
// (mirrors the Voice Agent adapter's rationale: these goroutines are outside
// the HTTP Recover middleware).
func (a *StreamAdapter) guardPump(done chan<- struct{}) {
	rec := recover()
	if rec == nil {
		return
	}
	slog.Error("dictation stream: session pump panic recovered",
		"session_id", a.Session.ID,
		"err", rec,
		"stack", string(debug.Stack()),
	)
	select {
	case done <- struct{}{}:
	default:
	}
}

func (a *StreamAdapter) readPump(ctx context.Context, done chan<- struct{}) {
	defer func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	var (
		cur       *streamSegment
		segmentID uint64
	)
	defer func() {
		if cur != nil {
			cur.endNow()
		}
	}()

	for {
		typ, data, err := a.Conn.Read(ctx)
		if err != nil {
			return
		}
		a.idle.Reset()
		// Opportunistically clear a segment whose receive loop has finished.
		if cur != nil {
			select {
			case <-cur.done:
				cur = nil
			default:
			}
		}
		switch typ {
		case websocket.MessageBinary:
			if cur == nil {
				a.sendError(ctx, StreamErrNoActiveSegment, "send a 'start' frame before audio")
				continue
			}
			if cur.finalizing.Load() {
				// Trailing PCM racing the finalize is expected; drop quietly.
				continue
			}
			if a.exceedsAudioBudget(int64(len(data)), cur.bytesPerSecond) {
				a.sendJSON(ctx, StreamSessionEndFrame{Type: StreamMsgSessionEnd, Reason: StreamEndReasonMaxAudio})
				return
			}
			if err := cur.stream.SendPCM(ctx, data); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("dictation stream: send audio failed", "err", err)
				a.sendError(ctx, StreamErrProviderStreamFailed, err.Error())
				cur.endNow()
				cur = nil
			}
		case websocket.MessageText:
			var env streamEnvelope
			if err := json.Unmarshal(data, &env); err != nil {
				a.sendError(ctx, StreamErrInvalidFrame, err.Error())
				continue
			}
			switch env.Type {
			case StreamMsgStart:
				if cur != nil {
					if !waitSegmentDone(cur.done, startWaitTimeout) {
						a.sendError(ctx, StreamErrSegmentActive, "finalize the active segment before starting a new one")
						continue
					}
					cur = nil
				}
				var start StreamStartFrame
				if err := json.Unmarshal(data, &start); err != nil {
					a.sendError(ctx, StreamErrInvalidFrame, err.Error())
					continue
				}
				segmentID++
				next, err := a.startSegment(ctx, segmentID, start)
				if err != nil {
					continue // error frame already sent
				}
				cur = next
			case StreamMsgFinalize:
				if cur == nil {
					a.sendError(ctx, StreamErrNoActiveSegment, "no segment to finalize")
					continue
				}
				a.finalizeSegment(ctx, cur)
			case StreamMsgPing:
				a.sendJSON(ctx, StreamPongFrame{Type: StreamMsgPong})
			case StreamMsgStop:
				a.sendJSON(ctx, StreamSessionEndFrame{Type: StreamMsgSessionEnd, Reason: StreamEndReasonClient})
				return
			default:
				// Unknown frames are tolerated (forward-compat); log and
				// ignore so a newer client doesn't break older servers.
				slog.Debug("dictation stream: unknown client frame", "type", env.Type)
			}
		}
	}
}

// startSegment validates the start frame, opens a provider stream, and spawns
// its receive loop. Protocol errors are reported to the client here; the
// returned error only signals "stay in awaiting-start".
func (a *StreamAdapter) startSegment(ctx context.Context, id uint64, start StreamStartFrame) (*streamSegment, error) {
	if ok, reason := start.ValidFormat(); !ok {
		a.sendError(ctx, StreamErrFormatUnsupported, reason)
		return nil, errors.New(reason)
	}
	if !a.Router.HasDictationStreaming() {
		a.sendError(ctx, StreamErrStreamingUnavailable,
			"no streaming-capable STT provider is configured; fall back to POST /v1/dictation/transcribe")
		return nil, errors.New("streaming unavailable")
	}
	// Provider precedence per the voice-preferences contract: an explicit
	// provider_profile_id in the start frame wins; otherwise default from the
	// edge-resolved user preference captured at session mint. The router's
	// candidate prioritization treats an unknown/unconfigured preference as a
	// no-op, so an unsatisfiable preference falls back instead of erroring.
	if strings.TrimSpace(start.ProviderProfileID) == "" {
		start.ProviderProfileID = a.sessionPreferredProfile()
	}
	format := start.AudioFormat()
	segCtx, segCancel := context.WithCancel(ctx)
	stream, err := a.Router.StartDictationStream(segCtx, start.Options(), format)
	if err != nil {
		segCancel()
		// No provider could serve this format (the router already tried them
		// all). That is the same class as the ValidFormat rejection above and
		// the client's remedy is the same — downmix to mono and re-start, or
		// fall back to the batch POST. Reporting provider_stream_failed here
		// would wrongly invite a blind retry of the same doomed format.
		code := StreamErrProviderStreamFailed
		if errors.Is(err, speechkit.ErrUnsupportedAudioFormat) {
			code = StreamErrFormatUnsupported
		}
		slog.Warn("dictation stream: provider stream start failed", "err", err, "code", code)
		a.sendError(ctx, code, err.Error())
		return nil, err
	}
	seg := &streamSegment{
		id:             id,
		stream:         stream,
		cancel:         segCancel,
		done:           make(chan struct{}),
		bytesPerSecond: int64(format.SampleRateHz) * int64(format.Channels) * 2, // S16 = 2 bytes/sample
	}
	go a.receiveSegment(segCtx, seg)
	a.sendJSON(ctx, StreamReadyFrame{Type: StreamMsgReady, SegmentID: id})
	return seg, nil
}

// finalizeSegment flushes the provider stream and arms the drain deadline.
// The receive loop emits segment_done when the flushed final arrives (or the
// deadline cancels the segment context).
func (a *StreamAdapter) finalizeSegment(ctx context.Context, seg *streamSegment) {
	if !seg.finalizing.CompareAndSwap(false, true) {
		return // already finalizing
	}
	finalizeCtx, cancel := context.WithTimeout(ctx, providerFinalizeTimeout)
	defer cancel()
	if err := seg.stream.Finalize(finalizeCtx); err != nil {
		slog.Warn("dictation stream: provider finalize failed", "err", err)
	}
	// Deepgram keeps the connection open after Finalize; bound the drain so
	// segment_done is deterministic even when nothing was buffered.
	seg.armFlushTimer(segmentFlushTimeout)
}

// receiveSegment pumps provider events to the client until the segment ends.
func (a *StreamAdapter) receiveSegment(ctx context.Context, seg *streamSegment) {
	defer func() {
		seg.stopFlushTimer()
		seg.cancel()
		if err := seg.stream.Close(); err != nil {
			slog.Debug("dictation stream: provider stream close", "err", err)
		}
		close(seg.done)
	}()

	for {
		event, err := seg.stream.Receive(ctx)
		if err != nil {
			finalizing := seg.finalizing.Load()
			switch {
			case finalizing:
				// Flush drain complete (EOF, cancel-by-deadline, or provider
				// close) — the segment ends cleanly.
				a.sendJSON(ctx, StreamSegmentDoneFrame{Type: StreamMsgSegmentDone, SegmentID: seg.id})
			case ctx.Err() != nil, errors.Is(err, context.Canceled):
				// Session shutdown; terminal frames are the read pump's job.
			case errors.Is(err, io.EOF):
				// Provider ended the stream without a client finalize (e.g.
				// provider-side inactivity timeout). Tell the client so it
				// can restart or fall back to batch.
				a.sendError(ctx, StreamErrProviderStreamClosed, "provider closed the stream")
				a.sendJSON(ctx, StreamSegmentDoneFrame{Type: StreamMsgSegmentDone, SegmentID: seg.id})
			default:
				slog.Warn("dictation stream: provider receive failed", "err", err)
				a.sendError(ctx, StreamErrProviderStreamFailed, err.Error())
				a.sendJSON(ctx, StreamSegmentDoneFrame{Type: StreamMsgSegmentDone, SegmentID: seg.id})
			}
			return
		}
		a.idle.Reset()
		if event.Text != "" || len(event.Words) > 0 {
			a.sendJSON(ctx, streamTranscriptFromEvent(seg.id, event))
		}
		if event.IsFinal && seg.finalizing.Load() {
			// The flushed tail arrived — the segment is complete.
			a.sendJSON(ctx, StreamSegmentDoneFrame{Type: StreamMsgSegmentDone, SegmentID: seg.id})
			return
		}
	}
}

// sessionPreferredProfile returns the STT provider preference captured on
// this session at mint time: primary first, secondary when the primary is
// unset. Values are bare provider names ("deepgram", "assemblyai"), which the
// router's profile prioritization accepts directly.
func (a *StreamAdapter) sessionPreferredProfile() string {
	if a.Session == nil {
		return ""
	}
	if primary := strings.TrimSpace(a.Session.VoicePrefs.STTPrimary); primary != "" {
		return primary
	}
	return strings.TrimSpace(a.Session.VoicePrefs.STTSecondary)
}

// exceedsAudioBudget accumulates uploaded PCM bytes and reports whether the
// session's cumulative audio budget is exhausted.
func (a *StreamAdapter) exceedsAudioBudget(frameBytes, bytesPerSecond int64) bool {
	if a.MaxStreamAudio <= 0 || bytesPerSecond <= 0 {
		return false
	}
	a.audioBytesSent += frameBytes
	budgetBytes := int64(a.MaxStreamAudio/time.Second) * bytesPerSecond
	return a.audioBytesSent > budgetBytes
}

// endNow tears a segment down without the finalize/drain dance (session
// shutdown or mid-segment provider failure).
func (s *streamSegment) endNow() {
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(segmentFlushTimeout):
	}
}

func waitSegmentDone(done <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ── write helpers (mirroring the Voice Agent adapter) ───────────────────────

func (a *StreamAdapter) sendJSON(ctx context.Context, v any) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Warn("dictation stream: marshal frame", "err", err)
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := a.Conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		slog.Debug("dictation stream: write text failed", "err", err)
	}
}

func (a *StreamAdapter) sendError(ctx context.Context, code, message string) {
	a.sendJSON(ctx, StreamErrorFrame{Type: StreamMsgError, Code: code, Message: message})
}

func (a *StreamAdapter) closeSocket(status websocket.StatusCode, reason string) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed {
		return
	}
	a.closed = true
	_ = a.Conn.Close(status, reason)
}
