//go:build linux

package wakewordtraining

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// ── Test fixtures ───────────────────────────────────────────────────────────

const (
	testUser = "alice"
	testOrg  = "kombify"
)

func testHandler(t *testing.T, opts Options) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	if opts.AudioDir == "" {
		opts.AudioDir = dir
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC) }
	}
	h, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, opts.AudioDir
}

// fakeStore is an in-memory WakewordActivationStore for handler tests.
type fakeStore struct {
	mu        sync.Mutex
	rows      map[string]store.WakewordActivation // key = owner|id
	failSave  bool
	saveErr   error
	getErr    error
	listErr   error
	updateErr error
	deleteErr error
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[string]store.WakewordActivation{}} }

func (f *fakeStore) key(id, user, org string) string { return user + "|" + org + "|" + id }

func (f *fakeStore) SaveWakewordActivation(_ context.Context, a store.WakewordActivation) (*store.WakewordActivation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSave {
		return nil, f.saveErr
	}
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.OwnerUserID) == "" ||
		strings.TrimSpace(a.OwnerOrgID) == "" || strings.TrimSpace(a.AudioPath) == "" {
		return nil, store.ErrInvalidWakewordActivation
	}
	a.UploadedAt = time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	f.rows[f.key(a.ID, a.OwnerUserID, a.OwnerOrgID)] = a
	out := a
	return &out, nil
}

func (f *fakeStore) GetWakewordActivation(_ context.Context, id, user, org string) (*store.WakewordActivation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	row, ok := f.rows[f.key(id, user, org)]
	if !ok {
		return nil, sql.ErrNoRows
	}
	out := row
	return &out, nil
}

func (f *fakeStore) ListWakewordActivations(_ context.Context, user, org string, opts store.ListOpts) ([]store.WakewordActivation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	prefix := user + "|" + org + "|"
	out := []store.WakewordActivation{}
	for k, v := range f.rows {
		if strings.HasPrefix(k, prefix) {
			out = append(out, v)
		}
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (f *fakeStore) UpdateWakewordActivationLabel(_ context.Context, id, user, org, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	row, ok := f.rows[f.key(id, user, org)]
	if !ok {
		return sql.ErrNoRows
	}
	row.Label = label
	f.rows[f.key(id, user, org)] = row
	return nil
}

func (f *fakeStore) DeleteWakewordActivation(_ context.Context, id, user, org string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return "", f.deleteErr
	}
	row, ok := f.rows[f.key(id, user, org)]
	if !ok {
		return "", sql.ErrNoRows
	}
	delete(f.rows, f.key(id, user, org))
	return row.AudioPath, nil
}

func (f *fakeStore) CountWakewordActivationsForUser(_ context.Context, user, org string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := user + "|" + org + "|"
	var n int64
	for k := range f.rows {
		if strings.HasPrefix(k, prefix) {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) SumWakewordActivationBytesForUser(_ context.Context, user, org string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := user + "|" + org + "|"
	var n int64
	for k, v := range f.rows {
		if strings.HasPrefix(k, prefix) {
			n += v.AudioBytes
		}
	}
	return n, nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func withIdentity(req *http.Request, user, org string) *http.Request {
	ctx := middleware.InjectIdentityForTest(req.Context(), middleware.Identity{
		UserID: user,
		OrgID:  org,
		Plan:   "test",
		Source: "test",
	})
	return req.WithContext(ctx)
}

func buildMultipartUpload(t *testing.T, meta map[string]any, audio []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if meta != nil {
		metaJSON, _ := json.Marshal(meta)
		_ = mw.WriteField("metadata", string(metaJSON))
	}
	if audio != nil {
		part, err := mw.CreateFormFile("audio", "clip.wav")
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		_, _ = part.Write(audio)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return body, mw.FormDataContentType()
}

func sampleMetadata() map[string]any {
	return map[string]any{
		"id":           "act-001",
		"phrase_id":    "hey_quby",
		"phrase":       "Hey Quby",
		"backend":      "livekit_openwakeword",
		"score":        0.91,
		"captured_at":  "2026-05-22T11:59:50Z",
		"pre_roll_ms":  1500,
		"post_roll_ms": 500,
		"sample_rate":  16000,
	}
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestNew_RequiresStoreWhenEnabled(t *testing.T) {
	_, err := New(Options{AcceptUploads: true, AudioDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error when AcceptUploads=true with nil Store")
	}
	if !strings.Contains(err.Error(), "Store") {
		t.Errorf("error %q does not mention Store", err)
	}
}

func TestNew_RequiresAudioDirWhenEnabled(t *testing.T) {
	_, err := New(Options{AcceptUploads: true, Store: newFakeStore()})
	if err == nil {
		t.Fatal("expected error when AcceptUploads=true with empty AudioDir")
	}
}

func TestNew_DisabledNeedsNoStoreOrDir(t *testing.T) {
	h, err := New(Options{AcceptUploads: false})
	if err != nil {
		t.Fatalf("expected no error when AcceptUploads=false: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestDisabled_Returns503ForAllRoutes(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: false})
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/wakeword/activations"},
		{http.MethodPost, "/v1/wakeword/activations"},
		{http.MethodGet, "/v1/wakeword/activations/foo"},
		{http.MethodGet, "/v1/wakeword/activations/foo/audio"},
		{http.MethodPatch, "/v1/wakeword/activations/foo"},
		{http.MethodDelete, "/v1/wakeword/activations/foo"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, http.NoBody)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestUpload_Success(t *testing.T) {
	st := newFakeStore()
	h, audioDir := testHandler(t, Options{AcceptUploads: true, Store: st})

	body, contentType := buildMultipartUpload(t, sampleMetadata(), []byte("PCMBYTES"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got store.WakewordActivation
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != "act-001" || got.PhraseID != "hey_quby" {
		t.Errorf("returned row mismatch: %+v", got)
	}
	if got.AudioBytes != int64(len("PCMBYTES")) {
		t.Errorf("AudioBytes = %d, want %d", got.AudioBytes, len("PCMBYTES"))
	}
	if got.OwnerUserID != testUser || got.OwnerOrgID != testOrg {
		t.Errorf("owner scoping lost: %+v", got)
	}

	// File on disk must exist under <audioDir>/<org>/<user>/<id>.wav.
	wantPath := filepath.Join(audioDir, testOrg, testUser, "act-001.wav")
	bytes, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", wantPath, err)
	}
	if string(bytes) != "PCMBYTES" {
		t.Errorf("on-disk audio mismatch: %q", string(bytes))
	}
}

func TestUpload_RejectsMissingIdentity(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})
	body, contentType := buildMultipartUpload(t, sampleMetadata(), []byte("data"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestUpload_RejectsMissingMetadata(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})
	body, contentType := buildMultipartUpload(t, nil, []byte("data"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing_metadata") {
		t.Errorf("body = %s, want missing_metadata", rec.Body.String())
	}
}

func TestUpload_RejectsInvalidMetadataJSON(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("metadata", "{not json")
	part, _ := mw.CreateFormFile("audio", "x.wav")
	_, _ = part.Write([]byte("d"))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_metadata") {
		t.Errorf("body = %s, want invalid_metadata", rec.Body.String())
	}
}

func TestUpload_RejectsMissingRequiredFields(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})
	meta := sampleMetadata()
	delete(meta, "id")
	body, contentType := buildMultipartUpload(t, meta, []byte("d"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpload_RejectsInvalidLabel(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})
	meta := sampleMetadata()
	meta["label"] = "GARBAGE"
	body, contentType := buildMultipartUpload(t, meta, []byte("d"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_label") {
		t.Errorf("body = %s, want invalid_label", rec.Body.String())
	}
}

func TestUpload_RejectsMissingAudioPart(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})
	body, contentType := buildMultipartUpload(t, sampleMetadata(), nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "missing_audio") {
		t.Errorf("body = %s, want missing_audio", rec.Body.String())
	}
}

func TestUpload_DuplicateIDReturns409(t *testing.T) {
	st := newFakeStore()
	h, audioDir := testHandler(t, Options{AcceptUploads: true, Store: st})

	// Pre-seed the file so the O_EXCL open fails on the second upload.
	dir := filepath.Join(audioDir, testOrg, testUser)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "act-001.wav"), []byte("old"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	body, contentType := buildMultipartUpload(t, sampleMetadata(), []byte("new"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
	}
}

func TestUpload_PathTraversalIsNeutralised(t *testing.T) {
	st := newFakeStore()
	h, audioDir := testHandler(t, Options{AcceptUploads: true, Store: st})

	meta := sampleMetadata()
	meta["id"] = "../../../etc/passwd"
	body, contentType := buildMultipartUpload(t, meta, []byte("hostile"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	// Safety property: every file written by the handler MUST be a
	// descendant of audioDir. A successful traversal would land bytes
	// outside that root.
	expectedRoot, _ := filepath.Abs(audioDir)
	_ = filepath.Walk(expectedRoot, func(p string, info os.FileInfo, _ error) error {
		if info == nil || info.IsDir() {
			return nil
		}
		absPath, _ := filepath.Abs(p)
		if !strings.HasPrefix(absPath, expectedRoot+string(filepath.Separator)) {
			t.Fatalf("path traversal: file at %q escaped %q", absPath, expectedRoot)
		}
		return nil
	})
	// Also assert the scrubbed filename contains neither "/" nor ".."
	// — the sanitiser must strip them completely.
	var got store.WakewordActivation
	_ = json.NewDecoder(rec.Body).Decode(&got)
	base := filepath.Base(got.AudioPath)
	if strings.Contains(base, "/") || strings.Contains(base, "\\") || strings.Contains(base, "..") {
		t.Errorf("scrubbed filename leaked separator/parent: %q", base)
	}
}

func TestUpload_QuotaExceeded(t *testing.T) {
	st := newFakeStore()
	// Pre-seed an existing big row so SumBytes returns over quota.
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "old", OwnerUserID: testUser, OwnerOrgID: testOrg,
		AudioPath: "x", AudioBytes: 1_000_000,
	})
	h, _ := testHandler(t, Options{
		AcceptUploads:     true,
		Store:             st,
		PerUserQuotaBytes: 500_000,
	})

	body, contentType := buildMultipartUpload(t, sampleMetadata(), []byte("d"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d body=%s, want 413", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "quota_exceeded") {
		t.Errorf("body = %s, want quota_exceeded", rec.Body.String())
	}
}

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

func TestUnsupportedMethod_Returns405(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})

	req := httptest.NewRequest(http.MethodPut, "/v1/wakeword/activations", http.NoBody)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") == "" {
		t.Error("expected Allow header")
	}
}

func TestSafePathSegment(t *testing.T) {
	cases := map[string]string{
		"abc":              "abc",
		"hey_quby-01":      "hey_quby-01",
		"hey_quby-01.wav":  "hey_quby-01wav",
		"../../etc/passwd": "etcpasswd",
		"..":               "_",
		".":                "_",
		"":                 "_",
		"  ":               "_",
		"a/b/c":            "abc",
		"a\\b":             "ab",
	}
	for in, want := range cases {
		got := safePathSegment(in)
		if got != want {
			t.Errorf("safePathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitItemPath(t *testing.T) {
	cases := []struct{ in, id, sub string }{
		{"a1", "a1", ""},
		{"a1/audio", "a1", "audio"},
		{"a1/audio/extra", "a1", "audio/extra"},
		{"", "", ""},
	}
	for _, c := range cases {
		id, sub := splitItemPath(c.in)
		if id != c.id || sub != c.sub {
			t.Errorf("splitItemPath(%q) = (%q,%q), want (%q,%q)", c.in, id, sub, c.id, c.sub)
		}
	}
}
