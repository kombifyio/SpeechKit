//go:build linux

// Package voiceagent implements the Voice Agent WebSocket surface on the
// Server-Target. It provides three things:
//
//  1. Session tracking with per-identity and global concurrency limits plus
//     HMAC-signed one-time upgrade tickets — shared machinery that lives in
//     internal/server/wssession and is aliased here so the voiceagent API
//     stays stable for existing callers and tests.
//  2. A wire protocol (see protocol.go) that carries control frames as
//     JSON and audio frames as binary.
//  3. An adapter that bridges the WebSocket to the Framework kernel's
//     internal/voiceagent.Session without the kernel needing to know
//     anything about HTTP.
package voiceagent

import (
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/wssession"
)

// Session/ticket machinery is shared with the streaming-Dictation surface via
// internal/server/wssession. The aliases below keep this package's exported
// API identical to the pre-extraction shape.
type (
	Identity       = wssession.Identity
	Options        = wssession.Options
	SessionManager = wssession.SessionManager
	SessionStats   = wssession.SessionStats
	ManagedSession = wssession.ManagedSession
	State          = wssession.State
)

const (
	StatePendingWS = wssession.StatePendingWS
	StateActive    = wssession.StateActive
	StateClosed    = wssession.StateClosed
)

// Common errors reported by the session manager.
var (
	ErrSessionNotFound       = wssession.ErrSessionNotFound
	ErrSessionAlreadyActive  = wssession.ErrSessionAlreadyActive
	ErrSessionExpired        = wssession.ErrSessionExpired
	ErrInvalidTicket         = wssession.ErrInvalidTicket
	ErrGlobalLimitExceeded   = wssession.ErrGlobalLimitExceeded
	ErrIdentityLimitExceeded = wssession.ErrIdentityLimitExceeded
)

// NewSessionManager constructs a manager with opts. Unset fields receive
// sensible defaults.
func NewSessionManager(opts Options) (*SessionManager, error) {
	return wssession.NewSessionManager(opts)
}

// idleWatchdog aliases keep adapter.go and its tests unchanged after the
// watchdog moved to wssession.
type idleWatchdog = wssession.IdleWatchdog

func newIdleWatchdog(timeout time.Duration) *idleWatchdog {
	return wssession.NewIdleWatchdog(timeout)
}
