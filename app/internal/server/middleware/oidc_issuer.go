package middleware

// The issuer matcher is build-tag neutral so its tests run on every
// platform; the validator that uses it (oidc.go) is part of the Linux-only
// server surface.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// oidcTenantPlaceholder is the token in an issuer template that stands
	// for the caller's tenant id, e.g.
	// "https://login.microsoftonline.com/{tenantid}/v2.0" for Microsoft
	// Entra, whose issuer differs per tenant while one JWKS serves them all.
	// config.ServerOIDCTenantPlaceholder spells the same literal for the
	// configuration layer.
	oidcTenantPlaceholder = "{tenantid}"
	// oidcAllowAnyTenant is the allowed_tenants entry that opts a deployment
	// into every tenant the identity provider serves.
	oidcAllowAnyTenant = "*"
)

// oidcTenantPattern bounds what may be substituted into an issuer: a tenant
// id (GUID) or a DNS name. Anything else could turn the template into an
// issuer the operator never meant to trust.
var oidcTenantPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)

// oidcIssuerMatcher decides which "iss" claim a token must carry. A plain
// issuer is compared exactly. A template containing {tenantid} is expanded
// with the token's tenant claim, and only tenants on the allow list (or every
// tenant with "*") are accepted, so a multi-tenant identity provider never
// becomes "anyone with an account there" by omission.
type oidcIssuerMatcher struct {
	template  string
	multi     bool
	anyTenant bool
	allowed   map[string]struct{}
}

func newOIDCIssuerMatcher(issuer string, allowedTenants []string) (oidcIssuerMatcher, error) {
	m := oidcIssuerMatcher{template: strings.TrimSpace(issuer), allowed: map[string]struct{}{}}
	if m.template == "" {
		return m, errors.New("oidc: issuer is required")
	}
	m.multi = strings.Contains(m.template, oidcTenantPlaceholder)
	for _, raw := range allowedTenants {
		tenant := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case tenant == "":
			continue
		case tenant == oidcAllowAnyTenant:
			m.anyTenant = true
		case !oidcTenantPattern.MatchString(tenant):
			return m, fmt.Errorf("oidc: allowed_tenants entry %q is neither a tenant id nor a domain", raw)
		default:
			m.allowed[tenant] = struct{}{}
		}
	}
	if m.multi && !m.anyTenant && len(m.allowed) == 0 {
		return m, fmt.Errorf("oidc: issuer %q is a multi-tenant template; set allowed_tenants to the tenant ids to accept, or [\"*\"] for every tenant", m.template)
	}
	return m, nil
}

// expectedIssuer returns the issuer a token from tenant must carry, or false
// when the tenant is not acceptable. Tenant checks only apply when the
// configuration asked for them (a template, or an explicit allow list), so a
// single-tenant issuer keeps working for providers without a tenant claim.
func (m oidcIssuerMatcher) expectedIssuer(tenant string) (string, bool) {
	tenant = strings.ToLower(strings.TrimSpace(tenant))
	enforce := m.multi || len(m.allowed) > 0
	if !enforce {
		return m.template, true
	}
	if tenant == "" || !oidcTenantPattern.MatchString(tenant) {
		return "", false
	}
	if !m.anyTenant {
		if _, ok := m.allowed[tenant]; !ok {
			return "", false
		}
	}
	if !m.multi {
		return m.template, true
	}
	return strings.ReplaceAll(m.template, oidcTenantPlaceholder, tenant), true
}
