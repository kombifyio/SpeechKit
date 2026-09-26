//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
)

// csrfTokenFor derives a deterministic CSRF token from the admin
// session cookie value. Using HMAC over the session signature binds the
// CSRF cookie to its session — a stale CSRF cookie cannot validate
// against a freshly issued session, and an attacker who somehow obtains
// only the CSRF cookie (e.g. via XSS on a non-credential surface) still
// cannot forge a session.
//
// The signing key is the process-local adminSessionSigningKey: it
// rotates on every restart, so observability of a token leaks only
// within one process lifetime.
func csrfTokenFor(adminSessionToken string) string {
	mac := hmac.New(sha256.New, adminSessionSigningKey)
	mac.Write([]byte(adminSessionToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ValidateAdminCSRF returns true when the request carries a CSRF
// cookie + matching X-CSRF-Token header AND the cookie value matches
// csrfTokenFor(<session-cookie>). All comparisons are constant-time.
// Returns false when any input is missing or mismatched — callers
// should reject the request with 403.
func ValidateAdminCSRF(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get(adminCSRFHeaderName))
	if header == "" {
		return false
	}
	cookie, err := r.Cookie(adminCSRFCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return false
	}
	if !hmacEqual([]byte(cookie.Value), []byte(header)) {
		return false
	}
	sessionCookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || strings.TrimSpace(sessionCookie.Value) == "" {
		return false
	}
	want := csrfTokenFor(sessionCookie.Value)
	return hmacEqual([]byte(cookie.Value), []byte(want))
}

// EnforceAdminCSRF is the handler-level helper for endpoints that
// accept admin-session-cookie-authenticated state changes. Call it at
// the top of the handler when the resolved Identity has
// Source="admin_session"; the helper writes 403 + JSON envelope when
// the double-submit check fails and returns false (caller MUST return
// without further writes).
//
// Bearer / edge-HMAC / smoke callers are not subject to CSRF (their
// credentials are not automatically attached by the browser to
// cross-origin requests), so they should skip this check entirely.
func EnforceAdminCSRF(w http.ResponseWriter, r *http.Request) bool {
	id := IdentityFromContext(r.Context())
	if id.Source != "admin_session" {
		return true
	}
	if ValidateAdminCSRF(r) {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"error":{"code":"csrf_required","message":"X-CSRF-Token header missing or invalid"}}`))
	return false
}
