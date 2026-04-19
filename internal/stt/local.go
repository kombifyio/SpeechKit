package stt

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/netsec"
)

// whisperModelPattern restricts whisper.cpp model filenames to the
// ggml-<variant>.bin naming convention. This blocks attempts to load
// arbitrary binaries or paths containing shell metacharacters.
var whisperModelPattern = regexp.MustCompile(`^ggml-[A-Za-z0-9._\-]+\.bin$`)

// ValidateModelPath verifies that path points at a whisper.cpp ggml model
// file with a safe filename. It rejects path traversal, non-absolute paths,
// and filenames that don't match the ggml-*.bin pattern.
func ValidateModelPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("whisper: model path is empty")
	}
	clean := filepath.Clean(path)
	if clean != path && filepath.ToSlash(clean) != filepath.ToSlash(path) {
		// filepath.Clean collapses ../ and double separators. A change
		// means the caller supplied something suspicious.
		return fmt.Errorf("whisper: model path must be in canonical form (got %q, want %q)", path, clean)
	}
	if strings.Contains(filepath.ToSlash(clean), "../") {
		return fmt.Errorf("whisper: model path must not contain .. traversal: %s", clean)
	}
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("whisper: model path must be absolute: %s", clean)
	}
	base := filepath.Base(clean)
	if !whisperModelPattern.MatchString(base) {
		return fmt.Errorf("whisper: model filename %q does not match ggml-*.bin pattern", base)
	}
	return nil
}

const (
	whisperHealthRetries      = 120
	whisperHealthInterval     = 500 * time.Millisecond
	whisperWarmupRetries      = 180
	whisperWarmupInterval     = 500 * time.Millisecond
	whisperWarmupTimeout      = 90 * time.Second
	localMinTranscribeTimeout = 60 * time.Second
	localMaxTranscribeTimeout = 5 * time.Minute
	localMaxResponseBytes     = 1 << 20
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
		client:    netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second}),
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

	if err := ValidateModelPath(p.ModelPath); err != nil {
		return err
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

	p.cmd = exec.CommandContext(ctx, binaryPath, args...) //nolint:gosec // G204: binaryPath from app data dir, not user input
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
	if err := p.waitForInferenceReady(ctx); err != nil {
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

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, http.NoBody)
		if reqErr != nil {
			return fmt.Errorf("create health request: %w", reqErr)
		}
		resp, err := p.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(whisperHealthInterval)
	}
	return fmt.Errorf("whisper-server did not become ready after %v", time.Duration(whisperHealthRetries)*whisperHealthInterval)
}

func (p *LocalProvider) waitForInferenceReady(ctx context.Context) error {
	warmupCtx, cancel := context.WithTimeout(ctx, whisperWarmupTimeout)
	defer cancel()

	warmupClient := netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: whisperWarmupTimeout})
	return p.waitForInferenceReadyWithClient(warmupCtx, warmupClient, whisperWarmupRetries, whisperWarmupInterval)
}

func (p *LocalProvider) waitForInferenceReadyWithRetry(ctx context.Context, retries int, interval time.Duration) error {
	return p.waitForInferenceReadyWithClient(ctx, p.client, retries, interval)
}

func (p *LocalProvider) waitForInferenceReadyWithClient(ctx context.Context, client *http.Client, retries int, interval time.Duration) error {
	if retries <= 0 {
		retries = 1
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	if client == nil {
		client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second})
	}

	endpoint := fmt.Sprintf("%s/v1/audio/transcriptions", p.BaseURL)
	warmupAudio := buildWarmupWAV()
	var lastErr error

	for i := 0; i < retries; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := p.probeInferenceReady(ctx, client, endpoint, warmupAudio); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if i == retries-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unknown warmup failure")
	}
	return fmt.Errorf("whisper-server inference not ready: %w", lastErr)
}

func (p *LocalProvider) probeInferenceReady(ctx context.Context, client *http.Client, endpoint string, audioData []byte) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "warmup.wav")
	if err != nil {
		return fmt.Errorf("create warmup form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return fmt.Errorf("write warmup audio: %w", err)
	}
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return fmt.Errorf("write warmup model field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close warmup multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("create warmup request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, localMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read warmup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("warmup status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil
}

func buildWarmupWAV() []byte {
	// 200ms of silence is enough to verify the inference route without
	// adding noticeable startup cost or depending on user audio.
	pcm := make([]byte, (audio.SampleRate/5)*audio.BytesPerSample)
	return audio.PCMToWAV(pcm)
}

// StopServer terminates the whisper-server subprocess.
func (p *LocalProvider) StopServer() {
	p.ready.Store(false)
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
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

	requestTimeout := localTranscribeTimeout(audio)
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	start := time.Now()
	resp, err := transcribeHTTPClient(p.client, requestTimeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("local transcribe: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
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

func localTranscribeTimeout(audioData []byte) time.Duration {
	timeout := localMinTranscribeTimeout
	if durationSecs := estimateWAVDurationSecs(audioData); durationSecs > 0 {
		scaled := 20*time.Second + time.Duration(durationSecs*3*float64(time.Second))
		if scaled > timeout {
			timeout = scaled
		}
	}
	if timeout > localMaxTranscribeTimeout {
		return localMaxTranscribeTimeout
	}
	return timeout
}

func transcribeHTTPClient(base *http.Client, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = localMinTranscribeTimeout
	}
	if base == nil {
		return netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: timeout + 5*time.Second})
	}
	cloned := *base
	cloned.Timeout = timeout + 5*time.Second
	return &cloned
}

func estimateWAVDurationSecs(audioData []byte) float64 {
	if len(audioData) >= 44 &&
		string(audioData[0:4]) == "RIFF" &&
		string(audioData[8:12]) == "WAVE" {
		channels := int(binary.LittleEndian.Uint16(audioData[22:24]))
		sampleRate := int(binary.LittleEndian.Uint32(audioData[24:28]))
		bitsPerSample := int(binary.LittleEndian.Uint16(audioData[34:36]))
		dataSize := int(binary.LittleEndian.Uint32(audioData[40:44]))
		bytesPerFrame := channels * (bitsPerSample / 8)
		if sampleRate > 0 && bytesPerFrame > 0 && dataSize > 0 {
			return float64(dataSize/bytesPerFrame) / float64(sampleRate)
		}
	}
	return audio.PCMDurationSecs(audioData)
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
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, http.NoBody)
	if err != nil {
		return err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.ready.Store(false)
		return fmt.Errorf("local health: %w", err)
	}
	_ = resp.Body.Close()

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
	} else if err := ValidateModelPath(p.ModelPath); err != nil {
		status.Problems = append(status.Problems, err.Error())
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

// FindWhisperBinary exposes the local whisper runtime lookup for callers that
// need to reflect runtime readiness without starting the subprocess.
func FindWhisperBinary() (string, error) {
	return findWhisperBinary()
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
			if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: path is app data dir, not user input
				return path, nil
			}
		}
	}

	// Check managed install location next.
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData != "" {
		searchDirs := []string{
			filepath.Join(localAppData, "SpeechKit"),
			filepath.Join(localAppData, "SpeechKit", "bin"),
		}
		for _, dir := range searchDirs {
			for _, name := range names {
				path := filepath.Join(dir, name)
				if _, err := os.Stat(path); err == nil { //nolint:gosec // G703: path is app data dir, not user input
					return path, nil
				}
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
