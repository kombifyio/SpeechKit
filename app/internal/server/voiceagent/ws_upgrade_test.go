//go:build linux

package voiceagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketRejectsDisallowedOrigin(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:        manager,
		Provider:       staticProviderFactory{provider: newFakeProvider()},
		Persona:        &fakeResolver{},
		AllowedOrigins: []string{"https://app.example.com"},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	session, ticket, err := manager.Create(Identity{UserID: "user-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{"https://evil.example"}},
		Subprotocols: []string{wsTicketSubprotocol(ticket)},
	})
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
	if err == nil {
		t.Fatalf("websocket dial unexpectedly succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		if resp == nil {
			t.Fatalf("response is nil, want %d", http.StatusForbidden)
		}
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestWebSocketAllowsConfiguredBrowserOrigin(t *testing.T) {
	manager := mustManager(t, Options{})
	provider := newFakeProvider()
	handler, err := New(HandlerOptions{
		Manager:        manager,
		Provider:       staticProviderFactory{provider: provider},
		Persona:        &fakeResolver{},
		AllowedOrigins: []string{"https://app.example.com"},
		IdleTimeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	defer provider.Close() //nolint:errcheck

	session, ticket, err := manager.Create(Identity{UserID: "user-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": []string{"https://app.example.com"}},
		Subprotocols: []string{wsTicketSubprotocol(ticket)},
	})
	if err != nil {
		t.Fatalf("websocket dial with allowed Origin: %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestWebSocketRejectsQueryOnlyTicket(t *testing.T) {
	manager := mustManager(t, Options{})
	provider := newFakeProvider()
	handler, err := New(HandlerOptions{
		Manager:        manager,
		Provider:       staticProviderFactory{provider: provider},
		Persona:        &fakeResolver{},
		AllowedOrigins: []string{"https://app.example.com"},
		IdleTimeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	defer provider.Close() //nolint:errcheck

	session, ticket, err := manager.Create(Identity{UserID: "user-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws?ticket=" + ticket
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://app.example.com"}},
	})
	if conn != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}
	if err == nil {
		t.Fatalf("websocket dial with query-only ticket unexpectedly succeeded")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		if resp == nil {
			t.Fatalf("response is nil, want %d", http.StatusUnauthorized)
		}
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestWebSocketRejectsClientWithoutOriginByDefault(t *testing.T) {
	// Audit S-2 hardening: a browser that omits Origin defeats CSRF-style
	// protection, so TICKETLESS empty-Origin upgrades stay denied by
	// default (opt-in via SPEECHKIT_ALLOW_EMPTY_ORIGIN=1, covered by the
	// next test). Ticketed native clients proceed to ticket verification
	// instead — their HMAC session ticket is the credential.
	manager := mustManager(t, Options{})
	provider := newFakeProvider()
	handler, err := New(HandlerOptions{
		Manager:        manager,
		Provider:       staticProviderFactory{provider: provider},
		Persona:        &fakeResolver{},
		AllowedOrigins: []string{"https://app.example.com"},
		IdleTimeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	defer provider.Close() //nolint:errcheck

	session, _, err := manager.Create(Identity{UserID: "user-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, resp, dialErr := websocket.Dial(ctx, wsURL, nil)
	if dialErr == nil {
		t.Fatalf("ticketless websocket dial without Origin should fail")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 from origin gate; got %d", resp.StatusCode)
	}
}

func TestWebSocketAllowsNativeClientWithoutOriginWhenEnvSet(t *testing.T) {
	// CLIs, sk-e2e, and native desktop clients that never set an Origin
	// header opt into the upgrade via SPEECHKIT_ALLOW_EMPTY_ORIGIN=1.
	// This is the operator-controlled escape hatch added in S-2.
	t.Setenv(envAllowEmptyWSOriginVar, "1")
	manager := mustManager(t, Options{})
	provider := newFakeProvider()
	handler, err := New(HandlerOptions{
		Manager:        manager,
		Provider:       staticProviderFactory{provider: provider},
		Persona:        &fakeResolver{},
		AllowedOrigins: []string{"https://app.example.com"},
		IdleTimeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	defer provider.Close() //nolint:errcheck

	session, ticket, err := manager.Create(Identity{UserID: "user-1", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{wsTicketSubprotocol(ticket)},
	})
	if err != nil {
		t.Fatalf("websocket dial without Origin (env opt-in): %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestExtractWSTicketReadsSubprotocolOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/abc/ws?ticket=querytkt", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "ticket.subprototkt, ticket-v1")
	gotTicket, gotSubproto := extractWSTicket(req)
	if gotTicket != "subprototkt" {
		t.Fatalf("ticket = %q, want subprototkt", gotTicket)
	}
	if gotSubproto != "ticket.subprototkt" {
		t.Fatalf("subproto = %q, want ticket.subprototkt", gotSubproto)
	}
}

func TestExtractWSTicketRejectsQueryOnlyTicket(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/abc/ws?ticket=onlyquery", nil)
	gotTicket, gotSubproto := extractWSTicket(req)
	if gotTicket != "" {
		t.Fatalf("ticket = %q, want empty query rejection", gotTicket)
	}
	if gotSubproto != "" {
		t.Fatalf("subproto = %q, want empty query rejection", gotSubproto)
	}
}

func TestExtractWSTicket_IgnoresUnrelatedSubprotocols(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/abc/ws?ticket=fallback", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "speechkit.audio, mqtt-v3")
	gotTicket, gotSubproto := extractWSTicket(req)
	if gotTicket != "" {
		t.Fatalf("ticket = %q, want empty without ticket.* subproto", gotTicket)
	}
	if gotSubproto != "" {
		t.Fatalf("subproto = %q, want empty (no ticket.* subproto)", gotSubproto)
	}
}

func TestHandler_DefaultReadLimitIs64KiB(t *testing.T) {
	manager := mustManager(t, Options{})
	h, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	if h.readLimit != defaultWSReadLimitBytes {
		t.Fatalf("readLimit = %d, want %d (64 KiB default)", h.readLimit, defaultWSReadLimitBytes)
	}
}

func TestHandler_ReadLimitOverrideHonored(t *testing.T) {
	manager := mustManager(t, Options{})
	h, err := New(HandlerOptions{
		Manager:   manager,
		Provider:  staticProviderFactory{provider: newFakeProvider()},
		Persona:   &fakeResolver{},
		ReadLimit: 128 * 1024,
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	if h.readLimit != 128*1024 {
		t.Fatalf("readLimit = %d, want 128 KiB", h.readLimit)
	}
}
