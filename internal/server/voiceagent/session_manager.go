//go:build linux

// Package voiceagent implements the Voice Agent WebSocket surface on the
// Server-Target. It provides three things:
//
//  1. A Session Manager that tracks active Voice Agent sessions, enforces
//     per-identity and global concurrency limits, and mints HMAC-signed
//     one-time tickets so browser clients can upgrade a WebSocket without
//     sending Authorization headers.
//  2. A wire protocol (see protocol.go) that carries control frames as
//     JSON and audio frames as binary.
//  3. An adapter that bridges the WebSocket to the Framework kernel's
//     internal/voiceagent.Session without the kernel needing to know
//     anything about HTTP.
//
// This file covers the Session Manager and ticket machinery. The wire
// protocol, WebSocket handler, and adapter live in sibling files.
package voiceagent

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
	ErrSessionNotFound       = errors.New("voiceagent: session not found")
	ErrSessionAlreadyActive  = errors.New("voiceagent: session already has an active WS connection")
	ErrSessionExpired        = errors.New("voiceagent: session ticket expired")
	ErrInvalidTicket         = errors.New("voiceagent: ticket signature or payload invalid")
	ErrGlobalLimitExceeded   = errors.New("voiceagent: global session limit exceeded")
	ErrIdentityLimitExceeded = errors.New("voiceagent: per-identity session limit exceeded")
)

// Identity is the caller's resolved identity from the auth middleware.
// Stored on each session so closes can verify ownership.
type Identity struct {
	UserID string
	OrgID  string
	Plan   string
	Role   string
}

// Options configures a SessionManager.
type Options struct {
	// TicketSecret signs the one-time WS upgrade tickets. Must be at least
	// 16 bytes; when empty, the manager generates a random secret at
	// construction time (acceptable only for single-process deployments).
	TicketSecret []byte
	// TicketTTL limits how long a minted ticket remains valid. Defaults to
	// 30 seconds.
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

// SessionManager tracks active Voice Agent sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*ManagedSession
	byUser   map[string]int
	opts     Options
}

// ManagedSession is the manager's record for one active or pending session.
// The WebSocket handler and adapter pull additional fields (conn, pumps)
// onto this struct at handshake time.
type ManagedSession struct {
	ID          string
	Owner       Identity
	CreatedAt   time.Time
	State       State
	HasWSClient bool // true once the client has upgraded
}

// State captures the session's manager-level lifecycle. The Framework kernel
// (internal/voiceagent.Session) has its own finer-grained state; this one
// only distinguishes pending ticket vs. active connection.
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
			return nil, fmt.Errorf("voiceagent: generate ticket secret: %w", err)
		}
	}
	if len(opts.TicketSecret) < 16 {
		return nil, errors.New("voiceagent: ticket secret must be at least 16 bytes")
	}
	if opts.TicketTTL <= 0 {
		opts.TicketTTL = 30 * time.Second
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
	if strings.TrimSpace(owner.UserID) == "" {
		return nil, "", errors.New("voiceagent: owner.UserID must not be empty")
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.sessions) >= m.opts.MaxGlobalSessions {
		return nil, "", ErrGlobalLimitExceeded
	}
	if m.byUser[owner.UserID] >= m.opts.MaxPerIdentitySessions {
		return nil, "", ErrIdentityLimitExceeded
	}

	now := m.opts.Clock().UTC()
	session := &ManagedSession{
		ID:        uuid.NewString(),
		Owner:     owner,
		CreatedAt: now,
		State:     StatePendingWS,
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
			copy := *s
			out = append(out, &copy)
		}
	}
	return out
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
	if m.byUser[s.Owner.UserID] > 0 {
		m.byUser[s.Owner.UserID]--
	}
	slog.Debug("voiceagent: session removed", "session_id", id, "user_id", s.Owner.UserID)
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
	expiryUnix := int64(binary.BigEndian.Uint64(raw[:8]))
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
	binary.BigEndian.PutUint64(buf[:8], uint64(expiry.UTC().Unix()))
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
