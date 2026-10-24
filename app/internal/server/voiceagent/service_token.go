//go:build linux

package voiceagent

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// ServiceTokenSigner selects the configured service identity without changing
// the caller's claims or authorization lifetime.
type ServiceTokenSigner struct {
	secret     []byte
	privateKey ed25519.PrivateKey
	keyID      string
}

func NewServiceTokenSigner(secret, encodedPrivateKey string) (*ServiceTokenSigner, error) {
	if encodedPrivateKey != "" {
		kid, encoded, ok := strings.Cut(encodedPrivateKey, "=")
		if !ok || !validServiceTokenKeyID(kid) {
			return nil, errors.New("service auth Ed25519 private key requires a key id and raw base64url seed")
		}
		seed, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
		if err != nil || base64.RawURLEncoding.EncodeToString(seed) != encoded || len(seed) != ed25519.SeedSize {
			return nil, errors.New("service auth Ed25519 private key requires a canonical raw base64url 32-byte seed")
		}
		return &ServiceTokenSigner{privateKey: ed25519.NewKeyFromSeed(seed), keyID: kid}, nil
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, errors.New("service auth signing credential is missing")
	}
	return &ServiceTokenSigner{secret: []byte(secret)}, nil
}

func (s *ServiceTokenSigner) Sign(claims jwt.MapClaims) (string, error) {
	if s == nil {
		return "", errors.New("service auth signing credential is missing")
	}
	if len(s.privateKey) != 0 {
		token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
		token.Header["kid"] = s.keyID
		return token.SignedString(s.privateKey)
	}
	if len(s.secret) == 0 {
		return "", errors.New("service auth signing credential is missing")
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

func validServiceTokenKeyID(kid string) bool {
	if len(kid) < 1 || len(kid) > 64 {
		return false
	}
	for i := 0; i < len(kid); i++ {
		c := kid[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
