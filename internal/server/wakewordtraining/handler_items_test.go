//go:build linux

package wakewordtraining

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/store"
)

func TestList_ScopedToOwner(t *testing.T) {
	st := newFakeStore()
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "a1", OwnerUserID: testUser, OwnerOrgID: testOrg, AudioPath: "p1", AudioBytes: 10,
	})
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "b1", OwnerUserID: "bob", OwnerOrgID: testOrg, AudioPath: "p2", AudioBytes: 10,
	})
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: st})

	req := httptest.NewRequest(http.MethodGet, "/v1/wakeword/activations", http.NoBody)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["count"].(float64) != 1 {
		t.Errorf("count = %v, want 1 (bob's row must not leak)", got["count"])
	}
}

func TestGet_OneActivationScoped(t *testing.T) {
	st := newFakeStore()
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "a1", OwnerUserID: testUser, OwnerOrgID: testOrg, AudioPath: "p1", AudioBytes: 10,
	})
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: st})

	req := httptest.NewRequest(http.MethodGet, "/v1/wakeword/activations/a1", http.NoBody)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.item(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Cross-owner returns 404.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/wakeword/activations/a1", http.NoBody)
	req2 = withIdentity(req2, "bob", testOrg)
	rec2 := httptest.NewRecorder()
	h.item(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("cross-owner status = %d, want 404", rec2.Code)
	}
}

func TestGetAudio_StreamsBytes(t *testing.T) {
	st := newFakeStore()
	h, audioDir := testHandler(t, Options{AcceptUploads: true, Store: st})

	// Write a WAV bytestream and register the row.
	relPath := filepath.Join(testOrg, testUser, "a1.wav")
	abs := filepath.Join(audioDir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte("RIFFWAVE..."), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "a1", OwnerUserID: testUser, OwnerOrgID: testOrg, AudioPath: relPath, AudioBytes: 11,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/wakeword/activations/a1/audio", http.NoBody)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.item(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got, _ := io.ReadAll(rec.Body)
	if string(got) != "RIFFWAVE..." {
		t.Errorf("body = %q", string(got))
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestPatchLabel_HappyPath(t *testing.T) {
	st := newFakeStore()
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "a1", OwnerUserID: testUser, OwnerOrgID: testOrg, AudioPath: "p", AudioBytes: 1,
	})
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: st})

	body := strings.NewReader(`{"label":"correct"}`)
	req := httptest.NewRequest(http.MethodPatch, "/v1/wakeword/activations/a1", body)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.item(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	row, _ := st.GetWakewordActivation(context.Background(), "a1", testUser, testOrg)
	if row.Label != "correct" {
		t.Errorf("label = %q, want correct", row.Label)
	}
}

func TestPatchLabel_RejectsBogus(t *testing.T) {
	st := newFakeStore()
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "a1", OwnerUserID: testUser, OwnerOrgID: testOrg, AudioPath: "p", AudioBytes: 1,
	})
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: st})

	body := strings.NewReader(`{"label":"GARBAGE"}`)
	req := httptest.NewRequest(http.MethodPatch, "/v1/wakeword/activations/a1", body)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.item(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDelete_RemovesRowAndFile(t *testing.T) {
	st := newFakeStore()
	h, audioDir := testHandler(t, Options{AcceptUploads: true, Store: st})

	relPath := filepath.Join(testOrg, testUser, "a1.wav")
	abs := filepath.Join(audioDir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, []byte("bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "a1", OwnerUserID: testUser, OwnerOrgID: testOrg, AudioPath: relPath, AudioBytes: 5,
	})

	req := httptest.NewRequest(http.MethodDelete, "/v1/wakeword/activations/a1", http.NoBody)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.item(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Errorf("expected file removed, stat err = %v", err)
	}
}
