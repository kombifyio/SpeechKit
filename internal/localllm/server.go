package localllm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

var ggufModelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*\.gguf$`)

const (
	defaultPort        = 8082
	defaultContextSize = 4096
	defaultThreads     = 4
	readyRetries       = 180
	readyInterval      = 500 * time.Millisecond
)

// ValidateModelPath verifies that path points at a safe GGUF model filename.
func ValidateModelPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("llama.cpp: model path is empty")
	}
	clean := filepath.Clean(path)
	if clean != path && filepath.ToSlash(clean) != filepath.ToSlash(path) {
		return fmt.Errorf("llama.cpp: model path must be in canonical form (got %q, want %q)", path, clean)
	}
	if strings.Contains(filepath.ToSlash(clean), "../") {
		return fmt.Errorf("llama.cpp: model path must not contain .. traversal: %s", clean)
	}
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("llama.cpp: model path must be absolute: %s", clean)
	}
	base := filepath.Base(clean)
	if !ggufModelPattern.MatchString(base) {
		return fmt.Errorf("llama.cpp: model filename %q does not match *.gguf pattern", base)
	}
	return nil
}

// Server manages SpeechKit's bundled llama.cpp OpenAI-compatible server.
type Server struct {
	BaseURL     string
	Port        int
	ModelPath   string
	GPU         string
	ContextSize int
	Threads     int
	Validation  netsec.ValidationOptions
	cmd         *exec.Cmd
	ready       atomic.Bool
	startMu     sync.Mutex
	startDone   chan struct{}
	client      *http.Client
}

func NewServer(port int, modelPath, gpu string) *Server {
	if port <= 0 {
		port = defaultPort
	}
	s := &Server{
		BaseURL:     fmt.Sprintf("http://127.0.0.1:%d/v1", port),
		Port:        port,
		ModelPath:   modelPath,
		GPU:         gpu,
		ContextSize: defaultContextSize,
		Threads:     defaultThreads,
		Validation: netsec.ValidationOptions{
			AllowLoopback: true,
			AllowHTTP:     true,
		},
	}
	s.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &s.Validation})
	return s
}

func (s *Server) RuntimeKey() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%d|%s|%s", s.Port, filepath.Clean(s.ModelPath), strings.TrimSpace(s.GPU))
}

func (s *Server) StartServer(ctx context.Context) error {
	done := make(chan struct{})
	s.startMu.Lock()
	s.startDone = done
	s.startMu.Unlock()
	defer close(done)

	binaryPath, err := FindServerBinary()
	if err != nil {
		return fmt.Errorf("llama.cpp server binary: %w", err)
	}
	if err := ValidateModelPath(s.ModelPath); err != nil {
		return err
	}
	if _, err := os.Stat(s.ModelPath); err != nil {
		return fmt.Errorf("model not found: %s", s.ModelPath)
	}

	args := []string{
		"--model", s.ModelPath,
		"--alias", filepath.Base(s.ModelPath),
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(s.Port),
		"--ctx-size", strconv.Itoa(s.ContextSize),
		"--threads", strconv.Itoa(s.Threads),
	}
	if strings.EqualFold(strings.TrimSpace(s.GPU), "cpu") {
		args = append(args, "--n-gpu-layers", "0")
	}

	s.cmd = exec.CommandContext(ctx, binaryPath, args...) //nolint:gosec // G204: binaryPath is resolved from trusted bundle locations.
	configureHiddenProcess(s.cmd)
	s.cmd.Stdout = os.Stderr
	s.cmd.Stderr = os.Stderr

	slog.Info("starting llama-server", "binary", binaryPath, "args", args)
	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start llama-server: %w", err)
	}

	if err := s.waitForReady(ctx); err != nil {
		s.StopServer()
		return err
	}

	s.ready.Store(true)
	slog.Info("llama-server ready", "url", s.BaseURL)
	return nil
}

func (s *Server) waitForReady(ctx context.Context) error {
	for i := 0; i < readyRetries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := ProbeEndpoint(ctx, s.BaseURL); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readyInterval):
		}
	}
	return fmt.Errorf("llama-server did not become ready after %v", time.Duration(readyRetries)*readyInterval)
}

func ProbeEndpoint(ctx context.Context, baseURL string) error {
	validation := netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true}
	endpoint, err := netsec.BuildEndpoint(strings.TrimSpace(baseURL), "models", validation)
	if err != nil {
		return err
	}
	client := netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 800 * time.Millisecond, DialValidation: &validation})
	probeCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // close failure is not actionable for readiness.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("llama-server status %d", resp.StatusCode)
	}
	return nil
}

func (s *Server) StopServer() {
	if s == nil {
		return
	}
	s.ready.Store(false)
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		s.cmd = nil
	}
}

func (s *Server) IsReady() bool {
	return s != nil && s.ready.Load()
}

func (s *Server) Health(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("llama-server not configured")
	}
	if err := ProbeEndpoint(ctx, s.BaseURL); err != nil {
		s.ready.Store(false)
		return err
	}
	s.ready.Store(true)
	return nil
}

type InstallStatus struct {
	BinaryFound bool     `json:"binaryFound"`
	BinaryPath  string   `json:"binaryPath"`
	ModelFound  bool     `json:"modelFound"`
	ModelPath   string   `json:"modelPath"`
	ModelBytes  int64    `json:"modelBytes"`
	ServerReady bool     `json:"serverReady"`
	Problems    []string `json:"problems,omitempty"`
}

func (s *Server) VerifyInstallation() InstallStatus {
	status := InstallStatus{
		ModelPath:   "",
		ServerReady: s.IsReady(),
	}
	if s != nil {
		status.ModelPath = s.ModelPath
	}

	binaryPath, err := FindServerBinary()
	if err != nil {
		status.Problems = append(status.Problems, "llama-server binary not found")
	} else {
		status.BinaryFound = true
		status.BinaryPath = binaryPath
	}

	if s == nil || strings.TrimSpace(s.ModelPath) == "" {
		status.Problems = append(status.Problems, "no GGUF model path configured")
	} else if err := ValidateModelPath(s.ModelPath); err != nil {
		status.Problems = append(status.Problems, err.Error())
	} else if fi, err := os.Stat(s.ModelPath); err != nil {
		status.Problems = append(status.Problems, fmt.Sprintf("GGUF model file missing: %s", s.ModelPath))
	} else {
		status.ModelBytes = fi.Size()
		status.ModelFound = true
	}

	return status
}

func FindServerBinary() (string, error) {
	return findServerBinary()
}

func findServerBinary() (string, error) {
	names := []string{"llama-server", "llama-server.exe"}
	if runtime.GOOS == "windows" {
		names = []string{"llama-server.exe"}
	}

	exe, _ := os.Executable()
	if exe != "" {
		exeDir := filepath.Dir(exe)
		for _, name := range names {
			for _, dir := range []string{
				filepath.Join(exeDir, "llama"),
				exeDir,
			} {
				path := filepath.Join(dir, name)
				if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: path is app bundle directory, not user input.
					return path, nil
				}
			}
		}
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		for _, dir := range []string{
			filepath.Join(localAppData, "SpeechKit", "llama"),
			filepath.Join(localAppData, "SpeechKit"),
			filepath.Join(localAppData, "SpeechKit", "bin"),
		} {
			for _, name := range names {
				path := filepath.Join(dir, name)
				if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: path is managed app directory, not user input.
					return path, nil
				}
			}
		}
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPEECHKIT_ALLOW_LLAMA_PATH")), "1") {
		for _, name := range names {
			if path, err := exec.LookPath(name); err == nil {
				slog.Warn("using llama-server from PATH due to SPEECHKIT_ALLOW_LLAMA_PATH=1", "path", path)
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("llama-server binary not found in bundle or managed install location")
}
