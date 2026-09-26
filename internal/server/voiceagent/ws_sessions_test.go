//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

func TestListSessionsReturnsCallerSessions(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	own, _, err := manager.Create(Identity{UserID: "anonymous", OrgID: "public"})
	if err != nil {
		t.Fatalf("create own session: %v", err)
	}
	if _, _, err := manager.Create(Identity{UserID: "other", OrgID: "public"}); err != nil {
		t.Fatalf("create other session: %v", err)
	}

	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	req := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body listSessionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Sessions) != 1 || body.Sessions[0].SessionID != own.ID {
		t.Fatalf("sessions = %+v, want only %s", body.Sessions, own.ID)
	}
	if body.Metrics.TotalSessions != 2 || body.Metrics.IdentitySessions != 1 {
		t.Fatalf("metrics = %+v, want total=2 identity=1", body.Metrics)
	}
	if body.Metrics.MaxGlobalSessions != 100 || body.Metrics.MaxPerIdentitySessions != 3 {
		t.Fatalf("metric limits = %+v, want default 100/3", body.Metrics)
	}
}

func TestDeleteSessionRequiresOwnerOrAdmin(t *testing.T) {
	manager := mustManager(t, Options{})
	handler, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler: %v", err)
	}
	own, _, err := manager.Create(Identity{UserID: "anonymous", OrgID: "public"})
	if err != nil {
		t.Fatalf("create own session: %v", err)
	}
	other, _, err := manager.Create(Identity{UserID: "other", OrgID: "public"})
	if err != nil {
		t.Fatalf("create other session: %v", err)
	}

	mux := http.NewServeMux()
	handler.Mount(mux)
	wrapped := middleware.Auth(middleware.AuthOptions{Mode: "none"})(mux)

	forbiddenReq := httptest.NewRequest(http.MethodDelete, "https://speechkit.test/v1/voiceagent/sessions/"+other.ID, nil)
	forbiddenRec := httptest.NewRecorder()
	wrapped.ServeHTTP(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("delete other got %d body=%s, want 403", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "https://speechkit.test/v1/voiceagent/sessions/"+own.ID, nil)
	deleteRec := httptest.NewRecorder()
	wrapped.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete own got %d body=%s, want 204", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := manager.Get(own.ID); err != ErrSessionNotFound {
		t.Fatalf("deleted session lookup err = %v, want ErrSessionNotFound", err)
	}
	if _, err := manager.Get(other.ID); err != nil {
		t.Fatalf("other session should remain: %v", err)
	}
}

func TestPersistedSessionSubresourcesRequireStoreAndOwnership(t *testing.T) {
	manager := mustManager(t, Options{})
	handlerWithoutStore, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
	})
	if err != nil {
		t.Fatalf("New handler without store: %v", err)
	}
	muxWithoutStore := http.NewServeMux()
	handlerWithoutStore.Mount(muxWithoutStore)
	wrappedWithoutStore := middleware.Auth(middleware.AuthOptions{Mode: "none"})(muxWithoutStore)

	missingStoreReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/1/transcript", nil)
	missingStoreRec := httptest.NewRecorder()
	wrappedWithoutStore.ServeHTTP(missingStoreRec, missingStoreReq)
	if missingStoreRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing store got %d body=%s, want 503", missingStoreRec.Code, missingStoreRec.Body.String())
	}

	sqliteStore, err := store.NewSQLiteStore(store.StoreConfig{
		SQLitePath:        filepath.Join(t.TempDir(), "speechkit.db"),
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	ownerCtx := store.WithRecordOwner(context.Background(), store.RecordOwner{UserID: "anonymous", OrgID: "public", Source: "none"})
	sessionID, err := sqliteStore.SaveVoiceAgentSession(ownerCtx, store.VoiceAgentSession{
		StartedAt:   time.Now().Add(-time.Minute),
		EndedAt:     time.Now(),
		Language:    "en",
		Transcript:  "User: hello\nAgent: hi",
		Turns:       []store.VoiceAgentTurn{{Role: "user", Text: "hello"}},
		Summary:     store.VoiceAgentSessionSummary{Summary: "A short hello."},
		CreatedAt:   time.Now(),
		OwnerUserID: "anonymous",
		OwnerOrgID:  "public",
	})
	if err != nil {
		t.Fatalf("SaveVoiceAgentSession: %v", err)
	}

	handlerWithStore, err := New(HandlerOptions{
		Manager:  manager,
		Provider: staticProviderFactory{provider: newFakeProvider()},
		Persona:  &fakeResolver{},
		Store:    sqliteStore,
	})
	if err != nil {
		t.Fatalf("New handler with store: %v", err)
	}
	muxWithStore := http.NewServeMux()
	handlerWithStore.Mount(muxWithStore)
	wrappedWithStore := middleware.Auth(middleware.AuthOptions{Mode: "none"})(muxWithStore)

	transcriptReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/"+strconv.FormatInt(sessionID, 10)+"/transcript", nil)
	transcriptRec := httptest.NewRecorder()
	wrappedWithStore.ServeHTTP(transcriptRec, transcriptReq)
	if transcriptRec.Code != http.StatusOK {
		t.Fatalf("transcript got %d body=%s, want 200", transcriptRec.Code, transcriptRec.Body.String())
	}
	if !strings.Contains(transcriptRec.Body.String(), "User: hello") {
		t.Fatalf("transcript body = %s", transcriptRec.Body.String())
	}

	summaryReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/"+strconv.FormatInt(sessionID, 10)+"/summary", nil)
	summaryRec := httptest.NewRecorder()
	wrappedWithStore.ServeHTTP(summaryRec, summaryReq)
	if summaryRec.Code != http.StatusOK {
		t.Fatalf("summary got %d body=%s, want 200", summaryRec.Code, summaryRec.Body.String())
	}
	if !strings.Contains(summaryRec.Body.String(), "A short hello.") {
		t.Fatalf("summary body = %s", summaryRec.Body.String())
	}

	badIDReq := httptest.NewRequest(http.MethodGet, "https://speechkit.test/v1/voiceagent/sessions/not-numeric/transcript", nil)
	badIDRec := httptest.NewRecorder()
	wrappedWithStore.ServeHTTP(badIDRec, badIDReq)
	if badIDRec.Code != http.StatusNotFound {
		t.Fatalf("bad id got %d body=%s, want 404", badIDRec.Code, badIDRec.Body.String())
	}
}
