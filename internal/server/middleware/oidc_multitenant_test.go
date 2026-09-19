//go:build linux

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v3"
)

const (
	entraTenantA = "0f3a5c2e-1b4d-4e6f-8a9b-0c1d2e3f4a5b"
	entraTenantB = "11111111-1111-1111-1111-111111111111"
	entraIssuer  = "https://login.microsoftonline.com/{tenantid}/v2.0"
)

func (f *oidcFixture) multiTenantValidator(t *testing.T, allowed ...string) *OIDCValidator {
	t.Helper()
	v, err := NewOIDCValidator(OIDCConfig{
		JWKSURL:        f.jwks.URL,
		Issuer:         entraIssuer,
		Audience:       "speechkit",
		AllowedTenants: allowed,
		OrgClaim:       "tid",
		now:            func() time.Time { return f.now },
	})
	if err != nil {
		t.Fatalf("NewOIDCValidator: %v", err)
	}
	return v
}

func (f *oidcFixture) entraClaims(tenant string) map[string]any {
	claims := f.baseClaims()
	claims["iss"] = "https://login.microsoftonline.com/" + tenant + "/v2.0"
	claims["tid"] = tenant
	delete(claims, "org_id")
	return claims
}

func TestOIDCMultiTenant_TemplateWithoutAllowListIsRefused(t *testing.T) {
	f := newOIDCFixture(t)
	_, err := NewOIDCValidator(OIDCConfig{JWKSURL: f.jwks.URL, Issuer: entraIssuer, Audience: "speechkit"})
	if err == nil {
		t.Fatal("a {tenantid} issuer without allowed_tenants must fail closed")
	}
}

func TestOIDCMultiTenant_AllowedTenantMapsTenantToOrg(t *testing.T) {
	f := newOIDCFixture(t)
	v := f.multiTenantValidator(t, entraTenantA)
	tok := f.sign(t, jose.RS256, f.key, f.kid, f.entraClaims(entraTenantA))
	id, ok := v.Verify(bearerReq(tok))
	if !ok {
		t.Fatal("token from an allowed tenant must verify")
	}
	if id.OrgID != entraTenantA || id.UserID != "user-123" || id.Source != "oidc" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestOIDCMultiTenant_UnlistedTenantIsRejected(t *testing.T) {
	f := newOIDCFixture(t)
	v := f.multiTenantValidator(t, entraTenantA)
	tok := f.sign(t, jose.RS256, f.key, f.kid, f.entraClaims(entraTenantB))
	if _, ok := v.Verify(bearerReq(tok)); ok {
		t.Fatal("a correctly signed token from an unlisted tenant must be rejected")
	}
}

func TestOIDCMultiTenant_IssuerMustMatchTenantClaim(t *testing.T) {
	f := newOIDCFixture(t)
	v := f.multiTenantValidator(t, entraTenantA, entraTenantB)
	// Claims say tenant A, issuer says tenant B: the token was not issued
	// by the tenant it claims to belong to.
	claims := f.entraClaims(entraTenantA)
	claims["iss"] = "https://login.microsoftonline.com/" + entraTenantB + "/v2.0"
	tok := f.sign(t, jose.RS256, f.key, f.kid, claims)
	if _, ok := v.Verify(bearerReq(tok)); ok {
		t.Fatal("issuer and tid must agree")
	}
	// No tenant claim at all cannot expand the template.
	claims = f.entraClaims(entraTenantA)
	delete(claims, "tid")
	tok = f.sign(t, jose.RS256, f.key, f.kid, claims)
	if _, ok := v.Verify(bearerReq(tok)); ok {
		t.Fatal("a template needs the tenant claim")
	}
}

func TestOIDCMultiTenant_StarAcceptsEveryTenant(t *testing.T) {
	f := newOIDCFixture(t)
	v := f.multiTenantValidator(t, "*")
	for _, tenant := range []string{entraTenantA, entraTenantB} {
		tok := f.sign(t, jose.RS256, f.key, f.kid, f.entraClaims(tenant))
		id, ok := v.Verify(bearerReq(tok))
		if !ok || id.OrgID != tenant {
			t.Fatalf("tenant %s: ok=%v id=%+v", tenant, ok, id)
		}
	}
}

// TestOIDCMultiTenant_AuthChainAcceptsTenantToken runs the validator through
// the Auth middleware the way a server does, next to the static bearer of
// bearer_or_oidc.
func TestOIDCMultiTenant_AuthChainAcceptsTenantToken(t *testing.T) {
	f := newOIDCFixture(t)
	v := f.multiTenantValidator(t, entraTenantA)
	tok := f.sign(t, jose.RS256, f.key, f.kid, f.entraClaims(entraTenantA))

	var seenIdentity Identity
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenIdentity = IdentityFromContext(r.Context())
	})
	handler := Auth(AuthOptions{
		ModeProvider:        func() string { return string(AuthModeBearerOrOIDC) },
		BearerTokenProvider: func() string { return "static-service-token" },
		OIDCVerifier:        v.Verify,
	})(next)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, bearerReq(tok))
	if rec.Code != http.StatusOK || seenIdentity.Source != "oidc" || seenIdentity.OrgID != entraTenantA {
		t.Fatalf("oidc request: code=%d identity=%+v", rec.Code, seenIdentity)
	}

	seenIdentity = Identity{}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, bearerReq("static-service-token"))
	if rec.Code != http.StatusOK || seenIdentity.Source != "bearer" {
		t.Fatalf("bearer request: code=%d identity=%+v", rec.Code, seenIdentity)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, bearerReq(f.sign(t, jose.RS256, f.key, f.kid, f.entraClaims(entraTenantB))))
	if rec.Code == http.StatusOK {
		t.Fatal("a token from an unlisted tenant must not pass the auth chain")
	}
}
