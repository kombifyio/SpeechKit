package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"time"
)

const (
	whisperHealthRetries  = 30
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
	if p.GPU != "" && p.GPU != "cpu" {
		args = append(args, "--gpu", p.GPU)
	}

	p.cmd = exec.CommandContext(ctx, binaryPath, args...)
	configureHiddenProcess(p.cmd)
	p.cmd.Stdout = os.Stderr // whisper-server logs to stdout
	p.cmd.Stderr = os.Stderr

	log.Printf("Starting whisper-server: %s %v", binaryPath, args)
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("start whisper-server: %w", err)
	}

	if err := p.waitForReady(ctx); err != nil {
		p.StopServer()
		return err
	}

	p.ready.Store(true)
	log.Printf("whisper-server ready on %s", p.BaseURL)
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

		resp, err := http.Get(healthURL)
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
		return nil, fmt.Errorf("local whisper-server not ready")
	}

	endpoint := fmt.Sprintf("%s/v1/audio/transcriptions", p.BaseURL)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	part.Write(audio)

	if opts.Language != "" && opts.Language != "auto" {
		writer.WriteField("language", opts.Language)
	}
	writer.WriteField("model", "whisper-1")
	writer.Close()

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

// findWhisperBinary looks for the whisper-server executable in standard locations.
func findWhisperBinary() (string, error) {
	names := []string{"whisper-server", "whisper-server.exe"}
	if runtime.GOOS == "windows" {
		names = []string{"whisper-server.exe"}
	}

	// Check PATH
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}

	// Check local app data
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		for _, name := range names {
			path := filepath.Join(localAppData, "SpeechKit", "bin", name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// Check next to executable
	exe, _ := os.Executable()
	if exe != "" {
		for _, name := range names {
			path := filepath.Join(filepath.Dir(exe), name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("whisper-server binary not found in PATH or standard locations")
}
