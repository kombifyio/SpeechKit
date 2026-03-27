package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/auth"
)

type fakeAuthProvider struct {
	startResp  *auth.DeviceCodeResponse
	pollResp   *auth.TokenPair
	pollErr    error
	identity   *auth.Identity
	identityErr error
	loggedIn   bool
}

func (f *fakeAuthProvider) StartDeviceCodeFlow(context.Context) (*auth.DeviceCodeResponse, error) {
	return f.startResp, nil
}

func (f *fakeAuthProvider) PollDeviceCode(context.Context, string) (*auth.TokenPair, error) {
	return f.pollResp, f.pollErr
}

func (f *fakeAuthProvider) GetAccessToken(context.Context) (string, error) {
	if f.pollResp == nil {
		return "", errors.New("no token")
	}
	return f.pollResp.AccessToken, nil
}

func (f *fakeAuthProvider) GetIdentity(context.Context) (*auth.Identity, error) {
	return f.identity, f.identityErr
}

func (f *fakeAuthProvider) Logout(context.Context) error { return nil }
func (f *fakeAuthProvider) IsLoggedIn() bool             { return f.loggedIn }

func TestAuthPollDoesNotReturnTokenPair(t *testing.T) {
	auth.RegisterAuthProvider(&fakeAuthProvider{
		pollResp: &auth.TokenPair{
			AccessToken:  "access-secret",
			RefreshToken: "refresh-secret",
			ExpiresAt:    time.Unix(1735689600, 0),
			UserID:       "user-123",
		},
		identity: &auth.Identity{
			UserID:    "user-123",
			OrgID:     "org-456",
			Email:     "user@example.com",
			Roles:     []string{"member"},
			Plan:      "pro",
			ExpiresAt: time.Unix(1735689600, 0),
		},
		loggedIn: true,
	})
	defer auth.RegisterAuthProvider(nil)

	mux := http.NewServeMux()
	registerAuthRoutes(mux)

	form := url.Values{}
	form.Set("device_code", "device-code-123")
	req := httptest.NewRequest(http.MethodPost, "/auth/poll", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if strings.Contains(body, "access-secret") || strings.Contains(body, "refresh-secret") {
		t.Fatalf("response leaked raw tokens: %s", body)
	}

	var payload struct {
		Pending       bool           `json:"pending"`
		Authenticated bool           `json:"authenticated"`
		Identity      *auth.Identity `json:"identity"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Pending {
		t.Fatal("poll response should not be pending after successful login")
	}
	if !payload.Authenticated {
		t.Fatal("poll response should mark the user as authenticated")
	}
	if payload.Identity == nil || payload.Identity.Email != "user@example.com" {
		t.Fatalf("identity = %#v", payload.Identity)
	}
}
