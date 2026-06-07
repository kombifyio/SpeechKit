//go:build linux

package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
)

// oidcJWKSCacheTTL bounds how long a fetched JWKS is trusted before a refresh.
// A token whose kid is missing from the cache also forces an immediate refresh,
// so key rotation is picked up without waiting for the TTL.
const oidcJWKSCacheTTL = 5 * time.Minute

// oidcAllowedAlgorithms restricts accepted signature algorithms to asymmetric
// schemes. JWKS publishes public keys, so permitting a symmetric alg (e.g.
// HS256) would invite an algorithm-confusion attack where the public key is
// abused as an HMAC secret.
var oidcAllowedAlgorithms = map[string]struct{}{
	"RS256": {}, "RS384": {}, "RS512": {},
	"PS256": {}, "PS384": {}, "PS512": {},
	"ES256": {}, "ES384": {}, "ES512": {},
}

// OIDCConfig configures Bearer-JWT validation against an external identity
// provider. JWKSURL, Issuer, and Audience are required.
type OIDCConfig struct {
	JWKSURL          string
	Issuer           string
	Audience         string
	ClockSkewSeconds int    // tolerated exp/nbf skew; defaults to 60s
	OrgClaim         string // claim mapped to Identity.OrgID; defaults to "org_id"
	RoleClaim        string // claim mapped to Identity.Role; defaults to "role"

	// HTTPClient and now are injectable for tests; production defaults apply
	// when nil.
	HTTPClient *http.Client
	now        func() time.Time
}

// OIDCValidator validates Bearer JWTs against a cached JWKS and maps their
// claims to an [Identity]. It is safe for concurrent use.
type OIDCValidator struct {
	cfg        OIDCConfig
	httpClient *http.Client
	now        func() time.Time
	leeway     time.Duration

	mu        sync.RWMutex
	keys      *jose.JSONWebKeySet
	fetchedAt time.Time
}

// NewOIDCValidator constructs a validator, returning an error when required
// configuration is missing.
func NewOIDCValidator(cfg OIDCConfig) (*OIDCValidator, error) {
	if strings.TrimSpace(cfg.JWKSURL) == "" {
		return nil, fmt.Errorf("oidc: jwks_url is required")
	}
	if strings.TrimSpace(cfg.Issuer) == "" {
		return nil, fmt.Errorf("oidc: issuer is required")
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		return nil, fmt.Errorf("oidc: audience is required")
	}

	v := &OIDCValidator{cfg: cfg}
	v.httpClient = cfg.HTTPClient
	if v.httpClient == nil {
		v.httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	v.now = cfg.now
	if v.now == nil {
		v.now = time.Now
	}
	skew := cfg.ClockSkewSeconds
	if skew <= 0 {
		skew = 60
	}
	v.leeway = time.Duration(skew) * time.Second
	if strings.TrimSpace(v.cfg.OrgClaim) == "" {
		v.cfg.OrgClaim = "org_id"
	}
	if strings.TrimSpace(v.cfg.RoleClaim) == "" {
		v.cfg.RoleClaim = "role"
	}
	return v, nil
}

// Verify satisfies the AuthOptions.OIDCVerifier contract. It returns a mapped
// Identity only when the request carries a Bearer JWT that is correctly signed
// by a JWKS key and whose issuer, audience, and time claims validate.
func (v *OIDCValidator) Verify(r *http.Request) (Identity, bool) {
	raw := oidcBearerToken(r)
	if raw == "" {
		return Identity{}, false
	}
	tok, err := jwt.ParseSigned(raw)
	if err != nil || len(tok.Headers) == 0 {
		return Identity{}, false
	}
	if _, ok := oidcAllowedAlgorithms[tok.Headers[0].Algorithm]; !ok {
		return Identity{}, false
	}

	key, ok := v.keyForToken(r.Context(), tok.Headers[0].KeyID)
	if !ok {
		return Identity{}, false
	}

	var registered jwt.Claims
	all := map[string]any{}
	if err := tok.Claims(key, &registered, &all); err != nil {
		return Identity{}, false
	}
	if err := registered.ValidateWithLeeway(jwt.Expected{
		Issuer:   v.cfg.Issuer,
		Audience: jwt.Audience{v.cfg.Audience},
		Time:     v.now(),
	}, v.leeway); err != nil {
		return Identity{}, false
	}
	if strings.TrimSpace(registered.Subject) == "" {
		return Identity{}, false
	}

	org := oidcStringClaim(all, v.cfg.OrgClaim)
	if org == "" {
		org = "default"
	}
	return Identity{
		UserID: registered.Subject,
		OrgID:  org,
		Role:   oidcStringClaim(all, v.cfg.RoleClaim),
		Plan:   "oidc",
		Source: "oidc",
	}, true
}

func (v *OIDCValidator) keyForToken(ctx context.Context, kid string) (any, bool) {
	if key, ok := v.cachedKey(kid); ok {
		return key, true
	}
	// Cache miss, stale, or rotated key — refresh once, then look up again.
	if err := v.refresh(ctx); err != nil {
		return nil, false
	}
	return v.cachedKey(kid)
}

func (v *OIDCValidator) cachedKey(kid string) (any, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if v.keys == nil || v.now().Sub(v.fetchedAt) > oidcJWKSCacheTTL {
		return nil, false
	}
	matches := v.keys.Key(kid)
	if len(matches) == 0 {
		return nil, false
	}
	return matches[0].Key, true
}

func (v *OIDCValidator) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.cfg.JWKSURL, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := v.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("oidc: jwks fetch returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(body, &set); err != nil {
		return fmt.Errorf("oidc: decode jwks: %w", err)
	}
	v.mu.Lock()
	v.keys = &set
	v.fetchedAt = v.now()
	v.mu.Unlock()
	return nil
}

// oidcBearerToken returns the raw token from an "Authorization: Bearer <jwt>"
// header, or "" when absent.
func oidcBearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// oidcStringClaim extracts a string claim, tolerating providers that emit it as
// the first element of an array (e.g. some group/role claims).
func oidcStringClaim(claims map[string]any, key string) string {
	if key == "" {
		return ""
	}
	switch val := claims[key].(type) {
	case string:
		return strings.TrimSpace(val)
	case []any:
		if len(val) > 0 {
			if s, ok := val[0].(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
