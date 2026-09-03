// Package wssession holds the transport-neutral session plumbing shared by
// the Server-Target's ticket-authenticated WebSocket surfaces (Voice Agent,
// streaming Dictation):
//
//   - A Session Manager that tracks active sessions, enforces per-identity
//     and global concurrency limits, and mints HMAC-signed one-time tickets
//     so browser clients can upgrade a WebSocket without sending
//     Authorization headers.
//   - Origin/allowlist checks and the "ticket.<value>" Sec-WebSocket-Protocol
//     carrier (see origin.go).
//   - An idle watchdog for server-side session teardown (see
//     idle_watchdog.go) and public ws(s):// URL building (see url.go).
//
// The package is deliberately free of build tags and of linux-only imports so
// its ticket and codec logic stays natively testable on every development
// host; the mode packages that consume it stay `//go:build linux`.
package wssession

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Common errors reported by the session manager.
var (
	ErrSessionNotFound       = errors.New("ws session: session not found")
	ErrSessionAlreadyActive  = errors.New("ws session: session already has an active WS connection")
	ErrSessionExpired        = errors.New("ws session: session ticket expired")
	ErrInvalidTicket         = errors.New("ws session: ticket signature or payload invalid")
	ErrGlobalLimitExceeded   = errors.New("ws session: global session limit exceeded")
	ErrIdentityLimitExceeded = errors.New("ws session: per-identity session limit exceeded")
)

// Identity is the caller's resolved identity from the auth middleware.
// Stored on each session so closes can verify ownership.
type Identity struct {
	UserID string
	OrgID  string
	Plan   string
	Role   string
}

// VoicePrefs mirrors middleware.VoicePrefs (field-for-field, so handlers can
// convert directly) without importing the linux-only middleware package —
// this package stays build-tag-free and natively testable, the same reason
// Identity above duplicates the middleware's Identity fields. The values are
// non-secret provider/persona names the fronting edge resolved for the user;
// they are captured at session-mint time because the WebSocket upgrade
// authenticates via ticket and never sees the edge headers.
type VoicePrefs struct {
	STTPrimary   string
	STTSecondary string
	VAProvider   string
	VAPersona    string
}

// VoiceAgentBinding carries a Gateway-authorized registered-agent target from
// session mint to the ticket-authenticated WebSocket. Lease and endpoint are
// memory-only and are never included in tickets, API responses, or logs.
type VoiceAgentBinding struct {
	TargetAgentID string
	Endpoint      string
	Lease         string
}

// Options configures a SessionManager.
type Options struct {
	// TicketSecret signs the one-time WS upgrade tickets. Must be at least
	// 16 bytes; when empty, the manager generates a random secret at
	// construction time (acceptable only for single-process deployments).
	TicketSecret []byte
	// TicketTTL limits how long a minted ticket remains valid. Defaults to
	// 90 seconds.
	TicketTTL time.Duration
	// MaxGlobalSessions caps concurrent sessions across all callers.
	// Defaults to 100.
	MaxGlobalSessions int
	// MaxPerIdentitySessions caps concurrent sessions per caller.
	// Defaults to 3.
	MaxPerIdentitySessions int
	// Clock is a time source; nil means time.Now. Tests can override.
	Clock func() time.Time
}

// SessionManager tracks active WebSocket sessions for one server surface.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*ManagedSession
	byUser   map[string]int
	opts     Options
}

// SessionStats reports current session occupancy and configured capacity.
type SessionStats struct {
	TotalSessions          int `json:"total_sessions"`
	ActiveSessions         int `json:"active_sessions"`
	PendingSessions        int `json:"pending_sessions"`
	IdentitySessions       int `json:"identity_sessions,omitempty"`
	MaxGlobalSessions      int `json:"max_global_sessions"`
	MaxPerIdentitySessions int `json:"max_per_identity_sessions"`
}

// ManagedSession is the manager's record for one active or pending session.
// The WebSocket handler and adapter pull additional fields (conn, pumps)
// onto this struct at handshake time.
type ManagedSession struct {
	ID string
	// AISessionID binds this voice transport session to the durable agent
	// conversation that owns its turns. Empty preserves standalone sessions.
	AISessionID string
	Owner       Identity
	CreatedAt   time.Time
	State       State
	HasWSClient bool // true once the client has upgraded

	// BridgeCredential is the opaque per-session credential the fronting
	// proxy forwarded at session creation (see middleware
	// EdgeOboSubjectTokenFromContext). The Voice Agent surface sets it at
	// upgrade time to authorize that session's calls to the external tool
	// bridge; the streaming-Dictation surface leaves it empty. MEMORY-ONLY
	// SECRET: it is never minted into tickets, never serialized to the wire
	// (listedSession and createSessionResponse deliberately exclude it),
	// never persisted, and MUST never appear in slog attributes.
	BridgeCredential string

	// VoicePrefs carries the edge-resolved user voice preferences captured at
	// session-mint time (see middleware.VoicePrefsFromContext). Mode adapters
	// use them as defaults when the client's start frame omits an explicit
	// provider or persona; explicit start-frame values always win. Non-secret
	// names only — safe to log, never keys.
	VoicePrefs VoicePrefs

	VoiceAgentBinding VoiceAgentBinding
}

// State captures the session's manager-level lifecycle. The Framework kernel
// has its own finer-grained state; this one only distinguishes pending ticket
// vs. active connection.
type State string

const (
	StatePendingWS State = "pending_ws"
	StateActive    State = "active"
	StateClosed    State = "closed"
)

// NewSessionManager constructs a manager with opts. Unset fields receive
// sensible defaults.
func NewSessionManager(opts Options) (*SessionManager, error) {
	if len(opts.TicketSecret) == 0 {
		opts.TicketSecret = make([]byte, 32)
		if _, err := rand.Read(opts.TicketSecret); err != nil {
			return nil, fmt.Errorf("ws session: generate ticket secret: %w", err)
		}
	}
	if len(opts.TicketSecret) < 16 {
		return nil, errors.New("ws session: ticket secret must be at least 16 bytes")
	}
	if opts.TicketTTL <= 0 {
		opts.TicketTTL = 90 * time.Second
	}
	if opts.MaxGlobalSessions <= 0 {
		opts.MaxGlobalSessions = 100
	}
	if opts.MaxPerIdentitySessions <= 0 {
		opts.MaxPerIdentitySessions = 3
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	return &SessionManager{
		sessions: make(map[string]*ManagedSession),
		byUser:   make(map[string]int),
		opts:     opts,
	}, nil
}

// Create registers a new pending session for the given caller and returns the
// session record plus a one-time WS upgrade ticket. Fails with a limit error
// when concurrency caps are exceeded.
func (m *SessionManager) Create(owner Identity) (*ManagedSession, string, error) {
	return m.CreateWithAISession(owner, "")
}

// CreateWithAISession registers a session already bound to its durable agent
// conversation. Binding inside the manager lock keeps List/Get snapshots from
// observing an unbound intermediate record.
func (m *SessionManager) CreateWithAISession(owner Identity, aiSessionID string) (*ManagedSession, string, error) {
	if strings.TrimSpace(owner.UserID) == "" {
		return nil, "", errors.New("ws session: owner.UserID must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reap abandoned pending sessions first so they cannot pin concurrency
	// slots against this create (fixes the 409 brick after repeated
	// mint-without-attach flows, e.g. batch-fallback clients or crashes).
	m.sweepExpiredPendingLocked()

	if len(m.sessions) >= m.opts.MaxGlobalSessions {
		return nil, "", ErrGlobalLimitExceeded
	}
	if m.byUser[owner.UserID] >= m.opts.MaxPerIdentitySessions {
		return nil, "", ErrIdentityLimitExceeded
	}

	now := m.opts.Clock().UTC()
	session := &ManagedSession{
		ID:          uuid.NewString(),
		AISessionID: strings.TrimSpace(aiSessionID),
		Owner:       owner,
		CreatedAt:   now,
		State:       StatePendingWS,
	}
	m.sessions[session.ID] = session
	m.byUser[owner.UserID]++

	ticket := m.mintTicket(session.ID, now.Add(m.opts.TicketTTL))
	return session, ticket, nil
}

// Get returns the session with the given ID, or ErrSessionNotFound.
func (m *SessionManager) Get(id string) (*ManagedSession, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

// List returns a snapshot of sessions owned by the given user (or all when
// userID is empty — reserved for admin callers).
func (m *SessionManager) List(userID string) []*ManagedSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ManagedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		if userID == "" || s.Owner.UserID == userID {
			snapshot := *s
			out = append(out, &snapshot)
		}
	}
	return out
}

// Stats returns a point-in-time session and capacity snapshot.
func (m *SessionManager) Stats(userID string) SessionStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := SessionStats{
		TotalSessions:          len(m.sessions),
		IdentitySessions:       m.byUser[userID],
		MaxGlobalSessions:      m.opts.MaxGlobalSessions,
		MaxPerIdentitySessions: m.opts.MaxPerIdentitySessions,
	}
	for _, session := range m.sessions {
		switch session.State {
		case StateActive:
			stats.ActiveSessions++
		case StatePendingWS:
			stats.PendingSessions++
		case StateClosed:
		}
	}
	return stats
}

// Attach moves a session from pending to active. Returns an error when the
// session is already attached to another WS client or has been closed.
func (m *SessionManager) Attach(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	if s.State == StateClosed {
		return ErrSessionNotFound
	}
	if s.HasWSClient {
		return ErrSessionAlreadyActive
	}
	s.HasWSClient = true
	s.State = StateActive
	return nil
}

// Remove closes and removes the session. Safe to call multiple times.
func (m *SessionManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return
	}
	s.State = StateClosed
	delete(m.sessions, id)
	m.decrementUserLocked(s.Owner.UserID)
	slog.Debug("ws session: session removed", "session_id", id, "user_id", s.Owner.UserID) // #nosec G706 -- slog writes session identifiers as structured attributes, not interpolated log text.
}

// sweepExpiredPendingLocked reaps sessions that were minted but never
// attached within the ticket TTL. Their ticket can no longer pass
// VerifyTicket, so no WS upgrade will ever claim them — without the sweep
// every abandoned session-create (client crash, network drop after mint,
// batch-only fallback) permanently consumes a per-identity slot. Caller must
// hold m.mu.
func (m *SessionManager) sweepExpiredPendingLocked() {
	cutoff := m.opts.Clock().UTC().Add(-m.opts.TicketTTL)
	for id, s := range m.sessions {
		if s.State != StatePendingWS || !s.CreatedAt.Before(cutoff) {
			continue
		}
		s.State = StateClosed
		delete(m.sessions, id)
		m.decrementUserLocked(s.Owner.UserID)
		slog.Debug("ws session: expired pending session reaped",
			"session_id", id, "user_id", s.Owner.UserID) // #nosec G706 -- structured attributes, not interpolated log text.
	}
}

// decrementUserLocked lowers the per-identity counter and drops the map entry
// at zero so byUser does not grow one entry per distinct user forever. Caller
// must hold m.mu.
func (m *SessionManager) decrementUserLocked(userID string) {
	if n := m.byUser[userID]; n > 1 {
		m.byUser[userID] = n - 1
	} else {
		delete(m.byUser, userID)
	}
}

// TicketExpiresAt reports when a ticket minted right now would expire. Session
// handlers surface this in their create responses so clients know how long the
// WS upgrade window stays open.
func (m *SessionManager) TicketExpiresAt() time.Time {
	return m.opts.Clock().UTC().Add(m.opts.TicketTTL)
}

// VerifyTicket validates a ticket string against its expected session ID.
// Returns nil when the ticket is cryptographically valid, un-expired, and
// bound to this sessionID. Mutates nothing; callers must follow up with
// Attach to mark the session active.
func (m *SessionManager) VerifyTicket(sessionID, ticket string) error {
	if strings.TrimSpace(ticket) == "" {
		return ErrInvalidTicket
	}
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil || len(raw) < 8+len(sessionID)+sha256.Size {
		return ErrInvalidTicket
	}
	expiryUint := binary.BigEndian.Uint64(raw[:8])
	const maxInt64AsUint = uint64(1<<63 - 1)
	if expiryUint > maxInt64AsUint {
		return ErrInvalidTicket
	}
	expiryUnix := int64(expiryUint) // #nosec G115 -- expiryUint is checked against MaxInt64 above.
	payloadEnd := len(raw) - sha256.Size
	sid := string(raw[8:payloadEnd])
	sig := raw[payloadEnd:]

	if sid != sessionID {
		return ErrInvalidTicket
	}
	if m.opts.Clock().UTC().Unix() > expiryUnix {
		return ErrSessionExpired
	}
	want := m.hmacTicket(raw[:payloadEnd])
	if !hmac.Equal(sig, want) {
		return ErrInvalidTicket
	}
	return nil
}

// mintTicket produces a base64url-encoded ticket: 8-byte big-endian expiry +
// sessionID + HMAC-SHA256(expiry||sessionID). Kept tight to avoid bloating
// WS URLs.
func (m *SessionManager) mintTicket(sessionID string, expiry time.Time) string {
	buf := make([]byte, 8+len(sessionID))
	expiryUnix := expiry.UTC().Unix()
	if expiryUnix < 0 {
		expiryUnix = 0
	}
	binary.BigEndian.PutUint64(buf[:8], uint64(expiryUnix)) // #nosec G115 -- negative expiries are clamped to zero above.
	copy(buf[8:], sessionID)
	mac := m.hmacTicket(buf)
	out := make([]byte, len(buf)+len(mac))
	copy(out, buf)
	copy(out[len(buf):], mac)
	return base64.RawURLEncoding.EncodeToString(out)
}

func (m *SessionManager) hmacTicket(buf []byte) []byte {
	h := hmac.New(sha256.New, m.opts.TicketSecret)
	h.Write(buf)
	return h.Sum(nil)
}
