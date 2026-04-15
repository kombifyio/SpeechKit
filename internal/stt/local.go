package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	whisperHealthRetries  = 120
	whisperHealthInterval = 500 * time.Millisecond
	localMaxResponseBytes = 1 << 20
)

// LocalProvider implements STTProvider for Tier 1: localhost whisper.cpp server.
type LocalProvider struct {
	BaseURL   string // e.g. "http://127.0.0.1:8080"
	Port      int
	ModelPath string
	GPU       string
	cmd       *exec.Cmd
	ready     atomic.Bool
	startMu   sync.Mutex
	startDone chan struct{} // closed when the current StartServer call completes (nil = never started)
	client    *http.Client
}

func NewLocalProvider(port int, modelPath, gpu string) *LocalProvider {
	return &LocalProvider{
		BaseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		Port:      port,
		ModelPath: modelPath,
		GPU:       gpu,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// StartServer starts the whisper.cpp server subprocess. Blocks until ready or context cancelled.
func (p *LocalProvider) StartServer(ctx context.Context) error {
	// Create a fresh startup-done channel; Transcribe callers will wait on it.
	done := make(chan struct{})
	p.startMu.Lock()
	p.startDone = done
	p.startMu.Unlock()
	defer close(done)

	binaryPath, err := findWhisperBinary()
	if err != nil {
		return fmt.Errorf("whisper binary: %w", err)
	}

	if _, err := os.Stat(p.ModelPath); err != nil {
		return fmt.Errorf("model not found: %s", p.ModelPath)
	}

	args := []string{
		"--model", p.ModelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", p.Port),
		"--threads", "4",
		"--inference-path", "/v1/audio/transcriptions",
	}
	// whisper.cpp uses GPU by default; only pass --no-gpu when explicitly disabled.
	// "auto" and "" mean let whisper.cpp decide (default behavior).
	if p.GPU == "cpu" {
		args = append(args, "--no-gpu")
	}

	p.cmd = exec.CommandContext(ctx, binaryPath, args...)
	configureHiddenProcess(p.cmd)
	p.cmd.Stdout = os.Stderr // whisper-server logs to stdout
	p.cmd.Stderr = os.Stderr

	slog.Info("starting whisper-server", "binary", binaryPath, "args", args)
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start whisper-server: %w", err)
	}

	if err := p.waitForReady(ctx); err != nil {
		p.StopServer()
		return err
	}

	p.ready.Store(true)
	slog.Info("whisper-server ready", "url", p.BaseURL)
	return nil
}

func (p *LocalProvider) waitForReady(ctx context.Context) error {
	healthURL := fmt.Sprintf("%s/health", p.BaseURL)
	for i := 0; i < whisperHealthRetries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if reqErr != nil {
			return fmt.Errorf("create health request: %w", reqErr)
		}
		resp, err := p.client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(whisperHealthInterval)
	}
	return fmt.Errorf("whisper-server did not become ready after %v", time.Duration(whisperHealthRetries)*whisperHealthInterval)
}

// StopServer terminates the whisper-server subprocess.
func (p *LocalProvider) StopServer() {
	p.ready.Store(false)
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
		p.cmd = nil
	}
}

func (p *LocalProvider) Transcribe(ctx context.Context, audio []byte, opts TranscribeOpts) (*Result, error) {
	if !p.ready.Load() {
		// If startup is in progress, wait for it to complete before failing.
		p.startMu.Lock()
		done := p.startDone
		p.startMu.Unlock()
		if done != nil {
			slog.Info("whisper-server: waiting for startup to complete...")
			select {
			case <-done:
				// startup finished — check ready below
			case <-ctx.Done():
				return nil, fmt.Errorf("local whisper-server not ready: cancelled while waiting for startup")
			}
		}
		if !p.ready.Load() {
			return nil, fmt.Errorf("local whisper-server not ready")
		}
	}

	endpoint := fmt.Sprintf("%s/v1/audio/transcriptions", p.BaseURL)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	if opts.Language != "" && opts.Language != "auto" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return nil, fmt.Errorf("write language field: %w", err)
		}
	}
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}
	if opts.Prompt != "" {
		if err := writer.WriteField("prompt", opts.Prompt); err != nil {
			return nil, fmt.Errorf("write prompt field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local transcribe: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, localMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	lang := opts.Language
	if lang == "" {
		lang = "de"
	}

	return &Result{
		Text:     result.Text,
		Language: lang,
		Duration: duration,
		Provider: p.Name(),
		Model:    p.displayModel(),
	}, nil
}

func (p *LocalProvider) Name() string {
	return "local"
}

func (p *LocalProvider) displayModel() string {
	if p.ModelPath == "" {
		return ""
	}
	return filepath.Base(p.ModelPath)
}

func (p *LocalProvider) Health(ctx context.Context) error {
	if !p.ready.Load() {
		return fmt.Errorf("whisper-server not running")
	}

	healthURL := fmt.Sprintf("%s/health", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.ready.Store(false)
		return fmt.Errorf("local health: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("local health: status %d", resp.StatusCode)
	}
	return nil
}

// IsReady returns true if the whisper-server subprocess is running and responding.
func (p *LocalProvider) IsReady() bool {
	return p.ready.Load()
}

// InstallStatus describes what's present and what's missing for local STT.
type InstallStatus struct {
	BinaryFound bool     `json:"binaryFound"`
	BinaryPath  string   `json:"binaryPath"`
	ModelFound  bool     `json:"modelFound"`
	ModelPath   string   `json:"modelPath"`
	ModelBytes  int64    `json:"modelBytes"`
	ServerReady bool     `json:"serverReady"`
	Problems    []string `json:"problems,omitempty"`
}

// MinWhisperModelBytes is the minimum file size we expect for a valid ggml model.
// ggml-base.bin is ~150 MB; anything under 50 MB is clearly corrupt/truncated.
const MinWhisperModelBytes = 50_000_000

// VerifyInstallation checks binary and model availability without starting the server.
func (p *LocalProvider) VerifyInstallation() InstallStatus {
	status := InstallStatus{
		ModelPath:   p.ModelPath,
		ServerReady: p.ready.Load(),
	}

	// Check binary.
	binaryPath, err := findWhisperBinary()
	if err != nil {
		status.Problems = append(status.Problems, "whisper-server binary not found")
	} else {
		status.BinaryFound = true
		status.BinaryPath = binaryPath
	}

	// Check model file.
	if p.ModelPath == "" {
		status.Problems = append(status.Problems, "no model path configured")
	} else if fi, err := os.Stat(p.ModelPath); err != nil {
		status.Problems = append(status.Problems, fmt.Sprintf("model file missing: %s", p.ModelPath))
	} else {
		status.ModelBytes = fi.Size()
		if fi.Size() < MinWhisperModelBytes {
			status.Problems = append(status.Problems, fmt.Sprintf("model file too small (%d bytes) — likely corrupt or truncated", fi.Size()))
		} else {
			status.ModelFound = true
		}
	}

	return status
}

// findWhisperBinary looks for the whisper-server executable in standard locations.
func findWhisperBinary() (string, error) {
	names := []string{"whisper-server", "whisper-server.exe"}
	if runtime.GOOS == "windows" {
		names = []string{"whisper-server.exe"}
	}

	// Check next to executable first (trusted bundle path).
	exe, _ := os.Executable()
	if exe != "" {
		for _, name := range names {
			path := filepath.Join(filepath.Dir(exe), name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// Check managed install location next.
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		for _, name := range names {
			path := filepath.Join(localAppData, "SpeechKit", "bin", name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// Optional developer escape hatch: allow PATH lookup explicitly.
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPEECHKIT_ALLOW_WHISPER_PATH")), "1") {
		for _, name := range names {
			if path, err := exec.LookPath(name); err == nil {
				slog.Warn("using whisper-server from PATH due to SPEECHKIT_ALLOW_WHISPER_PATH=1", "path", path)
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("whisper-server binary not found in bundle or managed install location")
}
