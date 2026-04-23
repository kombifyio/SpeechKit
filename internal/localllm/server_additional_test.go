package localllm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServerDefaultsAndURL(t *testing.T) {
	t.Parallel()
	s := NewServer(0, "/tmp/m.gguf", "auto")
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
	if s.Port != defaultPort {
		t.Errorf("Port = %d, want %d", s.Port, defaultPort)
	}
	wantURL := "http://127.0.0.1:8082/v1"
	if s.BaseURL != wantURL {
		t.Errorf("BaseURL = %q, want %q", s.BaseURL, wantURL)
	}
	if s.ContextSize != defaultContextSize {
		t.Errorf("ContextSize = %d, want %d", s.ContextSize, defaultContextSize)
	}
	if s.Threads != defaultThreads {
		t.Errorf("Threads = %d, want %d", s.Threads, defaultThreads)
	}
	if !s.Validation.AllowLoopback || !s.Validation.AllowHTTP {
		t.Errorf("Validation must allow loopback+HTTP for local llama server, got %+v", s.Validation)
	}
}

func TestNewServerRespectsCustomPort(t *testing.T) {
	t.Parallel()
	s := NewServer(9999, "/tmp/m.gguf", "cpu")
	if s.Port != 9999 {
		t.Errorf("Port = %d, want 9999", s.Port)
	}
	if !strings.HasSuffix(s.BaseURL, ":9999/v1") {
		t.Errorf("BaseURL = %q, want suffix :9999/v1", s.BaseURL)
	}
	if s.GPU != "cpu" {
		t.Errorf("GPU = %q, want cpu", s.GPU)
	}
}

func TestRuntimeKeyNilSafe(t *testing.T) {
	t.Parallel()
	var s *Server
	if got := s.RuntimeKey(); got != "" {
		t.Errorf("nil.RuntimeKey() = %q, want \"\"", got)
	}
}

func TestRuntimeKeyIsStable(t *testing.T) {
	t.Parallel()
	s1 := NewServer(8080, "/opt/models/foo.gguf", "  auto  ")
	s2 := NewServer(8080, "/opt/models/foo.gguf", "auto")
	if s1.RuntimeKey() != s2.RuntimeKey() {
		t.Errorf("RuntimeKey should trim GPU whitespace: %q vs %q", s1.RuntimeKey(), s2.RuntimeKey())
	}
	if !strings.HasPrefix(s1.RuntimeKey(), "8080|") {
		t.Errorf("RuntimeKey should start with port: %q", s1.RuntimeKey())
	}
}

func TestRuntimeKeyDiffersOnModelChange(t *testing.T) {
	t.Parallel()
	s1 := NewServer(8080, "/a.gguf", "auto")
	s2 := NewServer(8080, "/b.gguf", "auto")
	if s1.RuntimeKey() == s2.RuntimeKey() {
		t.Errorf("RuntimeKey must differ on different model paths")
	}
}

func TestIsReadyNilSafe(t *testing.T) {
	t.Parallel()
	var s *Server
	if s.IsReady() {
		t.Error("nil.IsReady() must be false")
	}
}

func TestIsReadyStartsFalse(t *testing.T) {
	t.Parallel()
	s := NewServer(0, "/tmp/x.gguf", "")
	if s.IsReady() {
		t.Error("newly-constructed Server.IsReady() must be false")
	}
}

func TestStopServerNilSafe(t *testing.T) {
	t.Parallel()
	var s *Server
	// Must not panic.
	s.StopServer()
}

func TestStopServerWithoutStart(t *testing.T) {
	t.Parallel()
	s := NewServer(0, "/tmp/x.gguf", "")
	// Must not panic when cmd was never started.
	s.StopServer()
	if s.IsReady() {
		t.Error("IsReady must be false after StopServer")
	}
}

func TestVerifyInstallationReportsMissingBinary(t *testing.T) {
	// Not parallel: manipulates LOCALAPPDATA and Executable lookup.
	// Point bundle and managed-install directories at an empty temp dir so
	// FindServerBinary cannot resolve to a real install.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("SPEECHKIT_ALLOW_LLAMA_PATH", "0")

	s := NewServer(0, "/definitely/not/here.gguf", "")
	status := s.VerifyInstallation()
	if status.BinaryFound {
		// If the developer has llama-server alongside `os.Executable()`, this
		// would be a false positive. Accept but surface it in the log.
		t.Logf("binary found at %q (developer has llama bundle next to test runner)", status.BinaryPath)
	} else if len(status.Problems) == 0 {
		t.Error("expected at least one problem when binary missing")
	}
	if status.ModelFound {
		t.Error("model file must not be reported as found when path is bogus")
	}
	if status.ServerReady {
		t.Error("ServerReady must be false when server never started")
	}
}

func TestVerifyInstallationWithValidModel(t *testing.T) {
	// Not parallel: manipulates LOCALAPPDATA.
	modelDir := t.TempDir()
	modelPath := filepath.Join(modelDir, "tiny.gguf")
	if err := os.WriteFile(modelPath, []byte("fake-gguf-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	s := NewServer(0, modelPath, "cpu")
	status := s.VerifyInstallation()
	if !status.ModelFound {
		t.Errorf("ModelFound = false, want true (path=%s, problems=%v)", modelPath, status.Problems)
	}
	if status.ModelBytes == 0 {
		t.Error("ModelBytes must be non-zero for existing file")
	}
}

func TestProbeEndpointOK(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("probe should hit /models, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	if err := ProbeEndpoint(context.Background(), ts.URL); err != nil {
		t.Errorf("ProbeEndpoint(%s) = %v, want nil", ts.URL, err)
	}
}

func TestProbeEndpointNon200(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	err := ProbeEndpoint(context.Background(), ts.URL)
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code 500, got %v", err)
	}
}

func TestProbeEndpointInvalidURL(t *testing.T) {
	t.Parallel()
	if err := ProbeEndpoint(context.Background(), "not-a-url"); err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestHealthNilServer(t *testing.T) {
	t.Parallel()
	var s *Server
	if err := s.Health(context.Background()); err == nil {
		t.Error("Health on nil Server must return error")
	}
}

func TestHealthResetsReadyOnFailure(t *testing.T) {
	t.Parallel()
	s := NewServer(0, "/tmp/x.gguf", "")
	s.ready.Store(true) // pretend we were ready
	// Point at a closed server to force probe failure.
	s.BaseURL = "http://127.0.0.1:1/v1"
	if err := s.Health(context.Background()); err == nil {
		t.Error("expected probe failure for unreachable endpoint")
	}
	if s.IsReady() {
		t.Error("IsReady must be cleared after failed probe")
	}
}

func TestHealthSetsReadyOnSuccess(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	s := NewServer(0, "/tmp/x.gguf", "")
	s.BaseURL = ts.URL
	if err := s.Health(context.Background()); err != nil {
		t.Fatalf("Health = %v, want nil", err)
	}
	if !s.IsReady() {
		t.Error("IsReady must be true after successful probe")
	}
}
