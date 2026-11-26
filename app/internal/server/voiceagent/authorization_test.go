//go:build linux

package voiceagent

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/app/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/voiceagent/a2a"
)

func TestTurnReceiptSurvivesReopenAndIsOwnerScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipts.db")
	open := func() *store.SQLiteStore {
		s, err := store.NewSQLiteStore(store.StoreConfig{SQLitePath: path, MaxAudioStorageMB: 100})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	receipt := store.VoiceTurnReceipt{SessionID: "voice-1", TurnID: "voice-1:1", Status: "unknown"}
	if err := s.BeginVoiceTurn(context.Background(), "alice", "org-a", receipt); err != nil {
		t.Fatal(err)
	}
	otherOrg := store.VoiceTurnReceipt{SessionID: "voice-2", TurnID: "voice-2:1", Status: "unknown"}
	if err := s.BeginVoiceTurn(context.Background(), "alice", "org-b", otherOrg); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginVoiceTurn(context.Background(), "bob", "org-b", otherOrg); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginVoiceTurn(context.Background(), "alice", "org-a", receipt); err == nil {
		t.Fatal("duplicate dispatch admitted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	defer s.Close()
	h := &Handler{store: s}
	request := func(user, org, source string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/voice-1/turns/voice-1:1", nil)
		r = r.WithContext(middleware.InjectIdentityForTest(r.Context(), middleware.Identity{UserID: user, OrgID: org, Source: source}))
		w := httptest.NewRecorder()
		h.itemHandler(w, r)
		return w
	}
	if w := request("alice", "org-a", "bearer"); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"unknown"`) {
		t.Fatalf("owner: %d %s", w.Code, w.Body.String())
	}
	listRequest := httptest.NewRequest(http.MethodGet, "/v1/voiceagent/sessions/voice-1/turns", nil)
	listRequest = listRequest.WithContext(middleware.InjectIdentityForTest(listRequest.Context(), middleware.Identity{UserID: "alice", OrgID: "org-a", Source: "bearer"}))
	list := httptest.NewRecorder()
	h.itemHandler(list, listRequest)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"turn_id":"voice-1:1"`) {
		t.Fatalf("list: %d %s", list.Code, list.Body.String())
	}
	if w := request("bob", "org-a", "bearer"); w.Code != 404 {
		t.Fatalf("other user: %d", w.Code)
	}
	if w := request("alice", "org-b", "bearer"); w.Code != 404 {
		t.Fatalf("other organization: %d", w.Code)
	}
	if w := request("alice", "org-a", "none"); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if err := s.MarkVoiceTurnDispatched(context.Background(), "alice", "org-a", receipt); err != nil {
		t.Fatal(err)
	}
	if w := request("alice", "org-a", "bearer"); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"dispatched"`) {
		t.Fatalf("dispatched: %d %s", w.Code, w.Body.String())
	}
	if err := s.CompleteVoiceTurn(context.Background(), "alice", "org-a", receipt); err != nil {
		t.Fatal(err)
	}
	if w := request("alice", "org-a", "bearer"); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"completed"`) {
		t.Fatalf("completed: %d %s", w.Code, w.Body.String())
	}
	if _, err := s.DeleteSubject(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	if w := request("alice", "org-a", "bearer"); w.Code != 404 {
		t.Fatalf("erased: %d", w.Code)
	}
	if _, err := s.GetVoiceTurn(context.Background(), "alice", "org-b", "voice-2", "voice-2:1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("other org not erased: %v", err)
	}
	if _, err := s.GetVoiceTurn(context.Background(), "bob", "org-b", "voice-2", "voice-2:1"); err != nil {
		t.Fatalf("unrelated owner lost: %v", err)
	}
}

func boundTestToken(expiresAt int64) string {
	payload, _ := json.Marshal(map[string]any{"exp": expiresAt, "capabilities": []string{"agent.conversation"}})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func startReliabilityAdapter(t *testing.T, provider LiveProviderAdapter, session *ManagedSession, timeout time.Duration) (*websocket.Conn, <-chan struct{}) {
	t.Helper()
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		adapter := &Adapter{Session: session, Conn: conn, Provider: provider, Persona: &fakeResolver{}, MaxDuration: time.Hour, ConnectTimeout: timeout, OnClose: func() { close(done) }}
		adapter.Run(r.Context())
	}))
	t.Cleanup(server.Close)
	conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn, done
}

func TestRegisteredSessionEndsAtEarliestAuthorizationExpiry(t *testing.T) {
	for _, first := range []string{"lease", "jwt", "issuer opaque", "issuer cannot extend", "lease before start", "generic budget", "registered budget", "budget before start"} {
		t.Run(first, func(t *testing.T) {
			expiry := time.Now().Add(2 * time.Second).Unix()
			leaseExpiry, jwtExpiry, issuerExpiry := expiry+600, expiry+600, int64(0)
			token := ""
			switch first {
			case "lease", "lease before start":
				leaseExpiry = expiry
			case "jwt":
				jwtExpiry = expiry
			case "issuer opaque":
				issuerExpiry = expiry
				token = "opaque-credential"
			case "issuer cannot extend":
				leaseExpiry = expiry
				issuerExpiry = expiry + 600
			}
			if token == "" {
				token = boundTestToken(jwtExpiry)
			}
			session := &ManagedSession{ID: "expiry-session", DelegatedCredential: token}
			session.VoiceAgentBinding.TargetAgentID = "test-agent"
			session.VoiceAgentBinding.Lease = boundTestToken(leaseExpiry)
			session.VoiceAgentBinding.CredentialExpiresAt = issuerExpiry
			budget := strings.Contains(first, "budget")
			if budget {
				session.VoiceBudget.ReservationID = "budget-reservation"
				session.VoiceBudget.ExpiresAt = expiry
				if first != "registered budget" {
					session.VoiceAgentBinding.TargetAgentID = ""
				}
			}
			provider := newFakeProvider()
			conn, done := startReliabilityAdapter(t, provider, session, 0)
			beforeStart := first == "lease before start" || first == "budget before start"
			failureCode, endReason := "auth_expired", "authorization_expired"
			if budget {
				failureCode, endReason = "voice_budget_exhausted", "voice_budget_exhausted"
			}
			if !beforeStart {
				sendStart(t, conn, StartFrame{})
				var ready StateFrame
				readJSONFrame(t, conn, &ready)
				if ready.EventType != EventSessionReady {
					t.Fatal("authorized session did not become usable")
				}
			}
			var end SessionEndFrame
			// Read past the declared expiry with a bounded scheduling margin;
			// a fixed two-second read can race this two-second token itself.
			endCtx, endCancel := context.WithDeadline(context.Background(), time.Unix(expiry, 0).Add(time.Second))
			defer endCancel()
			if beforeStart {
				kind, data, err := conn.Read(endCtx)
				var failure ErrorFrame
				if err != nil || kind != websocket.MessageText || json.Unmarshal(data, &failure) != nil ||
					!failure.Fatal || failure.Code != failureCode {
					t.Fatalf("pre-start expiry did not deliver fatal authorization failure: frame=%+v err=%v", failure, err)
				}
			}
			kind, data, err := conn.Read(endCtx)
			if err != nil || kind != websocket.MessageText || json.Unmarshal(data, &end) != nil {
				t.Fatalf("read authorization expiry: kind=%v err=%v", kind, err)
			}
			if end.Type != MsgSessionEnd || end.Reason != endReason {
				t.Fatalf("expiry did not end session: %+v", end)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, _, err := conn.Read(ctx); err == nil {
				t.Fatal("downlink continued after terminal frame")
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("expired session was not removed")
			}
			provider.mu.Lock()
			closed := provider.closed
			connected := provider.connectCfg != nil
			provider.mu.Unlock()
			if beforeStart && connected {
				t.Fatal("expired pre-start binding admitted provider setup")
			}
			if !beforeStart && !closed {
				t.Fatal("expired session retained provider")
			}
			_, err = registeredAgentHeaders(LiveConfigFrame{CapabilityLease: boundTestToken(leaseExpiry), OboSubjectToken: token, CredentialExpiresAt: issuerExpiry}, "secret")(context.Background(), a2a.RequestContext{})
			if !budget && err == nil {
				t.Fatal("expired binding admitted a later agent turn")
			}
		})
	}
}

func TestOpaqueCredentialWithoutIssuerExpiryCannotStart(t *testing.T) {
	provider := newFakeProvider()
	session := &ManagedSession{ID: "missing-expiry", DelegatedCredential: "opaque-credential"}
	session.VoiceAgentBinding.TargetAgentID = "test-agent"
	session.VoiceAgentBinding.Lease = boundTestToken(time.Now().Add(time.Hour).Unix())
	conn, _ := startReliabilityAdapter(t, provider, session, 0)
	var failure ErrorFrame
	readJSONFrame(t, conn, &failure)
	if !failure.Fatal || failure.Code != "auth_expired" {
		t.Fatalf("unknown credential lifetime admitted: %+v", failure)
	}
	var end SessionEndFrame
	readJSONFrame(t, conn, &end)
	if end.Reason != "authorization_expired" {
		t.Fatalf("missing expiry not terminal: %+v", end)
	}
}

type stalledConnectProvider struct{ *fakeProvider }

func TestProviderAuthorizationFailureDeliversTerminalBeforeClose(t *testing.T) {
	provider := newFakeProvider()
	conn, done := startReliabilityAdapter(t, provider, &ManagedSession{ID: "provider-expired"}, 0)
	sendStart(t, conn, StartFrame{})
	var ready StateFrame
	readJSONFrame(t, conn, &ready)
	provider.push(&LiveMessage{ErrorCode: "auth_expired"})
	var failure ErrorFrame
	readJSONFrame(t, conn, &failure)
	if !failure.Fatal || failure.Code != "auth_expired" {
		t.Fatalf("provider expiry not reported as fatal: %+v", failure)
	}
	var end SessionEndFrame
	readJSONFrame(t, conn, &end)
	if end.Type != MsgSessionEnd || end.Reason != "authorization_expired" {
		t.Fatalf("provider expiry lost terminal delivery: %+v", end)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("downlink continued after authorization failure")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("failed session retained resources")
	}
}

func (p *stalledConnectProvider) Connect(ctx context.Context, _ LiveConfigFrame) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestProviderReadinessTimeoutEndsAndReleasesSession(t *testing.T) {
	provider := &stalledConnectProvider{newFakeProvider()}
	conn, done := startReliabilityAdapter(t, provider, &ManagedSession{ID: "stalled"}, 10*time.Millisecond)
	sendStart(t, conn, StartFrame{})
	var failure ErrorFrame
	readJSONFrame(t, conn, &failure)
	if failure.Code != "provider_connect_failed" || !failure.Fatal {
		t.Fatalf("readiness failure was not terminal: %+v", failure)
	}
	var end SessionEndFrame
	readJSONFrame(t, conn, &end)
	if end.Type != MsgSessionEnd || end.Reason != "error" {
		t.Fatalf("readiness failure did not end session: %+v", end)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stalled session was not released")
	}
}
