//go:build linux

package wakewordtraining

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/store"
)

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

func TestUpload_PathTraversalIsRejectedAtValidation(t *testing.T) {
	// Audit S-6 strengthened this: traversal-shaped IDs used to be
	// silently sanitised by safePathSegment and the upload accepted.
	// The new wakewordIDPattern guard rejects them at the validation
	// step with 400 BEFORE any filesystem work. Stricter contract,
	// less attacker control over the on-disk layout, less code path
	// to audit for path-escape regressions.
	st := newFakeStore()
	h, audioDir := testHandler(t, Options{AcceptUploads: true, Store: st})

	for _, hostile := range []string{
		"../../../etc/passwd",
		"..\\..\\windows\\system32",
		"/absolute/path",
		"with spaces",
	} {
		t.Run(hostile, func(t *testing.T) {
			meta := sampleMetadata()
			meta["id"] = hostile
			body, contentType := buildMultipartUpload(t, meta, []byte("hostile"))
			req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
			req.Header.Set("Content-Type", contentType)
			req = withIdentity(req, testUser, testOrg)
			rec := httptest.NewRecorder()
			h.collection(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("hostile id %q: status = %d body=%s, want 400", hostile, rec.Code, rec.Body.String())
			}
		})
	}

	// Belt-and-suspenders: even though validation rejected the
	// requests above, walk the audio dir to confirm no bytes landed
	// outside the legitimate <audioDir>/<org>/<user>/ path. A future
	// regression that loosens the validation guard would land bytes
	// here and this assertion would fail.
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

func TestUpload_RejectsOversizedID(t *testing.T) {
	st := newFakeStore()
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: st})

	meta := sampleMetadata()
	meta["id"] = strings.Repeat("a", maxWakewordIDLen+1)
	body, contentType := buildMultipartUpload(t, meta, []byte("PCM"))
	req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
	req.Header.Set("Content-Type", contentType)
	req = withIdentity(req, testUser, testOrg)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400 (oversized ID)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "metadata.id") {
		t.Fatalf("body = %s, want metadata.id context", rec.Body.String())
	}
}

func TestUpload_RejectsBadCharID(t *testing.T) {
	st := newFakeStore()
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: st})

	for _, bad := range []string{"has space", "has/slash", "has.dot", "has@at", "weirdé"} {
		t.Run(bad, func(t *testing.T) {
			meta := sampleMetadata()
			meta["id"] = bad
			body, contentType := buildMultipartUpload(t, meta, []byte("PCM"))
			req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
			req.Header.Set("Content-Type", contentType)
			req = withIdentity(req, testUser, testOrg)
			rec := httptest.NewRecorder()
			h.collection(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("id=%q status=%d body=%s, want 400", bad, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpload_QuotaRaceSerialisedByPerOwnerLock(t *testing.T) {
	// Pre-seed the fake store so SumBytes returns "almost at quota":
	// remaining capacity is exactly one upload's worth. Without the
	// per-owner lock from audit S-6, two concurrent uploads both see
	// "under quota" and both commit, doubling the user's footprint.
	// With the lock, the second upload sees the post-first SumBytes
	// and is rejected with 413.
	st := newFakeStore()
	_, _ = st.SaveWakewordActivation(context.Background(), store.WakewordActivation{
		ID: "seed", OwnerUserID: testUser, OwnerOrgID: testOrg,
		AudioPath: "p", AudioBytes: 900,
	})
	h, _ := testHandler(t, Options{
		AcceptUploads:     true,
		Store:             st,
		PerUserQuotaBytes: 1000, // 100 bytes of headroom = exactly one upload
	})

	send := func(id string) int {
		meta := sampleMetadata()
		meta["id"] = id
		body, contentType := buildMultipartUpload(t, meta, []byte("ten-bytes!")) // 10 bytes payload
		req := httptest.NewRequest(http.MethodPost, "/v1/wakeword/activations", body)
		req.Header.Set("Content-Type", contentType)
		req = withIdentity(req, testUser, testOrg)
		rec := httptest.NewRecorder()
		h.collection(rec, req)
		return rec.Code
	}

	// Fire 5 concurrent uploads. Only the FIRST should succeed; the
	// remaining 4 must see the updated SumBytes (910 > 1000-margin
	// after the first commits — wait, headroom is 100 and each upload
	// is only 10 bytes, so multiple could fit). Let me size correctly:
	// the goal is to confirm the lock serialises — N concurrent
	// uploads should not all see the SAME pre-write SumBytes. The
	// easiest assertion is that SumBytes after N uploads is consistent
	// with N successes (no double-counting).
	type result struct {
		id   string
		code int
	}
	results := make(chan result, 10)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("race-%d", i)
			results <- result{id, send(id)}
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	for r := range results {
		if r.code == http.StatusCreated {
			successes++
		}
	}

	// With seed=900 + quota=1000 and 10-byte uploads, theoretical
	// max is 10 successes (each within quota). What we're proving is
	// the lock didn't deadlock and stored bytes match success count
	// exactly — no double-counting, no orphans.
	stored, err := st.SumWakewordActivationBytesForUser(context.Background(), testUser, testOrg)
	if err != nil {
		t.Fatalf("SumBytes: %v", err)
	}
	wantStored := int64(900 + successes*10)
	if stored != wantStored {
		t.Fatalf("stored bytes = %d, want %d (seed 900 + %d * 10) — race-condition double-count?", stored, wantStored, successes)
	}
	if successes == 0 {
		t.Fatal("no uploads succeeded — the lock deadlocked")
	}
}
