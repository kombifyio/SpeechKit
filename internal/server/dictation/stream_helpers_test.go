//go:build linux

package dictation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// ── fakes ───────────────────────────────────────────────────────────────────

type fakeDictationStream struct {
	mu         sync.Mutex
	pcm        [][]byte
	events     chan speechkit.DictationStreamEvent
	closed     bool
	onFinalize func(*fakeDictationStream)
}

func newFakeDictationStream() *fakeDictationStream {
	return &fakeDictationStream{events: make(chan speechkit.DictationStreamEvent, 16)}
}

func (s *fakeDictationStream) SendPCM(_ context.Context, pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("fake stream closed")
	}
	s.pcm = append(s.pcm, append([]byte(nil), pcm...))
	return nil
}

func (s *fakeDictationStream) Finalize(context.Context) error {
	if s.onFinalize != nil {
		s.onFinalize(s)
	}
	return nil
}

func (s *fakeDictationStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	select {
	case event, ok := <-s.events:
		if !ok {
			return speechkit.DictationStreamEvent{}, io.EOF
		}
		return event, nil
	case <-ctx.Done():
		return speechkit.DictationStreamEvent{}, ctx.Err()
	}
}

func (s *fakeDictationStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeDictationStream) emit(event speechkit.DictationStreamEvent) {
	s.events <- event
}

func (s *fakeDictationStream) receivedPCMFrames() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pcm)
}

type fakeStreamRouter struct {
	mu       sync.Mutex
	has      bool
	startErr error
	streams  []*fakeDictationStream
	// prepared fixes the fake behind the NEXT StartDictationStream call so
	// tests can wire onFinalize before the segment exists.
	prepared []*fakeDictationStream
	lastOpts speechkit.DictationStreamOptions
}

func (r *fakeStreamRouter) HasDictationStreaming() bool { return r.has }

func (r *fakeStreamRouter) StartDictationStream(_ context.Context, opts speechkit.DictationStreamOptions, _ speaker.AudioFormat) (speechkit.DictationStream, error) {
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastOpts = opts
	var stream *fakeDictationStream
	if len(r.prepared) > 0 {
		stream = r.prepared[0]
		r.prepared = r.prepared[1:]
	} else {
		stream = newFakeDictationStream()
	}
	r.streams = append(r.streams, stream)
	return stream, nil
}

func (r *fakeStreamRouter) lastStartOpts() speechkit.DictationStreamOptions {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastOpts
}

func (r *fakeStreamRouter) stream(i int) *fakeDictationStream {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.streams) {
		return nil
	}
	return r.streams[i]
}

// ── helpers ─────────────────────────────────────────────────────────────────

func mustStreamManager(t *testing.T) *wssession.SessionManager {
	t.Helper()
	m, err := wssession.NewSessionManager(wssession.Options{
		TicketSecret: []byte("super-secret-key-16+bytes"),
	})
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	return m
}

func mustStreamHandler(t *testing.T, manager *wssession.SessionManager, router StreamRouter, opts func(*StreamHandlerOptions)) *StreamHandler {
	t.Helper()
	handlerOpts := StreamHandlerOptions{
		Manager:     manager,
		Router:      router,
		IdleTimeout: 5 * time.Second,
	}
	if opts != nil {
		opts(&handlerOpts)
	}
	h, err := NewStreamHandler(handlerOpts)
	if err != nil {
		t.Fatalf("NewStreamHandler: %v", err)
	}
	return h
}

func dialStreamWS(t *testing.T, ctx context.Context, serverURL, sessionID, ticket string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/v1/dictation/stream/sessions/" + sessionID + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{wssession.TicketSubprotocol(ticket)},
	})
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	return conn
}

func readStreamJSON(t *testing.T, ctx context.Context, conn *websocket.Conn) map[string]any {
	t.Helper()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("expected text frame, got %v", typ)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse frame %q: %v", data, err)
	}
	return out
}

func writeStreamJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write frame: %v", err)
	}
}
