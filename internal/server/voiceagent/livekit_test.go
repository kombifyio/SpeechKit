//go:build linux

package voiceagent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

func TestLiveKitIssuerMintsScopedJoinToken(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	issuer := &LiveKitTokenIssuer{
		URL:        "wss://livekit.example.com/",
		APIKey:     "api-key",
		APISecret:  "api-secret",
		TokenTTL:   5 * time.Minute,
		RoomPrefix: "speechkit-test",
		Clock:      func() time.Time { return now },
	}

	info, err := issuer.IssueJoinToken(context.Background(), "session-123", Identity{
		UserID: "auth0|user-1",
		OrgID:  "org-1",
		Plan:   "pro",
		Role:   "admin",
	})
	if err != nil {
		t.Fatalf("IssueJoinToken: %v", err)
	}
	if info.URL != "wss://livekit.example.com" {
		t.Fatalf("URL = %q", info.URL)
	}
	if info.Room != "speechkit-test-session-123" {
		t.Fatalf("room = %q", info.Room)
	}
	if info.ParticipantIdentity == "" || strings.Contains(info.ParticipantIdentity, "|") {
		t.Fatalf("participant identity = %q, want compact stable identity", info.ParticipantIdentity)
	}

	claims := decodeAndVerifyLiveKitToken(t, info.Token, "api-secret")
	if claims["iss"] != "api-key" {
		t.Fatalf("iss = %v", claims["iss"])
	}
	if claims["sub"] != info.ParticipantIdentity {
		t.Fatalf("sub = %v, want %q", claims["sub"], info.ParticipantIdentity)
	}
	if got := int64(claims["exp"].(float64)); got != now.Add(5*time.Minute).Unix() {
		t.Fatalf("exp = %d", got)
	}
	video := claims["video"].(map[string]any)
	if video["room"] != info.Room || video["roomJoin"] != true || video["canPublish"] != true || video["canSubscribe"] != true {
		t.Fatalf("video grant = %#v", video)
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(claims["metadata"].(string)), &metadata); err != nil {
		t.Fatalf("metadata decode: %v", err)
	}
	if metadata["session_id"] != "session-123" || metadata["org_id"] != "org-1" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestCreateSessionIncludesLiveKitJoinInfoWhenConfigured(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
		LiveKit: &LiveKitTokenIssuer{
			URL:        "wss://livekit.example.com",
			APIKey:     "api-key",
			APISecret:  "api-secret",
			TokenTTL:   time.Minute,
			RoomPrefix: "sk",
			Clock:      func() time.Time { return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) },
		},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body createSessionResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LiveKit == nil {
		t.Fatal("livekit response is nil")
	}
	if body.LiveKit.URL != "wss://livekit.example.com" || !strings.HasPrefix(body.LiveKit.Room, "sk-") {
		t.Fatalf("livekit response = %#v", body.LiveKit)
	}
	decodeAndVerifyLiveKitToken(t, body.LiveKit.Token, "api-secret")
}

func TestLiveKitTokenRefreshRequiresSessionOwner(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
		LiveKit: &LiveKitTokenIssuer{
			URL:       "wss://livekit.example.com",
			APIKey:    "api-key",
			APISecret: "api-secret",
		},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	session, _, err := manager.Create(Identity{UserID: "owner", OrgID: "org-1"})
	if err != nil {
		t.Fatalf("manager.Create: %v", err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	t.Setenv("TEST_EDGE_SECRET", "edge-secret")
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "edge_hmac", EdgeSecretEnv: "TEST_EDGE_SECRET"})(mux)

	req := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/"+session.ID+"/livekit-token", nil)
	setEdgeAuthHeaders(req, "edge-secret", Identity{UserID: "other", OrgID: "org-1"})
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func decodeAndVerifyLiveKitToken(t *testing.T, token, secret string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[2]), []byte(want)) {
		t.Fatal("token signature mismatch")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	return claims
}

func setEdgeAuthHeaders(req *http.Request, secret string, id Identity) {
	req.Header.Set("X-Edge-User-Id", id.UserID)
	req.Header.Set("X-Edge-Org-Id", id.OrgID)
	req.Header.Set("X-Edge-Plan", id.Plan)
	if id.Role != "" {
		req.Header.Set("X-Edge-Role", id.Role)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id.UserID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(id.OrgID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(id.Plan))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(id.Role))
	req.Header.Set("X-Edge-Auth-Hmac", hex.EncodeToString(mac.Sum(nil)))
}
