//go:build linux

package middleware

import (
	"context"
)

// Identity is attached to the request context by Auth and consumed by mode
// handlers for rate-limit keying, session ownership, and audit logs.
type Identity struct {
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`
	Plan   string `json:"plan"`
	Role   string `json:"role,omitempty"` // "admin" | "" (default)
	Source string `json:"source"`         // "none" | "bearer" | "edge_hmac" | "basic" | "admin_session"
}

type identityCtxKey struct{}

// EdgeOboSubjectTokenHeader is the default request header a fronting proxy
// uses to hand the server an opaque, short-lived per-session credential (for
// hosted Companion sessions this is the Gateway-minted AI OBO result, never
// the caller's raw login JWT). The header is honoured
// ONLY on requests whose identity was established via edge-HMAC — a bearer,
// admin, smoke, or anonymous caller can present the header but it is ignored,
// so a client can never smuggle a credential past the edge. The value is
// treated as a secret: it lives on the request context only and MUST never be
// logged or persisted.
const EdgeOboSubjectTokenHeader = "X-Edge-Obo-Subject-Token" //nolint:gosec // header name, not a credential

type edgeOboSubjectTokenCtxKey struct{}

// EdgeOboSubjectTokenFromContext returns the edge-forwarded OBO subject token
// attached by Auth, or "" when the request did not authenticate via edge-HMAC
// or the edge did not forward a token. Callers must treat the value as a
// secret (memory-only, never logged).
func EdgeOboSubjectTokenFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(edgeOboSubjectTokenCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// IdentityFromContext returns the Identity attached by Auth, or the zero
// Identity if none is present.
func IdentityFromContext(ctx context.Context) Identity {
	if v, ok := ctx.Value(identityCtxKey{}).(Identity); ok {
		return v
	}
	return Identity{}
}

// InjectIdentityForTest attaches an Identity to the context using the same
// unexported key the Auth middleware uses. Exported solely so handler tests in
// external packages can exercise endpoints that depend on
// IdentityFromContext without spinning up the full auth middleware. Production
// code MUST NOT use this; the function name and the package it lives in are
// intentionally awkward to make accidental use loud at review time.
func InjectIdentityForTest(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}
