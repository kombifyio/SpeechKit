// Package auth provides the authentication abstraction for SpeechKit.
//
// In OSS mode: no auth provider is registered, all auth functions return nil/false.
// In kombify Cloud mode: the private kombify-speechkit module registers an
// AuthProvider via init() that uses the Device Code Flow and kombify Gateway API.
package auth

import (
	"context"
	"sync"
	"time"
)

// Identity represents an authenticated user.
type Identity struct {
	UserID    string    `json:"userId"`
	OrgID     string    `json:"orgId"`
	Email     string    `json:"email"`
	Roles     []string  `json:"roles"`
	Plan      string    `json:"plan"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// DeviceCodeResponse is returned when initiating a device code login flow.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURL string `json:"verificationUrl"`
	ExpiresIn       int    `json:"expiresIn"`
	PollInterval    int    `json:"pollInterval"`
}

// TokenPair holds access + refresh tokens from a successful auth exchange.
type TokenPair struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	UserID       string    `json:"userId"`
}

// AuthProvider is the interface that auth backends must implement.
// OSS builds have no registered provider. kombify builds register one via init().
type AuthProvider interface {
	// StartDeviceCodeFlow initiates the OAuth 2.0 Device Code flow.
	StartDeviceCodeFlow(ctx context.Context) (*DeviceCodeResponse, error)

	// PollDeviceCode checks if the user has completed browser authorization.
	// Returns ErrAuthorizationPending if the user hasn't authorized yet.
	PollDeviceCode(ctx context.Context, deviceCode string) (*TokenPair, error)

	// GetAccessToken returns a valid access token, refreshing if necessary.
	GetAccessToken(ctx context.Context) (string, error)

	// GetIdentity returns the current authenticated user's identity.
	GetIdentity(ctx context.Context) (*Identity, error)

	// Logout clears stored tokens.
	Logout(ctx context.Context) error

	// IsLoggedIn returns true if the user has a valid (or refreshable) token.
	IsLoggedIn() bool
}

// --- Provider registry (same pattern as store.RegisterBackend) ---

var (
	registeredProvider AuthProvider
	registryMu        sync.RWMutex
)

// RegisterAuthProvider is called from init() in external modules (e.g. kombify-speechkit).
func RegisterAuthProvider(p AuthProvider) {
	registryMu.Lock()
	registeredProvider = p
	registryMu.Unlock()
}

// GetAuthProvider returns the registered auth provider, or nil in OSS mode.
func GetAuthProvider() AuthProvider {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registeredProvider
}

// HasAuthProvider returns true if an auth provider has been registered.
func HasAuthProvider() bool {
	return GetAuthProvider() != nil
}
