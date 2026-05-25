//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

type staticProviderFactory struct {
	provider LiveProviderAdapter
}

func (f staticProviderFactory) NewProvider() LiveProviderAdapter {
	return f.provider
}

func TestCreateSessionUsesAPIPrefixInWebSocketURL(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	req.Header.Set(httpx.APIPrefixHeader, "/api")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body createSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(body.WSURL, "/api/v1/voiceagent/sessions/") {
		t.Fatalf("ws_url = %q, want /api/v1 prefix", body.WSURL)
	}
}

func TestCreateSessionUsesConfiguredPublicURLForWebSocketURL(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:   manager,
		Provider:  staticProviderFactory{provider: newFakeProvider()},
		Persona:   &fakeResolver{},
		PublicURL: "https://speechkit.example.com/api",
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	req.Header.Set("X-Forwarded-Host", "evil.example")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body createSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(body.WSURL, "wss://speechkit.example.com/api/v1/voiceagent/sessions/") {
		t.Fatalf("ws_url = %q, want configured public URL", body.WSURL)
	}
	if strings.Contains(body.WSURL, "evil.example") {
		t.Fatalf("ws_url reflected untrusted forwarded host: %q", body.WSURL)
	}
}

func TestCreateSessionUsesMountedPublicURLForWebSocketURL(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:   manager,
		Provider:  staticProviderFactory{provider: newFakeProvider()},
		Persona:   &fakeResolver{},
		PublicURL: "https://speechkit-api.example.com/v1/speechkit",
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.internal/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body createSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(body.WSURL, "wss://speechkit-api.example.com/v1/speechkit/voiceagent/sessions/") {
		t.Fatalf("ws_url = %q, want configured mounted public URL", body.WSURL)
	}
}

func TestCreateSessionIgnoresForwardedHostWithoutPublicURL(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	req.Header.Set("X-Forwarded-Host", "evil.example")
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body createSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.HasPrefix(body.WSURL, "wss://speechkit.test/v1/voiceagent/sessions/") {
		t.Fatalf("ws_url = %q, want request host", body.WSURL)
	}
	if strings.Contains(body.WSURL, "evil.example") {
		t.Fatalf("ws_url reflected untrusted forwarded host: %q", body.WSURL)
	}
}

func TestListSessionsReturnsCallerSessions(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	own, _, err := manager.Create(Identity{UserID: "anonymous", OrgID: "public"})
	if err != nil {
		t.Fatalf("create own session: %v", err)
	}
	if _, _, err := manager.Create(Identity{UserID: "other", OrgID: "public"}); err != nil {
		t.Fatalf("create other session: %v", err)
	}

	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	req := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body listSessionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Sessions) != 1 || body.Sessions[0].SessionID != own.ID {
		t.Fatalf("sessions = %+v, want only %s", body.Sessions, own.ID)
	}
}

func TestDeleteSessionRequiresOwnerOrAdmin(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	own, _, err := manager.Create(Identity{UserID: "anonymous", OrgID: "public"})
	if err != nil {
		t.Fatalf("create own session: %v", err)
	}
	other, _, err := manager.Create(Identity{UserID: "other", OrgID: "public"})
	if err != nil {
		t.Fatalf("create other session: %v", err)
	}

	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	forbiddenReq := httptest.NewRequest(http.MethodDelete, "https://speechkit.test/v1/voiceagent/sessions/"+other.ID, nil)
	forbiddenRec := httptest.NewRecorder()
	wrapped.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("delete other got %d body=%s, want 403", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "https://speechkit.test/v1/voiceagent/sessions/"+own.ID, nil)
	deleteRec := httptest.NewRecorder()
	wrapped.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete own got %d body=%s, want 204", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := manager.Get(own.ID); err != ErrSessionNotFound {
		t.Fatalf("deleted session lookup err = %v, want ErrSessionNotFound", err)
	}
	if _, err := manager.Get(other.ID); err != nil {
		t.Fatalf("other session should remain: %v", err)
	}
}

func TestPersistedSessionSubresourcesRequireStoreAndOwnership(t *testing.T) {
	manager := mustManager(t, Options{})
	handlerWithoutStore, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler without store: %v", err)
	}
	muxWithoutStore := http.NewServeMux()
	handlerWithoutStore.Mount(muxWithoutStore)
	wrappedWithoutStore := middleware.Auth(middleware.AuthOptions{Mode: "none"})(muxWithoutStore)

	missingStoreReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/1/transcript", nil)
	missingStoreRec := httptest.NewRecorder()
	wrappedWithoutStore.ServeHTTP(missingStoreRec, missingStoreReq)
	if missingStoreRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing store got %d body=%s, want 503", missingStoreRec.Code, missingStoreRec.Body.String())
	}

	sqliteStore, err := store.NewSQLiteStore(store.StoreConfig{
		SQLitePath:        filepath.Join(t.TempDir(), "speechkit.db"),
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	ownerCtx := store.WithRecordOwner(context.Background(), store.RecordOwner{UserID: "anonymous", OrgID: "public", Source: "none"})
	sessionID, err := sqliteStore.SaveVoiceAgentSession(ownerCtx, store.VoiceAgentSession{
		StartedAt:   time.Now().Add(-time.Minute),
		EndedAt:     time.Now(),
		Language:    "en",
		Transcript:  "User: hello\nAgent: hi",
		Turns:       []store.VoiceAgentTurn{{Role: "user", Text: "hello"}},
		Summary:     store.VoiceAgentSessionSummary{Summary: "A short hello."},
		CreatedAt:   time.Now(),
		OwnerUserID: "anonymous",
		OwnerOrgID:  "public",
	})
	if err != nil {
		t.Fatalf("SaveVoiceAgentSession: %v", err)
	}

	handlerWithStore, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
		Store:    sqliteStore,
	})
	if err != nil {
		t.Fatalf("New handler with store: %v", err)
	}
	muxWithStore := http.NewServeMux()
	handlerWithStore.Mount(muxWithStore)
	wrappedWithStore := middleware.Auth(middleware.AuthOptions{Mode: "none"})(muxWithStore)

	transcriptReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/"+strconv.FormatInt(sessionID, 10)+"/transcript", nil)
	transcriptRec := httptest.NewRecorder()
	wrappedWithStore.ServeHTTP(transcriptRec, transcriptReq)
	if transcriptRec.Code != http.StatusOK {
		t.Fatalf("transcript got %d body=%s, want 200", transcriptRec.Code, transcriptRec.Body.String())
	}
	if !strings.Contains(transcriptRec.Body.String(), "User: hello") {
		t.Fatalf("transcript body = %s", transcriptRec.Body.String())
	}

	summaryReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/"+strconv.FormatInt(sessionID, 10)+"/summary", nil)
	summaryRec := httptest.NewRecorder()
	wrappedWithStore.ServeHTTP(summaryRec, summaryReq)
	if summaryRec.Code != http.StatusOK {
		t.Fatalf("summary got %d body=%s, want 200", summaryRec.Code, summaryRec.Body.String())
	}
	if !strings.Contains(summaryRec.Body.String(), "A short hello.") {
		t.Fatalf("summary body = %s", summaryRec.Body.String())
	}

	badIDReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/not-numeric/transcript", nil)
	badIDRec := httptest.NewRecorder()
	wrappedWithStore.ServeHTTP(badIDRec, badIDReq)
	if badIDRec.Code != http.StatusNotFound {
		t.Fatalf("bad id got %d body=%s, want 404", badIDRec.Code, badIDRec.Body.String())
	}
}

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
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws?ticket=" + ticket
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
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
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws?ticket=" + ticket
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://app.example.com"}},
	})
	if err != nil {
		t.Fatalf("websocket dial with allowed Origin: %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestWebSocketRejectsClientWithoutOriginByDefault(t *testing.T) {
	// Audit S-2 hardening: a browser that omits Origin defeats CSRF-style
	// protection. Default is now to reject empty Origin; native clients
	// opt in via SPEECHKIT_ALLOW_EMPTY_ORIGIN=1 (covered by the next
	// test). Without that env, the upgrade returns 403 with body
	// "origin_not_allowed".
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
	_, resp, dialErr := websocket.Dial(ctx, wsURL, nil)
	if dialErr == nil {
		t.Fatalf("websocket dial without Origin should fail")
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
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + session.ID + "/ws?ticket=" + ticket
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial without Origin (env opt-in): %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func TestExtractWSTicket_PrefersSubprotocolOverQuery(t *testing.T) {
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

func TestExtractWSTicket_FallsBackToQueryWhenSubprotocolAbsent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/abc/ws?ticket=onlyquery", nil)
	gotTicket, gotSubproto := extractWSTicket(req)
	if gotTicket != "onlyquery" {
		t.Fatalf("ticket = %q, want onlyquery", gotTicket)
	}
	if gotSubproto != "" {
		t.Fatalf("subproto = %q, want empty fallback", gotSubproto)
	}
}

func TestExtractWSTicket_IgnoresUnrelatedSubprotocols(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/abc/ws?ticket=fallback", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "speechkit.audio, mqtt-v3")
	gotTicket, gotSubproto := extractWSTicket(req)
	if gotTicket != "fallback" {
		t.Fatalf("ticket = %q, want fallback (no ticket.* subproto)", gotTicket)
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
