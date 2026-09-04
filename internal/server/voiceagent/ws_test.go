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
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
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
	if strings.Contains(body.WSURL, "?ticket=") {
		t.Fatalf("ws_url leaked ticket query: %q", body.WSURL)
	}
	if body.WSSubprotocol == "" || !strings.HasPrefix(body.WSSubprotocol, wsTicketSubprotocolPrefix) {
		t.Fatalf("ws_subprotocol = %q, want ticket.*", body.WSSubprotocol)
	}
}

func TestCreateSessionCarriesAISessionIntoVoiceEvents(t *testing.T) {
	manager := mustManager(t, Options{})
	provider := newFakeProvider()
	handler, err := New(HandlerOptions{
		Manager:     manager,
		Provider:    staticProviderFactory{provider: provider},
		Persona:     &fakeResolver{},
		IdleTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)
	server := httptest.NewServer(wrapped)
	defer server.Close()
	defer provider.Close() //nolint:errcheck

	requestBody := strings.NewReader(`{"ai_session_id":"ai-thread-42"}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/voiceagent/sessions", requestBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var ticket createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&ticket); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	if ticket.AISessionID != "ai-thread-42" {
		t.Fatalf("ai_session_id = %q", ticket.AISessionID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/voiceagent/sessions/" + ticket.SessionID + "/ws"
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{Subprotocols: []string{ticket.WSSubprotocol}})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"start"}`)); err != nil {
		t.Fatalf("send start: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read session event: %v", err)
	}
	var event StateFrame
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("decode session event: %v", err)
	}
	if event.AISessionID != "ai-thread-42" {
		t.Fatalf("event ai_session_id = %q", event.AISessionID)
	}
}

func TestCreateSessionCarriesOnlyMatchingAuthorizedAgentBinding(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager: manager,
		Providers: map[string]ProviderFactory{
			"default":       staticProviderFactory{provider: newFakeProvider()},
			"kombify-agent": staticProviderFactory{provider: newFakeProvider()},
		},
		DefaultProvider: "default",
		Persona:         &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	binding := middleware.VoiceAgentBinding{
		TargetAgentID: "kombify-ai",
		Endpoint:      "https://api.kombify.io/a2a/agents/kombify-ai",
		Lease:         "lease-secret",
	}
	serve := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := middleware.InjectIdentityForTest(req.Context(), middleware.Identity{UserID: "owner", OrgID: "org"})
		ctx = middleware.InjectVoiceAgentBindingForTest(ctx, binding)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req.WithContext(ctx))
		return rec
	}

	if rec := serve(`{"provider":"kombify-agent","target_agent_id":"other"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched target status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec := serve(`{"provider":"kombify-agent","target_agent_id":"kombify-ai","ai_session_id":"thread-1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("matching target status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response createSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	session, err := manager.Get(response.SessionID)
	if err != nil || session.VoiceAgentBinding != (wssession.VoiceAgentBinding(binding)) {
		t.Fatalf("authorized binding was not carried in memory: %+v", session)
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

func TestCreateSessionTrustsForwardedProtoOnlyFromTrustedProxy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		remoteAddr string
		wantPrefix string
	}{
		{name: "trusted proxy", remoteAddr: "203.0.113.10:4321", wantPrefix: "wss://speechkit.test/v1/voiceagent/sessions/"},
		{name: "untrusted remote", remoteAddr: "198.51.100.10:4321", wantPrefix: "ws://speechkit.test/v1/voiceagent/sessions/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := mustManager(t, Options{})
			handler, err := New(HandlerOptions{
				Manager:           manager,
				Provider:          staticProviderFactory{provider: newFakeProvider()},
				Persona:           &fakeResolver{},
				TrustedProxyCIDRs: []string{"203.0.113.0/24"},
			})
			if err != nil {
				t.Fatalf("New handler: %v", err)
			}
			mux := http.NewServeMux()
			handler.Mount(mux)
			wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

			req := httptest.NewRequest(http.MethodPost, "http://speechkit.test/v1/voiceagent/sessions", nil)
			req.RemoteAddr = tc.remoteAddr
			req.Header.Set("X-Forwarded-Proto", "https")
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
			}

			var body createSessionResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !strings.HasPrefix(body.WSURL, tc.wantPrefix) {
				t.Fatalf("ws_url = %q, want prefix %q", body.WSURL, tc.wantPrefix)
			}
		})
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
	if body.Metrics.TotalSessions != 2 || body.Metrics.IdentitySessions != 1 {
		t.Fatalf("metrics = %+v, want total=2 identity=1", body.Metrics)
	}
	if body.Metrics.MaxGlobalSessions != 100 || body.Metrics.MaxPerIdentitySessions != 3 {
		t.Fatalf("metric limits = %+v, want default 100/3", body.Metrics)
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
