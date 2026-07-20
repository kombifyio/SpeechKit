//go:build linux

package wyoming

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// Transcriber is the minimal STT surface the Wyoming adapter needs; satisfied
// by *internal/router.Router. Mirrors internal/server/dictation.Transcriber so
// the same batch router backs both HTTP dictation and Wyoming STT.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Synthesizer is the minimal TTS surface; satisfied by *internal/tts.Router.
type Synthesizer interface {
	Synthesize(ctx context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error)
}

// Options configures a Wyoming server. STT and/or TTS may be nil; the paired
// program is simply not advertised in Info and its events are ignored.
type Options struct {
	STT          Transcriber
	TTS          Synthesizer
	Info         Info
	DefaultVoice string
	DecodeLimits audio.DecodeLimits
	MaxSegment   int
	// AllowedCIDRs, when non-empty, restricts which peers may connect. Wyoming
	// has no in-protocol auth, so this is the adapter's only access control
	// beyond binding to a trusted interface.
	AllowedCIDRs []*net.IPNet
}

// Server accepts Wyoming TCP connections and bridges STT/TTS to the kernel
// routers. One goroutine per connection; a connection handles a describe
// handshake and either an STT turn (transcribe → audio-* → transcript) or a TTS
// turn (synthesize → audio-*).
type Server struct {
	opts Options
}

// NewServer returns a ready server. MaxSegment <= 0 uses DefaultMaxSegment.
func NewServer(opts Options) *Server {
	if opts.MaxSegment <= 0 {
		opts.MaxSegment = DefaultMaxSegment
	}
	return &Server{opts: opts}
}

// Serve accepts connections until ctx is cancelled or ln is closed. It returns
// nil on a clean shutdown (ctx cancellation / listener close).
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil //nolint:nilerr // clean shutdown: ctx-cancel / closed listener is not a caller-facing error
			}
			return err
		}
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if !s.allowedRemote(conn.RemoteAddr()) {
		slog.Debug("wyoming: rejected connection from disallowed peer", "remote", conn.RemoteAddr())
		return
	}
	rd := NewReader(conn, s.opts.MaxSegment)
	bw := bufio.NewWriter(conn)
	var sess sttSession

	for {
		if ctx.Err() != nil {
			return
		}
		ev, err := rd.ReadEvent()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				slog.Debug("wyoming: connection read ended", "err", err)
			}
			return
		}
		switch ev.Type {
		case TypeDescribe:
			if !s.reply(bw, TypeInfo, s.opts.Info) {
				return
			}
		case TypeTranscribe:
			var t Transcribe
			_ = decodeData(ev, &t)
			sess.begin(t.Language)
		case TypeAudioStart:
			var a AudioStart
			_ = decodeData(ev, &a)
			sess.start(a)
		case TypeAudioChunk:
			sess.appendPCM(ev.Payload)
		case TypeAudioStop:
			if !s.finishSTT(ctx, bw, &sess) {
				return
			}
		case TypeSynthesize:
			var syn Synthesize
			_ = decodeData(ev, &syn)
			if !s.synthesize(ctx, bw, syn) {
				return
			}
		default:
			slog.Debug("wyoming: ignoring event", "type", ev.Type)
		}
	}
}

// reply marshals data into an event of eventType and writes+flushes it. Returns
// false (and logs) on a write error so the caller can drop the connection.
func (s *Server) reply(bw *bufio.Writer, eventType string, data any) bool {
	ev, err := eventWithData(eventType, data)
	if err != nil {
		slog.Warn("wyoming: marshal reply", "type", eventType, "err", err)
		return true // marshal failure is our bug, not the peer's; keep the conn
	}
	return s.write(bw, ev)
}

// allowedRemote reports whether addr is permitted by the AllowedCIDRs
// allow-list. An empty list allows everyone.
func (s *Server) allowedRemote(addr net.Addr) bool {
	if len(s.opts.AllowedCIDRs) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range s.opts.AllowedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) write(bw *bufio.Writer, ev *Event) bool {
	if err := WriteEvent(bw, ev); err != nil {
		slog.Debug("wyoming: write event", "type", ev.Type, "err", err)
		return false
	}
	if err := bw.Flush(); err != nil {
		slog.Debug("wyoming: flush", "err", err)
		return false
	}
	return true
}
