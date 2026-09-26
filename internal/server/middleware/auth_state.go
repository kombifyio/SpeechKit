//go:build linux

package middleware

import (
	"os"
	"strings"
	"sync"
)

// AuthState is a concurrency-safe view of the mutable auth config used by the
// server setup flow. It stores env var names, not secret values.
type AuthState struct {
	mu                sync.RWMutex
	mode              string
	bearerTokenEnv    string
	edgeSecretEnv     string
	smokeTokenEnv     string
	adminUsername     string
	adminPasswordHash string
}

func NewAuthState(mode, bearerTokenEnv, edgeSecretEnv, adminUsername, adminPasswordHash string) *AuthState {
	return &AuthState{
		mode:              strings.TrimSpace(mode),
		bearerTokenEnv:    strings.TrimSpace(bearerTokenEnv),
		edgeSecretEnv:     strings.TrimSpace(edgeSecretEnv),
		adminUsername:     strings.TrimSpace(adminUsername),
		adminPasswordHash: strings.TrimSpace(adminPasswordHash),
	}
}

// SetSmokeTokenEnv records the env var name holding the optional public
// demo token. Empty string disables smoke-from-page authentication.
func (s *AuthState) SetSmokeTokenEnv(envName string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.smokeTokenEnv = strings.TrimSpace(envName)
}

// SmokeTokenEnv returns the configured env var name for the demo token.
func (s *AuthState) SmokeTokenEnv() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.smokeTokenEnv
}

// SmokeToken resolves the current value of the demo bearer token, or "" if
// the env var is unset / unconfigured.
func (s *AuthState) SmokeToken() string {
	envName := s.SmokeTokenEnv()
	if envName == "" {
		return ""
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func (s *AuthState) Mode() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode
}

func (s *AuthState) BearerTokenEnv() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bearerTokenEnv
}

func (s *AuthState) BearerToken() string {
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(s.BearerTokenEnv())))
}

func (s *AuthState) EdgeSecret() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	envName := s.edgeSecretEnv
	s.mu.RUnlock()
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(envName)))
}

func (s *AuthState) AdminUsername() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adminUsername
}

func (s *AuthState) AdminPasswordHash() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adminPasswordHash
}

func (s *AuthState) Set(mode, bearerTokenEnv, edgeSecretEnv string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value := strings.TrimSpace(mode); value != "" {
		s.mode = value
	}
	if value := strings.TrimSpace(bearerTokenEnv); value != "" {
		s.bearerTokenEnv = value
	}
	if value := strings.TrimSpace(edgeSecretEnv); value != "" {
		s.edgeSecretEnv = value
	}
}

func (s *AuthState) SetAdmin(username, passwordHash string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if value := strings.TrimSpace(username); value != "" {
		s.adminUsername = value
	}
	if value := strings.TrimSpace(passwordHash); value != "" {
		s.adminPasswordHash = value
	}
}
