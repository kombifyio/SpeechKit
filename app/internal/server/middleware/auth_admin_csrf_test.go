//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminCSRF_TokenIsDeterministicForSessionToken(t *testing.T) {
	tok := csrfTokenFor("session-cookie-value")
	again := csrfTokenFor("session-cookie-value")
	if tok != again {
		t.Fatalf("csrfTokenFor is not deterministic: %q vs %q", tok, again)
	}
	other := csrfTokenFor("different-session-value")
	if tok == other {
		t.Fatalf("csrfTokenFor returned the same token for different sessions")
	}
	if tok == "" {
		t.Fatal("csrfTokenFor returned empty token")
	}
}

func TestValidateAdminCSRF_RejectsMissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "sess.abc"})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: csrfTokenFor("sess.abc")})
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must require X-CSRF-Token header")
	}
}

func TestValidateAdminCSRF_RejectsMissingCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.Header.Set(adminCSRFHeaderName, "anything")
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must require CSRF cookie")
	}
}

func TestValidateAdminCSRF_RejectsHeaderCookieMismatch(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "sess.abc"})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: csrfTokenFor("sess.abc")})
	req.Header.Set(adminCSRFHeaderName, "wrong-token")
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must reject header that does not match cookie")
	}
}

func TestValidateAdminCSRF_RejectsStaleCSRFAgainstFreshSession(t *testing.T) {
	// CSRF cookie was minted for an older session; current session
	// cookie is different. The HMAC binding must catch the mismatch.
	staleCSRF := csrfTokenFor("old-session-value")
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: "new-session-value"})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: staleCSRF})
	req.Header.Set(adminCSRFHeaderName, staleCSRF)
	if ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must reject stale CSRF bound to a different session")
	}
}

func TestValidateAdminCSRF_AcceptsMatchedTriple(t *testing.T) {
	const sess = "sess.xyz"
	tok := csrfTokenFor(sess)
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookieName, Value: sess})
	req.AddCookie(&http.Cookie{Name: adminCSRFCookieName, Value: tok})
	req.Header.Set(adminCSRFHeaderName, tok)
	if !ValidateAdminCSRF(req) {
		t.Fatal("ValidateAdminCSRF must accept a matched (session-cookie, csrf-cookie, header) triple")
	}
}

func TestEnforceAdminCSRF_BypassesNonAdminSessionIdentity(t *testing.T) {
	// Bearer / edge-HMAC / smoke callers must NOT be required to send
	// X-CSRF-Token — their credentials are not browser-attached.
	for _, source := range []string{"bearer", "edge_hmac", "smoke", "none", ""} {
		t.Run("source="+source, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
			ctx := InjectIdentityForTest(req.Context(), Identity{UserID: "svc", Source: source})
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()
			if !EnforceAdminCSRF(rec, req) {
				t.Fatalf("EnforceAdminCSRF must bypass when Source=%q (no CSRF needed)", source)
			}
			if rec.Code != http.StatusOK { // unwritten = default 200
				t.Fatalf("EnforceAdminCSRF wrote a response for Source=%q (code=%d)", source, rec.Code)
			}
		})
	}
}

func TestEnforceAdminCSRF_RejectsAdminSessionWithoutToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/v1/server/settings", nil)
	ctx := InjectIdentityForTest(req.Context(), Identity{UserID: "admin", Source: "admin_session", Role: "admin"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	if EnforceAdminCSRF(rec, req) {
		t.Fatal("EnforceAdminCSRF must reject admin-session caller without CSRF token")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "csrf_required") {
		t.Fatalf("body = %q, want csrf_required envelope", rec.Body.String())
	}
}
