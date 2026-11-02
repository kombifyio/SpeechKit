package wssession

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

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

func TestSessionManager_RequiresSecret(t *testing.T) {
	_, err := NewSessionManager(Options{TicketSecret: []byte("short")})
	if err == nil {
		t.Fatalf("expected error for short ticket secret")
	}
}

func TestSessionManager_GeneratesSecretWhenAbsent(t *testing.T) {
	m, err := NewSessionManager(Options{})
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	if len(m.opts.TicketSecret) < 16 {
		t.Fatalf("expected auto-generated secret to be >= 16 bytes, got %d", len(m.opts.TicketSecret))
	}
}

func TestSessionManager_DefaultTicketTTL(t *testing.T) {
	m := mustManager(t, Options{})
	if m.opts.TicketTTL != 90*time.Second {
		t.Fatalf("TicketTTL = %s, want 90s default", m.opts.TicketTTL)
	}
}

func TestSessionManager_CreateAndGet(t *testing.T) {
	m := mustManager(t, Options{})
	session, ticket, err := m.Create(Identity{UserID: "u1", OrgID: "o1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if session.ID == "" || session.Owner.UserID != "u1" || session.State != StatePendingWS {
		t.Fatalf("unexpected session: %+v", session)
	}
	if ticket == "" {
		t.Fatalf("Create returned empty ticket")
	}
	got, err := m.Get(session.ID)
	if err != nil || got.ID != session.ID {
		t.Fatalf("Get: %v got=%+v", err, got)
	}
}

func TestSessionManager_CreateRejectsEmptyOwner(t *testing.T) {
	m := mustManager(t, Options{})
	if _, _, err := m.Create(Identity{}); err == nil {
		t.Fatalf("expected error when owner is empty")
	}
}

func TestSessionManager_PerIdentityLimit(t *testing.T) {
	m := mustManager(t, Options{MaxPerIdentitySessions: 2})
	for i := 0; i < 2; i++ {
		if _, _, err := m.Create(Identity{UserID: "u1"}); err != nil {
			t.Fatalf("create #%d: %v", i, err)
		}
	}
	_, _, err := m.Create(Identity{UserID: "u1"})
	if err != ErrIdentityLimitExceeded {
		t.Fatalf("expected ErrIdentityLimitExceeded, got %v", err)
	}
	// Different user is unaffected.
	if _, _, err := m.Create(Identity{UserID: "u2"}); err != nil {
		t.Fatalf("other user create: %v", err)
	}
}

func TestSessionManager_GlobalLimit(t *testing.T) {
	m := mustManager(t, Options{MaxGlobalSessions: 1})
	if _, _, err := m.Create(Identity{UserID: "u1"}); err != nil {
		t.Fatalf("create #1: %v", err)
	}
	_, _, err := m.Create(Identity{UserID: "u2"})
	if err != ErrGlobalLimitExceeded {
		t.Fatalf("expected ErrGlobalLimitExceeded, got %v", err)
	}
}

func TestSessionManager_RemoveDecrementsCounts(t *testing.T) {
	m := mustManager(t, Options{MaxPerIdentitySessions: 1})
	s1, _, _ := m.Create(Identity{UserID: "u1"})
	m.Remove(s1.ID)
	if _, _, err := m.Create(Identity{UserID: "u1"}); err != nil {
		t.Fatalf("create after remove: %v", err)
	}
}

func TestSessionManager_Attach(t *testing.T) {
	m := mustManager(t, Options{})
	s, _, _ := m.Create(Identity{UserID: "u1"})
	if err := m.Attach(s.ID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	// Second attach rejected.
	if err := m.Attach(s.ID); err != ErrSessionAlreadyActive {
		t.Fatalf("second attach: %v", err)
	}
}

func TestSessionManager_AttachUnknownFails(t *testing.T) {
	m := mustManager(t, Options{})
	if err := m.Attach("no-such-id"); err != ErrSessionNotFound {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSessionManager_VerifyTicketHappyPath(t *testing.T) {
	m := mustManager(t, Options{})
	s, ticket, _ := m.Create(Identity{UserID: "u1"})
	if err := m.VerifyTicket(s.ID, ticket); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestSessionManager_VerifyTicketRejectsWrongSession(t *testing.T) {
	m := mustManager(t, Options{})
	_, ticket, _ := m.Create(Identity{UserID: "u1"})
	if err := m.VerifyTicket("some-other-session-id", ticket); err != ErrInvalidTicket {
		t.Fatalf("expected ErrInvalidTicket, got %v", err)
	}
}

func TestSessionManager_VerifyTicketRejectsExpired(t *testing.T) {
	now := time.Now().UTC()
	clock := &mutableClock{t: now}
	m := mustManager(t, Options{TicketTTL: 10 * time.Second, Clock: clock.Now})
	s, ticket, _ := m.Create(Identity{UserID: "u1"})
	clock.Set(now.Add(20 * time.Second))
	if err := m.VerifyTicket(s.ID, ticket); err != ErrSessionExpired {
		t.Fatalf("expected ErrSessionExpired, got %v", err)
	}
}

func TestSessionManager_VerifyTicketRejectsTampered(t *testing.T) {
	m := mustManager(t, Options{})
	s, ticket, _ := m.Create(Identity{UserID: "u1"})

	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	if err != nil {
		t.Fatalf("decode ticket: %v", err)
	}
	// Flip a bit in the HMAC portion.
	raw[len(raw)-1] ^= 0x01
	tampered := base64.RawURLEncoding.EncodeToString(raw)
	if err := m.VerifyTicket(s.ID, tampered); err != ErrInvalidTicket {
		t.Fatalf("expected ErrInvalidTicket, got %v", err)
	}
}

func TestSessionManager_VerifyTicketRejectsGarbage(t *testing.T) {
	m := mustManager(t, Options{})
	for _, bad := range []string{"", "!!!", strings.Repeat("A", 16)} {
		if err := m.VerifyTicket("sid", bad); err != ErrInvalidTicket {
			t.Fatalf("expected ErrInvalidTicket for %q, got %v", bad, err)
		}
	}
}

func TestSessionManager_List(t *testing.T) {
	m := mustManager(t, Options{})
	_, _, _ = m.Create(Identity{UserID: "u1"})
	_, _, _ = m.Create(Identity{UserID: "u1"})
	_, _, _ = m.Create(Identity{UserID: "u2"})

	u1 := m.List("u1")
	if len(u1) != 2 {
		t.Fatalf("u1 list len = %d want 2", len(u1))
	}
	all := m.List("")
	if len(all) != 3 {
		t.Fatalf("all list len = %d want 3", len(all))
	}
}

func TestSessionManager_Stats(t *testing.T) {
	m := mustManager(t, Options{MaxGlobalSessions: 5, MaxPerIdentitySessions: 2})
	s1, _, _ := m.Create(Identity{UserID: "u1"})
	_, _, _ = m.Create(Identity{UserID: "u1"})
	_, _, _ = m.Create(Identity{UserID: "u2"})
	if err := m.Attach(s1.ID); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	stats := m.Stats("u1")
	if stats.TotalSessions != 3 {
		t.Fatalf("TotalSessions = %d, want 3", stats.TotalSessions)
	}
	if stats.ActiveSessions != 1 || stats.PendingSessions != 2 {
		t.Fatalf("active/pending = %d/%d, want 1/2", stats.ActiveSessions, stats.PendingSessions)
	}
	if stats.IdentitySessions != 2 {
		t.Fatalf("IdentitySessions = %d, want 2", stats.IdentitySessions)
	}
	if stats.MaxGlobalSessions != 5 || stats.MaxPerIdentitySessions != 2 {
		t.Fatalf("limits = %d/%d, want 5/2", stats.MaxGlobalSessions, stats.MaxPerIdentitySessions)
	}
}

func TestSessionManager_ConcurrentCreates(t *testing.T) {
	m := mustManager(t, Options{MaxGlobalSessions: 50, MaxPerIdentitySessions: 50})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := m.Create(Identity{UserID: "u1"}); err != nil {
				t.Errorf("concurrent create #%d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if got := len(m.List("u1")); got != 50 {
		t.Fatalf("concurrent creates produced %d sessions, want 50", got)
	}
}

func TestSessionManager_TicketStructureIsStable(t *testing.T) {
	// The ticket is 8 bytes expiry + sessionID + 32 bytes HMAC. We verify
	// the minimum decoded length so encoding changes don't silently shorten
	// the signature.
	m := mustManager(t, Options{})
	s, ticket, _ := m.Create(Identity{UserID: "u1"})
	raw, _ := base64.RawURLEncoding.DecodeString(ticket)
	if len(raw) < 8+len(s.ID)+sha256.Size {
		t.Fatalf("ticket too short: %d bytes", len(raw))
	}
}

// ── test helpers ────────────────────────────────────────────────────────────

// Public-guide identities cannot use the account preparation allowance.
func TestSessionManager_AnonymousAdmissionRetainsTotalCap(t *testing.T) {
	m := mustManager(t, Options{MaxPerIdentitySessions: 1, MaxPreparedPerIdentitySessions: 4})
	owner := Identity{UserID: "anonymous:guide", Role: "anonymous", Plan: "anonymous"}
	first, _, err := m.Create(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Create(owner); err != ErrIdentityLimitExceeded {
		t.Fatalf("second public preparation: %v", err)
	}
	if err := m.Attach(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Create(owner); err != ErrIdentityLimitExceeded {
		t.Fatalf("public standby bypassed total cap: %v", err)
	}
}

// Active admission is a quota/security boundary. A journey cannot reliably
// schedule simultaneous starts; prove the atomic handoff and failed-start
// release here, while sk-e2e proves the HTTP/standby preparation regression.
func TestSessionManager_ConcurrentConversationAdmission(t *testing.T) {
	m := mustManager(t, Options{MaxPerIdentitySessions: 1, MaxPreparedPerIdentitySessions: 4})
	var sessions []*ManagedSession
	for i := 0; i < 4; i++ {
		s, _, err := m.Create(Identity{UserID: "same-account"})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Attach(s.ID); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, s)
	}
	start := make(chan struct{})
	results := make(chan string, len(sessions))
	for _, s := range sessions {
		go func() {
			<-start
			err := m.StartConversation(s.ID)
			if err == nil {
				results <- s.ID
				return
			}
			if err != ErrIdentityLimitExceeded {
				t.Errorf("start: %v", err)
			}
			results <- ""
		}()
	}
	close(start)
	var winner string
	wins := 0
	for range sessions {
		if id := <-results; id != "" {
			winner = id
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("admitted %d concurrent conversations, want 1", wins)
	}
	stats := m.Stats("same-account")
	if stats.ActiveSessions != 1 || stats.PendingSessions != 3 {
		t.Fatalf("counts: %+v", stats)
	}
	spare, _, err := m.Create(Identity{UserID: "same-account"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Attach(spare.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.StartConversation(spare.ID); err != ErrIdentityLimitExceeded {
		t.Fatalf("start spare: %v", err)
	}
	stopped := false
	if err := m.SetConversationStop(winner, func() { stopped = true }); err != nil {
		t.Fatal(err)
	}
	m.Remove(winner)
	if !stopped {
		t.Fatal("DELETE did not request adapter shutdown")
	}
	if err := m.StartConversation(spare.ID); err != ErrIdentityLimitExceeded {
		t.Fatalf("DELETE released capacity before provider teardown: %v", err)
	}
	m.FinishConversation(winner) // provider-connect-failure/teardown path
	if err := m.StartConversation(spare.ID); err != nil {
		t.Fatalf("slot not released: %v", err)
	}
}

type mutableClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *mutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *mutableClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// D-2026-10-26-14: the running cap counts one device, never the account, and
// the global abuse ceiling counts running conversations only.
func TestSessionManager_HouseholdDevicesAdmittedIndependently(t *testing.T) {
	m := mustManager(t, Options{MaxGlobalSessions: 2, MaxPerIdentitySessions: 1, MaxPreparedPerIdentitySessions: 2})
	open := func(device string) *ManagedSession {
		t.Helper()
		s, _, err := m.Create(Identity{UserID: "household", DeviceID: device})
		if err != nil {
			t.Fatalf("prepare on %q: %v", device, err)
		}
		if err := m.Attach(s.ID); err != nil {
			t.Fatal(err)
		}
		return s
	}
	kitchen, kitchenSpare, office := open("dev_kitchen"), open("dev_kitchen"), open("dev_office")
	if _, _, err := m.Create(Identity{UserID: "household", DeviceID: "dev_kitchen"}); err != ErrIdentityLimitExceeded {
		t.Fatalf("third prepared session on one device: %v", err)
	}
	if err := m.StartConversation(kitchen.ID); err != nil {
		t.Fatalf("kitchen start: %v", err)
	}
	if err := m.StartConversation(office.ID); err != nil {
		t.Fatalf("a running kitchen conversation refused the office device: %v", err)
	}
	if err := m.StartConversation(kitchenSpare.ID); err != ErrIdentityLimitExceeded {
		t.Fatalf("second running conversation on one device: %v", err)
	}
	// Prepared sessions never take global capacity; starting one does.
	hall := open("dev_hall")
	if err := m.StartConversation(hall.ID); err != ErrGlobalLimitExceeded {
		t.Fatalf("start beyond the running ceiling: %v", err)
	}
	m.FinishConversation(office.ID)
	if err := m.StartConversation(hall.ID); err != nil {
		t.Fatalf("ceiling not released by a finished conversation: %v", err)
	}
}

// Device ids cannot multiply the per-device caps without limit: an account
// holds sessions on at most MaxDevicesPerAccount devices at once.
func TestSessionManager_AccountDeviceBound(t *testing.T) {
	m := mustManager(t, Options{MaxPreparedPerIdentitySessions: 1, MaxDevicesPerAccount: 2})
	first, _, err := m.Create(Identity{UserID: "u", DeviceID: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Create(Identity{UserID: "u", DeviceID: "b"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Create(Identity{UserID: "u", DeviceID: "c"}); err != ErrAccountDeviceLimitExceeded {
		t.Fatalf("third device: %v", err)
	}
	if _, _, err := m.Create(Identity{UserID: "other", DeviceID: "c"}); err != nil {
		t.Fatalf("another account was bounded: %v", err)
	}
	m.Remove(first.ID)
	if _, _, err := m.Create(Identity{UserID: "u", DeviceID: "c"}); err != nil {
		t.Fatalf("device bound not released: %v", err)
	}
}
