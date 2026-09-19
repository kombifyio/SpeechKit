package middleware

import (
	"strings"
	"testing"
)

func TestOIDCIssuerMatcherExactIssuerIgnoresTenant(t *testing.T) {
	m, err := newOIDCIssuerMatcher("https://issuer.test", nil)
	if err != nil {
		t.Fatalf("newOIDCIssuerMatcher: %v", err)
	}
	for _, tenant := range []string{"", "acme", "0f3a5c2e-1b4d-4e6f-8a9b-0c1d2e3f4a5b"} {
		iss, ok := m.expectedIssuer(tenant)
		if !ok || iss != "https://issuer.test" {
			t.Fatalf("tenant %q: got (%q, %v), want exact issuer", tenant, iss, ok)
		}
	}
}

func TestOIDCIssuerMatcherTemplateNeedsAllowList(t *testing.T) {
	_, err := newOIDCIssuerMatcher("https://login.microsoftonline.com/{tenantid}/v2.0", nil)
	if err == nil || !strings.Contains(err.Error(), "allowed_tenants") {
		t.Fatalf("template without allow list must fail closed, got %v", err)
	}
	if _, err := newOIDCIssuerMatcher("https://login.microsoftonline.com/{tenantid}/v2.0", []string{" ", ""}); err == nil {
		t.Fatal("blank allow list entries must not count")
	}
	if _, err := newOIDCIssuerMatcher("", []string{"*"}); err == nil {
		t.Fatal("empty issuer must be rejected")
	}
	if _, err := newOIDCIssuerMatcher("https://x/{tenantid}", []string{"not a tenant!"}); err == nil {
		t.Fatal("malformed allow list entry must be rejected")
	}
}

func TestOIDCIssuerMatcherTemplateExpandsAllowedTenantsOnly(t *testing.T) {
	m, err := newOIDCIssuerMatcher("https://login.microsoftonline.com/{tenantid}/v2.0",
		[]string{"0F3A5C2E-1B4D-4E6F-8A9B-0C1D2E3F4A5B", "contoso.onmicrosoft.com"})
	if err != nil {
		t.Fatalf("newOIDCIssuerMatcher: %v", err)
	}
	iss, ok := m.expectedIssuer("0f3a5c2e-1b4d-4e6f-8a9b-0c1d2e3f4a5b")
	if !ok || iss != "https://login.microsoftonline.com/0f3a5c2e-1b4d-4e6f-8a9b-0c1d2e3f4a5b/v2.0" {
		t.Fatalf("allowed tenant: got (%q, %v)", iss, ok)
	}
	if iss, ok := m.expectedIssuer("Contoso.onmicrosoft.com"); !ok || !strings.Contains(iss, "/contoso.onmicrosoft.com/") {
		t.Fatalf("domain tenant (case-insensitive): got (%q, %v)", iss, ok)
	}
	for _, tenant := range []string{"", "11111111-1111-1111-1111-111111111111", "evil/../common", "common"} {
		if iss, ok := m.expectedIssuer(tenant); ok {
			t.Fatalf("tenant %q must be rejected, got issuer %q", tenant, iss)
		}
	}
}

func TestOIDCIssuerMatcherStarAcceptsEveryWellFormedTenant(t *testing.T) {
	m, err := newOIDCIssuerMatcher("https://login.microsoftonline.com/{tenantid}/v2.0", []string{"*"})
	if err != nil {
		t.Fatalf("newOIDCIssuerMatcher: %v", err)
	}
	if iss, ok := m.expectedIssuer("11111111-1111-1111-1111-111111111111"); !ok || !strings.Contains(iss, "/11111111-1111-1111-1111-111111111111/") {
		t.Fatalf("any tenant: got (%q, %v)", iss, ok)
	}
	if _, ok := m.expectedIssuer(""); ok {
		t.Fatal("a template still needs a tenant claim to expand")
	}
	if _, ok := m.expectedIssuer("a b"); ok {
		t.Fatal("a malformed tenant must never be substituted into the issuer")
	}
}

func TestOIDCIssuerMatcherExactIssuerWithAllowListRestrictsTenants(t *testing.T) {
	m, err := newOIDCIssuerMatcher("https://issuer.test", []string{"acme"})
	if err != nil {
		t.Fatalf("newOIDCIssuerMatcher: %v", err)
	}
	if iss, ok := m.expectedIssuer("acme"); !ok || iss != "https://issuer.test" {
		t.Fatalf("listed tenant: got (%q, %v)", iss, ok)
	}
	if _, ok := m.expectedIssuer("globex"); ok {
		t.Fatal("unlisted tenant must be rejected when an allow list is configured")
	}
}
