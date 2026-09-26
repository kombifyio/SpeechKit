//go:build linux

package middleware

import (
	"testing"
)

func TestAuthStateAccessorsAndUpdates(t *testing.T) {
	t.Setenv("TEST_AUTH_STATE_BEARER", "bearer-secret")
	t.Setenv("TEST_AUTH_STATE_EDGE", "edge-secret")
	state := NewAuthState("bearer", "TEST_AUTH_STATE_BEARER", "TEST_AUTH_STATE_EDGE", "admin", "hash")

	if got := state.Mode(); got != "bearer" {
		t.Fatalf("mode = %q", got)
	}
	if got := state.BearerTokenEnv(); got != "TEST_AUTH_STATE_BEARER" {
		t.Fatalf("bearer env = %q", got)
	}
	if got := state.BearerToken(); got != "bearer-secret" {
		t.Fatalf("bearer token = %q", got)
	}
	if got := state.EdgeSecret(); got != "edge-secret" {
		t.Fatalf("edge secret = %q", got)
	}
	if got := state.AdminUsername(); got != "admin" {
		t.Fatalf("admin username = %q", got)
	}
	if got := state.AdminPasswordHash(); got != "hash" {
		t.Fatalf("admin hash = %q", got)
	}

	state.Set("edge_hmac", " ", "TEST_AUTH_STATE_EDGE_2")
	state.SetAdmin("root", "")
	t.Setenv("TEST_AUTH_STATE_EDGE_2", "edge-secret-2")

	if got := state.Mode(); got != "edge_hmac" {
		t.Fatalf("updated mode = %q", got)
	}
	if got := state.BearerTokenEnv(); got != "TEST_AUTH_STATE_BEARER" {
		t.Fatalf("blank bearer env update should be ignored, got %q", got)
	}
	if got := state.EdgeSecret(); got != "edge-secret-2" {
		t.Fatalf("updated edge secret = %q", got)
	}
	if got := state.AdminUsername(); got != "root" {
		t.Fatalf("updated admin username = %q", got)
	}
	if got := state.AdminPasswordHash(); got != "hash" {
		t.Fatalf("blank admin hash update should be ignored, got %q", got)
	}
}

func TestNilAuthStateAccessorsFailClosed(t *testing.T) {
	var state *AuthState
	if state.Mode() != "" || state.BearerTokenEnv() != "" || state.EdgeSecret() != "" || state.AdminUsername() != "" || state.AdminPasswordHash() != "" {
		t.Fatal("nil AuthState accessors should return empty values")
	}
	state.Set("bearer", "TOKEN", "EDGE")
	state.SetAdmin("admin", "hash")
}

func TestAuthState_SmokeTokenResolvesFromEnv(t *testing.T) {
	t.Setenv("TEST_SMOKE_ENV", "smoke-token-from-env")
	state := NewAuthState("bearer", "TEST_BEARER", "TEST_EDGE", "", "")
	if got := state.SmokeToken(); got != "" {
		t.Fatalf("smoke token must be empty until SetSmokeTokenEnv is called; got %q", got)
	}
	state.SetSmokeTokenEnv("TEST_SMOKE_ENV")
	if got := state.SmokeToken(); got != "smoke-token-from-env" {
		t.Fatalf("smoke token = %q, want smoke-token-from-env", got)
	}
}
