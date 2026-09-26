//go:build linux

package dictation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
)

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
