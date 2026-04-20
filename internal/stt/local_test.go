package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

func TestLocal_Transcribe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": "Local result"})
	}))
	defer server.Close()

	p := &LocalProvider{
		BaseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	p.ready.Store(true)

	result, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result.Text != "Local result" {
		t.Errorf("text = %q", result.Text)
	}
	if result.Provider != "local" {
		t.Errorf("provider = %q", result.Provider)
	}
}

func TestLocal_Transcribe_NotReady(t *testing.T) {
	p := NewLocalProvider(8080, "/fake/model.bin", "cpu")
	_, err := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
	if err == nil {
		t.Fatal("expected error when not ready")
	}
}

func TestLocal_Health_NotRunning(t *testing.T) {
	p := NewLocalProvider(8080, "/fake/model.bin", "cpu")
	err := p.Health(context.Background())
	if err == nil {
		t.Error("expected error when not running")
	}
}

func TestLocal_Health_OK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()

	p := &LocalProvider{
		BaseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	p.ready.Store(true)

	if err := p.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}

func TestLocal_IsReady(t *testing.T) {
	p := NewLocalProvider(8080, "/model.bin", "cpu")
	if p.IsReady() {
		t.Error("should not be ready before StartServer")
	}
}

func TestLocal_Name(t *testing.T) {
	p := NewLocalProvider(8080, "/model.bin", "cpu")
	if p.Name() != "local" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestLocal_TranscribeTimeoutScalesWithLongWAV(t *testing.T) {
	wav := audio.PCMToWAV(make([]byte, audio.SampleRate*audio.BytesPerSample*90))

	timeout := localTranscribeTimeout(wav)

	if timeout <= 30*time.Second {
		t.Fatalf("timeout = %v, want more than the legacy 30s cap", timeout)
	}
	if timeout < 4*time.Minute {
		t.Fatalf("timeout = %v, want enough headroom for 90s local whisper inference", timeout)
	}
}

func TestFindWhisperBinary_FindsManagedInstallRootBinary(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LOCALAPPDATA", localAppData)

	expected := filepath.Join(localAppData, "SpeechKit", "whisper-server.exe")
	if err := os.MkdirAll(filepath.Dir(expected), 0o755); err != nil {
		t.Fatalf("mkdir managed install dir: %v", err)
	}
	if err := os.WriteFile(expected, []byte("binary"), 0o644); err != nil {
		t.Fatalf("write managed binary: %v", err)
	}

	got, err := findWhisperBinary()
	if err != nil {
		t.Fatalf("findWhisperBinary: %v", err)
	}
	if got != expected {
		t.Fatalf("binary path = %q, want %q", got, expected)
	}
}

// TestLocal_Transcribe_WaitsForStartupThenSucceeds verifies that Transcribe
// blocks while startup is in progress and succeeds once the server becomes ready.
// This is a regression test for the bug where Transcribe returned "not ready"
// immediately instead of waiting, causing hotkey-triggered recordings to fail
// during the first ~60 seconds after app launch.
func TestLocal_Transcribe_WaitsForStartupThenSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"text": "waited"})
	}))
	defer server.Close()

	p := &LocalProvider{
		BaseURL: server.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	// Simulate startup in progress: startDone is open (not yet closed).
	done := make(chan struct{})
	p.startDone = done

	resultCh := make(chan *Result, 1)
	errCh := make(chan error, 1)
	go func() {
		r, e := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
		resultCh <- r
		errCh <- e
	}()

	// Allow the goroutine to reach the select block.
	time.Sleep(50 * time.Millisecond)

	// Simulate successful startup completing.
	p.ready.Store(true)
	close(done)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("expected success after startup completed, got: %v", err)
		}
		result := <-resultCh
		if result.Text != "waited" {
			t.Errorf("text = %q, want %q", result.Text, "waited")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Transcribe did not unblock after startup completed")
	}
}

// TestLocal_Transcribe_WaitsForStartupThenFails verifies that Transcribe
// returns a "not ready" error when it waits for startup but the server
// never becomes ready (startup failed).
func TestLocal_Transcribe_WaitsForStartupThenFails(t *testing.T) {
	p := &LocalProvider{
		BaseURL: "http://127.0.0.1:1", // unreachable
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	done := make(chan struct{})
	p.startDone = done

	errCh := make(chan error, 1)
	go func() {
		_, e := p.Transcribe(context.Background(), []byte("wav"), TranscribeOpts{})
		errCh <- e
	}()

	time.Sleep(50 * time.Millisecond)

	// Startup failed: close the channel without setting ready.
	close(done)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error when startup failed but got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Transcribe did not unblock after failed startup")
	}
}

// TestLocal_Transcribe_ContextCancelledDuringStartupWait verifies that
// Transcribe respects context cancellation while waiting for startup.
func TestLocal_Transcribe_ContextCancelledDuringStartupWait(t *testing.T) {
	p := &LocalProvider{
		BaseURL: "http://127.0.0.1:1",
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	done := make(chan struct{}) // never closed â€” startup hangs indefinitely
	p.startDone = done

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, e := p.Transcribe(ctx, []byte("wav"), TranscribeOpts{})
		errCh <- e
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error when context cancelled during startup wait")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Transcribe did not unblock after context cancellation")
	}
}

func TestLocal_WaitForInferenceReady_RetriesUntilInferenceSucceeds(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %q", r.Method)
		}

		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if got := r.FormValue("model"); got != "whisper-1" {
			t.Fatalf("model = %q, want whisper-1", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile(file): %v", err)
		}
		_ = file.Close()

		if attempts.Add(1) < 3 {
			http.Error(w, "warming", http.StatusServiceUnavailable)
			return
		}

		if err := json.NewEncoder(w).Encode(map[string]string{"text": "warm"}); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}))
	defer server.Close()

	p := &LocalProvider{
		BaseURL: server.URL,
		client:  &http.Client{Timeout: 2 * time.Second},
	}

	if err := p.waitForInferenceReadyWithRetry(context.Background(), 5, time.Millisecond); err != nil {
		t.Fatalf("waitForInferenceReadyWithRetry: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}
