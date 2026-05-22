package wakeword

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── Helpers ─────────────────────────────────────────────────────────────────

func writeFakeCapture(t *testing.T, dir, id, label string, captured time.Time) (jsonPath, wavPath string) {
	t.Helper()
	prefix := captured.UTC().Format("20060102T150405.000Z") + "_" + id
	wavPath = filepath.Join(dir, prefix+".wav")
	jsonPath = filepath.Join(dir, prefix+".json")
	// Minimal valid WAV header is fine — the test server doesn't decode.
	if err := os.WriteFile(wavPath, []byte("RIFF....WAVEfmt ....data....BYTES"), 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	rec := TrainingRecord{
		ID:         id,
		PhraseID:   "hey_quby",
		Phrase:     "Hey Quby",
		Backend:    "livekit_openwakeword",
		Score:      0.88,
		CapturedAt: captured,
		PreRollMs:  1500,
		PostRollMs: 500,
		SampleRate: 16000,
		AudioPath:  filepath.Base(wavPath),
		AudioBytes: 33,
		Label:      label,
		Uploaded:   false,
	}
	b, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(jsonPath, b, 0o600); err != nil {
		t.Fatalf("write json: %v", err)
	}
	return jsonPath, wavPath
}

type collectingHandler struct {
	mu          sync.Mutex
	requests    [][]byte // captured metadata JSON
	respond     func(w http.ResponseWriter, r *http.Request)
	callCounter atomic.Int64
}

func (c *collectingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.callCounter.Add(1)
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	meta := r.FormValue("metadata")
	c.mu.Lock()
	c.requests = append(c.requests, []byte(meta))
	c.mu.Unlock()
	if c.respond != nil {
		c.respond(w, r)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// ── Tests ───────────────────────────────────────────────────────────────────

func TestNewTrainingUploader_RejectsMissingFields(t *testing.T) {
	cases := []TrainingUploaderConfig{
		{ServerURL: "http://x", Interval: time.Second},                    // missing Dir
		{Dir: t.TempDir(), Interval: time.Second},                         // missing ServerURL
		{Dir: t.TempDir(), ServerURL: "http://x"},                         // missing Interval
		{Dir: t.TempDir(), ServerURL: "http://x", Interval: -time.Second}, // negative
	}
	for i, c := range cases {
		if _, err := NewTrainingUploader(c); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestUploader_UploadsUnsentRecord(t *testing.T) {
	dir := t.TempDir()
	jsonPath, _ := writeFakeCapture(t, dir, "act-001", "", time.Now())

	handler := &collectingHandler{}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	up, err := NewTrainingUploader(TrainingUploaderConfig{
		Dir:        dir,
		ServerURL:  srv.URL,
		Interval:   50 * time.Millisecond,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = up.Run(ctx) // returns ctx.Err()

	if handler.callCounter.Load() == 0 {
		t.Fatal("server saw no requests")
	}

	// Sidecar should now be marked uploaded.
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var rec TrainingRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !rec.Uploaded {
		t.Error("expected uploaded=true after success")
	}

	// Metadata captured by server should echo the ID.
	handler.mu.Lock()
	gotMeta := handler.requests
	handler.mu.Unlock()
	if len(gotMeta) == 0 {
		t.Fatal("no metadata captured")
	}
	if !strings.Contains(string(gotMeta[0]), `"id":"act-001"`) {
		t.Errorf("metadata = %s", string(gotMeta[0]))
	}
}

func TestUploader_SkipsAlreadyUploaded(t *testing.T) {
	dir := t.TempDir()
	jsonPath, _ := writeFakeCapture(t, dir, "act-001", "", time.Now())

	// Pre-mark as uploaded.
	rec, _ := readRecord(jsonPath)
	rec.Uploaded = true
	_ = writeRecord(jsonPath, rec)

	handler := &collectingHandler{}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	up, _ := NewTrainingUploader(TrainingUploaderConfig{
		Dir:        dir,
		ServerURL:  srv.URL,
		Interval:   50 * time.Millisecond,
		HTTPClient: srv.Client(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = up.Run(ctx)

	if handler.callCounter.Load() != 0 {
		t.Errorf("expected 0 server calls (already uploaded), got %d", handler.callCounter.Load())
	}
}

func TestUploader_OnlyLabeledSkipsUnlabelled(t *testing.T) {
	dir := t.TempDir()
	writeFakeCapture(t, dir, "unlabeled", "", time.Now())
	jsonPath, _ := writeFakeCapture(t, dir, "labeled", "correct", time.Now().Add(time.Second))

	handler := &collectingHandler{}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	up, _ := NewTrainingUploader(TrainingUploaderConfig{
		Dir:         dir,
		ServerURL:   srv.URL,
		Interval:    50 * time.Millisecond,
		OnlyLabeled: true,
		HTTPClient:  srv.Client(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = up.Run(ctx)

	if got := handler.callCounter.Load(); got != 1 {
		t.Errorf("expected 1 upload (only labeled), got %d", got)
	}
	// Labeled record should be marked uploaded; unlabeled stays false.
	b, _ := os.ReadFile(jsonPath)
	var rec TrainingRecord
	_ = json.Unmarshal(b, &rec)
	if !rec.Uploaded {
		t.Error("labeled record was not marked uploaded")
	}
}

func TestUploader_HandlesConflict409AsSuccess(t *testing.T) {
	dir := t.TempDir()
	jsonPath, _ := writeFakeCapture(t, dir, "act-dup", "", time.Now())

	handler := &collectingHandler{
		respond: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "duplicate", http.StatusConflict)
		},
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	up, _ := NewTrainingUploader(TrainingUploaderConfig{
		Dir:        dir,
		ServerURL:  srv.URL,
		Interval:   50 * time.Millisecond,
		HTTPClient: srv.Client(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = up.Run(ctx)

	rec, _ := readRecord(jsonPath)
	if !rec.Uploaded {
		t.Error("expected 409 to mark uploaded=true (idempotent)")
	}
}

func TestUploader_HandlesServerDisabled503(t *testing.T) {
	dir := t.TempDir()
	jsonPath, _ := writeFakeCapture(t, dir, "act-disabled", "", time.Now())

	handler := &collectingHandler{
		respond: func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "training_data_disabled", http.StatusServiceUnavailable)
		},
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	up, _ := NewTrainingUploader(TrainingUploaderConfig{
		Dir:        dir,
		ServerURL:  srv.URL,
		Interval:   50 * time.Millisecond,
		HTTPClient: srv.Client(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = up.Run(ctx)

	rec, _ := readRecord(jsonPath)
	if rec.Uploaded {
		t.Error("503 must NOT mark uploaded=true")
	}
}

func TestUploader_BearerTokenIsForwarded(t *testing.T) {
	dir := t.TempDir()
	writeFakeCapture(t, dir, "act-auth", "", time.Now())

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	up, _ := NewTrainingUploader(TrainingUploaderConfig{
		Dir:         dir,
		ServerURL:   srv.URL,
		BearerToken: "tok-xyz",
		Interval:    50 * time.Millisecond,
		HTTPClient:  srv.Client(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = up.Run(ctx)

	if gotAuth != "Bearer tok-xyz" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok-xyz")
	}
}

func TestUploader_TolerantOfMalformedSidecar(t *testing.T) {
	dir := t.TempDir()
	// One good one and one corrupt one.
	writeFakeCapture(t, dir, "ok", "", time.Now())
	if err := os.WriteFile(filepath.Join(dir, "bogus.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write bogus: %v", err)
	}

	handler := &collectingHandler{}
	srv := httptest.NewServer(handler)
	defer srv.Close()

	up, _ := NewTrainingUploader(TrainingUploaderConfig{
		Dir:        dir,
		ServerURL:  srv.URL,
		Interval:   50 * time.Millisecond,
		HTTPClient: srv.Client(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_ = up.Run(ctx)

	if got := handler.callCounter.Load(); got != 1 {
		t.Errorf("expected 1 upload (bogus.json skipped), got %d", got)
	}
}

func TestUploader_StopsOnCtxCancel(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(&collectingHandler{})
	defer srv.Close()
	up, _ := NewTrainingUploader(TrainingUploaderConfig{
		Dir:        dir,
		ServerURL:  srv.URL,
		Interval:   50 * time.Millisecond,
		HTTPClient: srv.Client(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- up.Run(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Run() = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
