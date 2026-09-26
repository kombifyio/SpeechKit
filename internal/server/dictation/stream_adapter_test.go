//go:build linux

package dictation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

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
