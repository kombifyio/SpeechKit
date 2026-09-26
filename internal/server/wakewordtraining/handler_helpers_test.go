//go:build linux

package wakewordtraining

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

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
