//go:build linux

package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
)

type oidcFixture struct {
	key  *rsa.PrivateKey
	kid  string
	jwks *httptest.Server
	now  time.Time
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	f := &oidcFixture{key: key, kid: "test-kid-1", now: time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)}
	f.jwks = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: f.key.Public(), KeyID: f.kid, Algorithm: "RS256", Use: "sig",
		}}}
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(f.jwks.Close)
	return f
}

func (f *oidcFixture) validator(t *testing.T) *OIDCValidator {
	t.Helper()
	v, err := NewOIDCValidator(OIDCConfig{
		JWKSURL:  f.jwks.URL,
		Issuer:   "https://issuer.test",
		Audience: "speechkit",
		now:      func() time.Time { return f.now },
	})
	if err != nil {
		t.Fatalf("NewOIDCValidator: %v", err)
	}
	return v
}

func (f *oidcFixture) sign(t *testing.T, alg jose.SignatureAlgorithm, key any, kid string, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return raw
}

func (f *oidcFixture) baseClaims() map[string]any {
	return map[string]any{
		"iss":    "https://issuer.test",
		"aud":    "speechkit",
		"sub":    "user-123",
		"org_id": "acme",
		"role":   "admin",
		"exp":    f.now.Add(time.Hour).Unix(),
		"iat":    f.now.Add(-time.Minute).Unix(),
		"nbf":    f.now.Add(-time.Minute).Unix(),
	}
}

func bearerReq(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestOIDC_ValidTokenMapsIdentity(t *testing.T) {
	f := newOIDCFixture(t)
	tok := f.sign(t, jose.RS256, f.key, f.kid, f.baseClaims())
	id, ok := f.validator(t).Verify(bearerReq(tok))
	if !ok {
		t.Fatal("valid token rejected")
	}
	if id.UserID != "user-123" || id.OrgID != "acme" || id.Role != "admin" || id.Source != "oidc" {
		t.Errorf("identity = %+v", id)
	}
}

func TestOIDC_Rejections(t *testing.T) {
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	cases := []struct {
		name   string
		alg    jose.SignatureAlgorithm
		key    any    // nil => the fixture's correct private key
		kid    string // "" => the fixture's published kid
		mutate func(f *oidcFixture, c map[string]any)
	}{
		{name: "expired", alg: jose.RS256, mutate: func(f *oidcFixture, c map[string]any) { c["exp"] = f.now.Add(-time.Hour).Unix() }},
		{name: "wrong_audience", alg: jose.RS256, mutate: func(_ *oidcFixture, c map[string]any) { c["aud"] = "someone-else" }},
		{name: "wrong_issuer", alg: jose.RS256, mutate: func(_ *oidcFixture, c map[string]any) { c["iss"] = "https://evil.test" }},
		{name: "missing_subject", alg: jose.RS256, mutate: func(_ *oidcFixture, c map[string]any) { delete(c, "sub") }},
		{name: "unknown_kid", alg: jose.RS256, kid: "rotated-out-kid"},
		{name: "wrong_signing_key", alg: jose.RS256, key: otherKey},
		{name: "symmetric_alg_confusion", alg: jose.HS256, key: []byte("0123456789abcdef0123456789abcdef")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOIDCFixture(t)
			c := f.baseClaims()
			if tc.mutate != nil {
				tc.mutate(f, c)
			}
			key := tc.key
			if key == nil {
				key = f.key
			}
			kid := tc.kid
			if kid == "" {
				kid = f.kid
			}
			tok := f.sign(t, tc.alg, key, kid, c)
			if _, ok := f.validator(t).Verify(bearerReq(tok)); ok {
				t.Errorf("%s: token accepted but should be rejected", tc.name)
			}
		})
	}
}

func TestOIDC_NoAuthorizationHeader(t *testing.T) {
	f := newOIDCFixture(t)
	if _, ok := f.validator(t).Verify(bearerReq("")); ok {
		t.Error("request without a Bearer token was accepted")
	}
}

func TestOIDC_CustomClaimMappingAndOrgDefault(t *testing.T) {
	f := newOIDCFixture(t)
	v, err := NewOIDCValidator(OIDCConfig{
		JWKSURL: f.jwks.URL, Issuer: "https://issuer.test", Audience: "speechkit",
		OrgClaim: "tenant", RoleClaim: "groups", now: func() time.Time { return f.now },
	})
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	c := f.baseClaims()
	delete(c, "org_id")
	c["tenant"] = "tenant-7"
	c["groups"] = []any{"editor", "viewer"} // first element wins
	id, ok := v.Verify(bearerReq(f.sign(t, jose.RS256, f.key, f.kid, c)))
	if !ok {
		t.Fatal("token rejected")
	}
	if id.OrgID != "tenant-7" || id.Role != "editor" {
		t.Errorf("custom claim mapping wrong: %+v", id)
	}
}

func TestOIDC_MissingConfigErrors(t *testing.T) {
	if _, err := NewOIDCValidator(OIDCConfig{Issuer: "x", Audience: "y"}); err == nil {
		t.Error("missing jwks_url should error")
	}
	if _, err := NewOIDCValidator(OIDCConfig{JWKSURL: "x", Audience: "y"}); err == nil {
		t.Error("missing issuer should error")
	}
	if _, err := NewOIDCValidator(OIDCConfig{JWKSURL: "x", Issuer: "y"}); err == nil {
		t.Error("missing audience should error")
	}
}
