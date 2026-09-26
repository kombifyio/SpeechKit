//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/wssession"
)

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
