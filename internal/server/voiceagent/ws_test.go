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

func TestWebSocketAllowsNativeClientWithoutOrigin(t *testing.T) {
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
		t.Fatalf("websocket dial without Origin: %v", err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
