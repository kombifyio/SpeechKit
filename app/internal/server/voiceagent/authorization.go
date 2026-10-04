//go:build linux

package voiceagent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type authorizationExpired struct{}

func (authorizationExpired) Error() string { return "voiceagent: session authorization expired" }
func (authorizationExpired) Code() string  { return "auth_expired" }

var errAuthorizationExpired error = authorizationExpired{}

var errVoiceBudgetExpired = errors.New("voiceagent: reserved Voice quota expired")

// These claims only shorten the lifetime of a binding already authenticated
// by the edge middleware. This is not token verification or grant issuance;
// Gateway remains the authority for signature, audience, scope and revocation.
func registeredAuthorizationExpiry(lease, delegatedToken string, issuerExpiry int64) (time.Time, error) {
	leaseExpiry, err := boundTokenExpiry(lease)
	if err != nil {
		return time.Time{}, err
	}
	if issuerExpiry > 0 {
		leaseExpiry = minTime(leaseExpiry, time.Unix(issuerExpiry, 0))
	}
	tokenExpiry, err := boundTokenExpiry(delegatedToken)
	if err != nil {
		// Opaque credentials require an expiry authenticated by the edge binding.
		// A lease cannot establish the lifetime of a different credential.
		if issuerExpiry <= 0 || strings.TrimSpace(delegatedToken) == "" {
			return time.Time{}, errAuthorizationExpired
		}
		return leaseExpiry, nil
	}
	return minTime(leaseExpiry, tokenExpiry), nil
}

func boundTokenExpiry(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, errors.New("voiceagent: bound authorization expiry is unavailable")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, errors.New("voiceagent: bound authorization expiry is unavailable")
	}
	var claims struct {
		ExpiresAt int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.ExpiresAt <= 0 {
		return time.Time{}, errors.New("voiceagent: bound authorization expiry is unavailable")
	}
	return time.Unix(claims.ExpiresAt, 0), nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
