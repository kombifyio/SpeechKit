//go:build linux

package voiceagent

import "testing"

// mustManager builds a SessionManager with a deterministic test secret. The
// session-manager behavior tests themselves moved to
// internal/server/wssession; this helper stays because the WS handler and
// LiveKit tests construct managers constantly.
func mustManager(t *testing.T, opts Options) *SessionManager {
	t.Helper()
	if len(opts.TicketSecret) == 0 {
		opts.TicketSecret = []byte("super-secret-key-16+bytes")
	}
	m, err := NewSessionManager(opts)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	return m
}
