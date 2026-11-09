//go:build linux

package voiceagent

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// defaultHeartbeat is the WebSocket ping period of a running conversation.
const defaultHeartbeat = 15 * time.Second

// clientGoneAfter is how many heartbeat periods the socket may stay without
// any traffic (a frame or pong read, a frame written) before its session ends.
const clientGoneAfter = 3

// pongWait bounds one ping (its write and the pong). It stays well below the
// period, so a ping never runs into the next tick.
func pongWait(period time.Duration) time.Duration {
	return min(5*time.Second, period/3)
}

// clientLiveness records the socket's last traffic in wall-clock time (the
// heartbeat's tickers are wall-clock too). Reads, handled frames, successful
// writes and writes in progress all count: a busy readPump cannot read a pong,
// and a client draining a long downlink (the C6 blocks its socket task while
// its speaker drains) can answer a ping only late.
type clientLiveness struct {
	seen     atomic.Int64 // unix nanoseconds of the last traffic
	handling atomic.Bool  // readPump is handling a frame
	writing  atomic.Int32 // socket writes in progress
}

func (l *clientLiveness) touch() { l.seen.Store(time.Now().UnixNano()) }

func (l *clientLiveness) busy() bool { return l.handling.Load() || l.writing.Load() > 0 }

func (l *clientLiveness) silentFor(now time.Time) time.Duration {
	return now.Sub(time.Unix(0, l.seen.Load()))
}

// connWrite is every socket write of the adapter: it marks the write in
// progress for the heartbeat and counts a delivered frame as traffic.
func (a *Adapter) connWrite(ctx context.Context, typ websocket.MessageType, data []byte) error {
	a.live.writing.Add(1)
	defer a.live.writing.Add(-1)
	err := a.Conn.Write(ctx, typ, data)
	if err == nil {
		a.live.touch()
	}
	return err
}

// heartbeat ends a running conversation whose client is gone (power or
// network lost without a close), so its device slot and voice minutes are
// released after clientGoneAfter periods of silence (45 to 60 s with the
// default period) instead of after IdleTimeout. Only silence ends a session:
// nothing read, nothing written and no frame being handled or written for the
// whole window. A ping that fails (the write lock held by a long downlink
// write, or a late pong) is never counted on its own. The ping uses its own
// bounded context, never the session context: coder/websocket closes the
// socket when a write's context ends, and teardown still has to send
// session_end.
func (a *Adapter) heartbeat(ctx context.Context, done chan<- struct{}, gone *atomic.Bool) {
	period := a.Heartbeat
	if period == 0 {
		period = defaultHeartbeat
	}
	if period < 0 || a.Conn == nil {
		return
	}
	window := time.Duration(clientGoneAfter) * period
	a.live.touch()
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if a.live.busy() {
			a.live.touch()
			continue
		}
		if silent := a.live.silentFor(time.Now()); silent >= window {
			slog.Info("voiceagent: client gone; closing", "session_id", a.Session.ID, "silent", silent.Round(time.Second))
			gone.Store(true)
			select {
			case done <- struct{}{}:
			default:
			}
			return
		}
		pingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pongWait(period))
		err := a.Conn.Ping(pingCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			a.live.touch()
		}
	}
}
