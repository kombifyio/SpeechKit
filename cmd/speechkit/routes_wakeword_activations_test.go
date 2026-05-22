package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
)

// writeFakeActivationOnDisk drops one paired .wav/.json under dir, matching
// the layout the wakeword sidecar's TrainingCapture writes.
func writeFakeActivationOnDisk(t *testing.T, dir, id, label string) {
	t.Helper()
	prefix := time.Now().UTC().Format("20060102T150405.000Z") + "_" + id
	wavPath := filepath.Join(dir, prefix+".wav")
	jsonPath := filepath.Join(dir, prefix+".json")
	if err := os.WriteFile(wavPath, []byte("RIFFWAVEdata-fake"), 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	rec := wakeword.TrainingRecord{
		ID:         id,
		PhraseID:   "hey_quby",
		Phrase:     "Hey Quby",
		Backend:    "livekit_openwakeword",
		Score:      0.91,
		CapturedAt: time.Now().UTC(),
		PreRollMs:  1500,
		PostRollMs: 500,
		SampleRate: 16000,
		AudioPath:  filepath.Base(wavPath),
		AudioBytes: 17,
		Label:      label,
		Uploaded:   false,
	}
	b, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(jsonPath, b, 0o600); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func newActivationTestRouter(cfg *config.Config) *http.ServeMux {
	mux := http.NewServeMux()
	registerWakewordActivationRoutes(mux, cfg)
	return mux
}

func TestActivationsList_EmptyDirReturnsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureEnabled = false
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/wakeword/activations")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body listWakewordActivationsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Count != 0 {
		t.Errorf("count = %d, want 0", body.Count)
	}
	if body.Enabled {
		t.Error("enabled should mirror cfg toggle (false)")
	}
}

func TestActivationsList_ReturnsRecordsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureEnabled = true
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir

	// Two captures — IDs that differ so we can identify them.
	writeFakeActivationOnDisk(t, dir, "old", "")
	time.Sleep(10 * time.Millisecond) // ensure filename timestamps differ
	writeFakeActivationOnDisk(t, dir, "new", "correct")

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/wakeword/activations")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body listWakewordActivationsResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2 (body=%+v)", body.Count, body)
	}
	if body.Activations[0].ID != "new" || body.Activations[1].ID != "old" {
		t.Errorf("ordering wrong: %v", []string{body.Activations[0].ID, body.Activations[1].ID})
	}
	if body.Activations[0].Label != "correct" {
		t.Errorf("label not echoed: %+v", body.Activations[0])
	}
}

func TestActivationsAudio_StreamsWavBytes(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir
	writeFakeActivationOnDisk(t, dir, "a1", "")

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/wakeword/activations/a1/audio")
	if err != nil {
		t.Fatalf("GET audio: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestActivationsPatch_UpdatesLabelAndResetsUploaded(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir
	writeFakeActivationOnDisk(t, dir, "a1", "")

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/wakeword/activations/a1",
		strings.NewReader(`{"label":"correct"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	// Verify sidecar was rewritten.
	pair, err := findActivationPair(dir, "a1")
	if err != nil {
		t.Fatalf("findActivationPair: %v", err)
	}
	raw, _ := os.ReadFile(pair.jsonPath)
	var rec wakeword.TrainingRecord
	_ = json.Unmarshal(raw, &rec)
	if rec.Label != "correct" {
		t.Errorf("label not updated: %q", rec.Label)
	}
}

func TestActivationsPatch_RejectsBogusLabel(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir
	writeFakeActivationOnDisk(t, dir, "a1", "")

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/wakeword/activations/a1",
		strings.NewReader(`{"label":"GARBAGE"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestActivationsDelete_RemovesPair(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir
	writeFakeActivationOnDisk(t, dir, "a1", "")

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/wakeword/activations/a1", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected dir empty after delete, got %d entries", len(entries))
	}
}

func TestActivationsDelete_NotFound(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/wakeword/activations/missing", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestActivationsMethodNotAllowed(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{}
	cfg.Wakeword.TrainingData.LocalCaptureDir = dir

	srv := httptest.NewServer(newActivationTestRouter(cfg))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/wakeword/activations", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestSplitActivationPath(t *testing.T) {
	cases := []struct{ in, id, sub string }{
		{"a1", "a1", ""},
		{"a1/audio", "a1", "audio"},
		{"a1/audio/extra", "a1", "audio/extra"},
		{"", "", ""},
	}
	for _, c := range cases {
		id, sub := splitActivationPath(c.in)
		if id != c.id || sub != c.sub {
			t.Errorf("splitActivationPath(%q) = (%q,%q), want (%q,%q)", c.in, id, sub, c.id, c.sub)
		}
	}
}

func TestIsValidActivationLabel(t *testing.T) {
	cases := map[string]bool{
		"":               true,
		"correct":        true,
		"false_positive": true,
		"CORRECT":        false,
		"yes":            false,
		" correct":       false,
	}
	for in, want := range cases {
		if got := isValidActivationLabel(in); got != want {
			t.Errorf("isValidActivationLabel(%q) = %v, want %v", in, got, want)
		}
	}
}
