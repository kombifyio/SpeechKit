//go:build linux

package dictation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
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

// ── HTTP endpoint tests ─────────────────────────────────────────────────────

func TestStreamCreateSessionReportsCapabilities(t *testing.T) {
	for _, streaming := range []bool{true, false} {
		manager := mustStreamManager(t)
		handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: streaming}, nil)
		mux := http.NewServeMux()
		handler.Mount(mux)
		wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

		req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/dictation/stream/sessions", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
		}
		var body createStreamSessionResponse
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.SessionID == "" || body.Ticket == "" {
			t.Fatalf("missing session/ticket: %+v", body)
		}
		if !strings.Contains(body.WSURL, "/v1/dictation/stream/sessions/") || !strings.HasSuffix(body.WSURL, "/ws") {
			t.Fatalf("ws_url = %q", body.WSURL)
		}
		if !strings.HasPrefix(body.WSSubprotocol, wssession.TicketSubprotocolPrefix) {
			t.Fatalf("ws_subprotocol = %q", body.WSSubprotocol)
		}
		if body.Capabilities.Streaming != streaming || body.Capabilities.Emulation != "off" {
			t.Fatalf("capabilities = %+v, want streaming=%v emulation=off", body.Capabilities, streaming)
		}
	}
}

func TestStreamCreateSessionCapturesVoicePrefs(t *testing.T) {
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	prefs := middleware.VoicePrefs{STTPrimary: "assemblyai", STTSecondary: "deepgram"}
	inject := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(middleware.InjectVoicePrefsForTest(r.Context(), prefs)))
	})
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(inject)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/dictation/stream/sessions", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body createStreamSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	session, err := manager.Get(body.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	want := wssession.VoicePrefs{STTPrimary: "assemblyai", STTSecondary: "deepgram"}
	if session.VoicePrefs != want {
		t.Fatalf("session.VoicePrefs = %+v, want %+v", session.VoicePrefs, want)
	}
}

func TestStreamCollectionRejectsNonPOST(t *testing.T) {
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/dictation/stream/sessions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestStreamDeleteSessionOwnership(t *testing.T) {
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	session, _, err := manager.Create(wssession.Identity{UserID: "someone-else"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete,
		"https://speechkit.test/v1/dictation/stream/sessions/"+session.ID, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign session, got %d body=%s", rec.Code, rec.Body.String())
	}

	own, _, err := manager.Create(wssession.Identity{UserID: "anonymous"})
	if err != nil {
		t.Fatalf("create own session: %v", err)
	}
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete,
		"https://speechkit.test/v1/dictation/stream/sessions/"+own.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for own session, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStreamUpgradeRequiresTicket(t *testing.T) {
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, _, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/dictation/stream/sessions/" + session.ID + "/ws"
	_, resp, dialErr := websocket.Dial(ctx, wsURL, nil)
	if dialErr == nil {
		t.Fatal("dial without ticket should fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		if resp == nil {
			t.Fatal("response is nil, want 401")
		}
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestStreamUpgradeRejectsEmptyOriginByDefault(t *testing.T) {
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, func(o *StreamHandlerOptions) {
		o.AllowedOrigins = []string{"https://app.example.com"}
	})
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/dictation/stream/sessions/" + session.ID + "/ws"

	// Ticketless upgrade without an Origin header stays denied by default —
	// the CSRF-style gate for anything that is not a ticketed native client.
	_, resp, dialErr := websocket.Dial(ctx, wsURL, nil)
	if dialErr == nil {
		t.Fatal("ticketless dial without Origin should fail by default")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 from origin gate, got %d", resp.StatusCode)
	}

	// A native client that presents its session ticket subprotocol (and, like
	// all non-browser clients, no Origin) proceeds to ticket verification and
	// upgrades successfully.
	conn, _, dialErr := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{wssession.TicketSubprotocol(ticket)},
	})
	if dialErr != nil {
		t.Fatalf("ticketed dial without Origin should succeed: %v", dialErr)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "test done")
}

// ── WS protocol tests ───────────────────────────────────────────────────────

func TestStreamWS_SegmentLifecycle(t *testing.T) {
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	first := newFakeDictationStream()
	first.onFinalize = func(s *fakeDictationStream) {
		s.emit(speechkit.DictationStreamEvent{Sequence: 2, Text: "hallo welt", IsFinal: true, Provider: "fake"})
	}
	router := &fakeStreamRouter{has: true, prepared: []*fakeDictationStream{first}}
	handler := mustStreamHandler(t, manager, router, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
	defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

	// Segment 1: start → ready.
	writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart, Language: "de"})
	ready := readStreamJSON(t, ctx, conn)
	if ready["type"] != StreamMsgReady || ready["segment_id"] != float64(1) {
		t.Fatalf("expected ready segment 1, got %v", ready)
	}

	// Binary PCM reaches the provider stream.
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatalf("write pcm: %v", err)
	}

	// A provider draft flows down as transcript done=false.
	first.emit(speechkit.DictationStreamEvent{Sequence: 1, Text: "hallo we", Provider: "fake"})
	draft := readStreamJSON(t, ctx, conn)
	if draft["type"] != StreamMsgTranscript || draft["done"] != false || draft["text"] != "hallo we" {
		t.Fatalf("expected draft transcript, got %v", draft)
	}

	// finalize → flushed final → segment_done.
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgFinalize})
	final := readStreamJSON(t, ctx, conn)
	if final["type"] != StreamMsgTranscript || final["done"] != true || final["text"] != "hallo welt" {
		t.Fatalf("expected flushed final transcript, got %v", final)
	}
	done := readStreamJSON(t, ctx, conn)
	if done["type"] != StreamMsgSegmentDone || done["segment_id"] != float64(1) {
		t.Fatalf("expected segment_done 1, got %v", done)
	}
	if got := first.receivedPCMFrames(); got != 1 {
		t.Fatalf("provider stream received %d PCM frames, want 1", got)
	}

	// Segment 2 on the same socket.
	writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart})
	ready2 := readStreamJSON(t, ctx, conn)
	if ready2["type"] != StreamMsgReady || ready2["segment_id"] != float64(2) {
		t.Fatalf("expected ready segment 2, got %v", ready2)
	}
	if router.stream(1) == nil {
		t.Fatal("second start must open a fresh provider stream")
	}

	// stop → session_end{client}.
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgStop})
	for {
		frame := readStreamJSON(t, ctx, conn)
		if frame["type"] == StreamMsgSessionEnd {
			if frame["reason"] != StreamEndReasonClient {
				t.Fatalf("expected client end reason, got %v", frame)
			}
			break
		}
	}
}

// Provider precedence on the streaming surface: an explicit
// provider_profile_id in the start frame wins; otherwise the edge-resolved
// preference captured at session mint fills the gap (primary, then
// secondary); without either the router keeps its configured order.
func TestStreamWS_StartDefaultsProviderProfileFromSessionPref(t *testing.T) {
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	tests := []struct {
		name         string
		prefs        wssession.VoicePrefs
		startProfile string
		want         string
	}{
		{
			name:  "pref primary fills omitted provider_profile_id",
			prefs: wssession.VoicePrefs{STTPrimary: "deepgram", STTSecondary: "assemblyai"},
			want:  "deepgram",
		},
		{
			name:  "secondary applies when primary unset",
			prefs: wssession.VoicePrefs{STTSecondary: "assemblyai"},
			want:  "assemblyai",
		},
		{
			name:         "explicit start frame value wins over pref",
			prefs:        wssession.VoicePrefs{STTPrimary: "deepgram"},
			startProfile: "stt.assemblyai.universal",
			want:         "stt.assemblyai.universal",
		},
		{
			name: "no prefs leaves router default order",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := mustStreamManager(t)
			router := &fakeStreamRouter{has: true}
			handler := mustStreamHandler(t, manager, router, nil)
			mux := http.NewServeMux()
			handler.Mount(mux)
			server := httptest.NewServer(mux)
			defer server.Close()

			session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			session.VoicePrefs = tt.prefs

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
			defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

			writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart, ProviderProfileID: tt.startProfile})
			if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgReady {
				t.Fatalf("expected ready, got %v", frame)
			}
			if got := router.lastStartOpts().ProviderProfileID; got != tt.want {
				t.Fatalf("ProviderProfileID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamWS_StreamingUnavailable(t *testing.T) {
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: false}, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
	defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

	writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart})
	frame := readStreamJSON(t, ctx, conn)
	if frame["type"] != StreamMsgError || frame["code"] != StreamErrStreamingUnavailable {
		t.Fatalf("expected streaming_unavailable error, got %v", frame)
	}
	// The socket survives the capability error: ping still answers.
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgPing})
	pong := readStreamJSON(t, ctx, conn)
	if pong["type"] != StreamMsgPong {
		t.Fatalf("expected pong, got %v", pong)
	}
}

func TestStreamWS_BinaryBeforeStart(t *testing.T) {
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
	defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 64)); err != nil {
		t.Fatalf("write pcm: %v", err)
	}
	frame := readStreamJSON(t, ctx, conn)
	if frame["type"] != StreamMsgError || frame["code"] != StreamErrNoActiveSegment {
		t.Fatalf("expected no_active_segment error, got %v", frame)
	}
}

// The Android keepalive driver (KeepAliveSession) rests on one asymmetry: an
// application-level {"type":"ping"} frame resets the idle watchdog, while a
// transport-level WebSocket control ping does not — the websocket library
// answers control frames internally, so they never reach the read loop that
// calls idle.Reset(). If that ever flips, the client's ping becomes redundant
// (harmless) but OkHttp's transport ping would silently start keeping
// sessions alive, and the next person to touch this would draw the wrong
// conclusion about which mechanism matters. Pin both directions.
func TestStreamWS_IdleWatchdogResetSemantics(t *testing.T) {
	t.Run("application ping resets the watchdog", func(t *testing.T) {
		t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
		manager := mustStreamManager(t)
		handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, func(o *StreamHandlerOptions) {
			o.IdleTimeout = 600 * time.Millisecond
		})
		mux := http.NewServeMux()
		handler.Mount(mux)
		server := httptest.NewServer(mux)
		defer server.Close()

		session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
		defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

		// Ping well inside the window, repeatedly, for longer than the window.
		// A session that survives this proves the ping resets the watchdog.
		for range 5 {
			time.Sleep(200 * time.Millisecond)
			writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgPing})
			if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgPong {
				t.Fatalf("expected pong, got %v", frame)
			}
		}
	})

	t.Run("transport ping does not reset the watchdog", func(t *testing.T) {
		t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
		manager := mustStreamManager(t)
		handler := mustStreamHandler(t, manager, &fakeStreamRouter{has: true}, func(o *StreamHandlerOptions) {
			o.IdleTimeout = 600 * time.Millisecond
		})
		mux := http.NewServeMux()
		handler.Mount(mux)
		server := httptest.NewServer(mux)
		defer server.Close()

		session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
		defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

		// Transport pings at the same cadence as the case above. The session
		// must still die of idle.
		pingCtx, stopPings := context.WithCancel(ctx)
		defer stopPings()
		go func() {
			for {
				select {
				case <-pingCtx.Done():
					return
				case <-time.After(200 * time.Millisecond):
					_ = conn.Ping(pingCtx)
				}
			}
		}()

		frame := readStreamJSON(t, ctx, conn)
		if frame["type"] != StreamMsgSessionEnd || frame["reason"] != StreamEndReasonIdle {
			t.Fatalf("expected idle session_end despite transport pings, got %v", frame)
		}
	})
}

func TestStreamWS_MaxAudioBudget(t *testing.T) {
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	router := &fakeStreamRouter{has: true}
	handler := mustStreamHandler(t, manager, router, func(o *StreamHandlerOptions) {
		o.MaxStreamAudio = time.Second // 32000 bytes at 16 kHz mono S16
	})
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
	defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

	writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart})
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgReady {
		t.Fatalf("expected ready, got %v", frame)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 20000)); err != nil {
		t.Fatalf("write pcm 1: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 20000)); err != nil {
		t.Fatalf("write pcm 2: %v", err)
	}
	for {
		frame := readStreamJSON(t, ctx, conn)
		if frame["type"] == StreamMsgSessionEnd {
			if frame["reason"] != StreamEndReasonMaxAudio {
				t.Fatalf("expected max_audio end reason, got %v", frame)
			}
			break
		}
	}
}

func TestStreamWS_FinalizeDrainDeadline(t *testing.T) {
	// A finalize with nothing buffered must still produce segment_done via
	// the flush deadline (Deepgram keeps the connection open after Finalize).
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	router := &fakeStreamRouter{has: true}
	handler := mustStreamHandler(t, manager, router, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
	defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck

	writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart})
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgReady {
		t.Fatalf("expected ready, got %v", frame)
	}
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgFinalize})
	done := readStreamJSON(t, ctx, conn)
	if done["type"] != StreamMsgSegmentDone {
		t.Fatalf("expected segment_done after drain deadline, got %v", done)
	}
}
