package local

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"io"
	"log/slog"
	"mime/multipart"
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

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/procguard"
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
	// whisperCppAutoDetectLanguage is whisper.cpp's documented auto-detect
	// value. Its server help reads
	// "-l LANG, --language LANG  [en] spoken language ('auto' for auto-detect)",
	// so the default is English and the field must be sent explicitly to get
	// language-free transcription.
	whisperCppAutoDetectLanguage = "auto"

	// Cold-start budgets for whisper-server. The CI install-E2E loads
	// Whisper Large v3 Turbo (~1.6 GB) on a CPU-only runner; the model
	// can take 90+ s just to memory-map and ggml-init before the
	// /health endpoint starts responding. Pre-CI values (60 s + 90 s)
	// were a too-tight on cold cache.
	whisperHealthRetries  = 360 // 360 * 500ms = 180s health-wait
	whisperHealthInterval = 500 * time.Millisecond
	whisperWarmupRetries  = 360 // 360 * 500ms = 180s warmup-wait
	whisperWarmupInterval = 500 * time.Millisecond
	whisperWarmupTimeout  = 180 * time.Second
	// Cold-CPU per-request budget. Whisper Large v3 Turbo on a 2-core
	// GH-hosted runner takes ~70 s to transcribe a 1 s clip; the
	// previous 60 s floor caused dictation timeouts that never
	// reached the encoder. Generous floor keeps the existing 3x-audio
	// scaling intact for longer clips.
	localMinTranscribeTimeout = 5 * time.Minute
	localMaxTranscribeTimeout = 10 * time.Minute
	localMaxResponseBytes     = 1 << 20
)

// Provider implements stt.STTProvider for Tier 1: localhost whisper.cpp server.
type Provider struct {
	BaseURL    string // e.g. "http://127.0.0.1:8080"
	Port       int
	ModelPath  string
	GPU        string
	Validation netsec.ValidationOptions
	// LowerSubprocessPriority overrides the process-wide default set via
	// SetSubprocessPriorityLowered for this provider only. nil keeps the
	// default (true on Windows). Ignored outside Windows.
	LowerSubprocessPriority *bool
	cmd                     *exec.Cmd
	ready                   atomic.Bool
	startDone               chan struct{} // closed when the current StartServer call completes (nil = never started)
	stopMu                  sync.Mutex
	processMu               sync.Mutex
	processDone             chan struct{}
	processErr              error
	starting                bool
	stopping                bool
	stopRequested           bool
	generation              uint64
	client                  *http.Client
}

// New creates the built-in whisper.cpp provider. The process is
// not started; lifecycle stays with the host.
func New(port int, modelPath, gpu string) *Provider {
	p := &Provider{
		BaseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		Port:      port,
		ModelPath: modelPath,
		GPU:       gpu,
		Validation: netsec.ValidationOptions{
			AllowLoopback: true,
			AllowHTTP:     true,
			RequireLocal:  true,
		},
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

// StartServer starts the whisper.cpp server subprocess. Blocks until ready or context cancelled.
func (p *Provider) StartServer(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("whisper-server process context is required")
	}
	generation, startDone, err := p.beginProcessStart()
	if err != nil {
		return err
	}
	defer p.finishProcessStart(generation, startDone)

	binaryPath, err := findWhisperBinary()
	if err != nil {
		return fmt.Errorf("whisper binary: %w", err)
	}

	if err := ValidateModelPath(p.ModelPath); err != nil {
		return fmt.Errorf("validate whisper model path: %w", err)
	}
	if _, err := os.Stat(p.ModelPath); err != nil {
		return fmt.Errorf("model not found: %s", p.ModelPath)
	}
	if err := verifyReadableModelFile(p.ModelPath); err != nil {
		return fmt.Errorf("model not readable: %s: %w", p.ModelPath, err)
	}
	if err := p.processStartCanceled(ctx, generation); err != nil {
		return err
	}

	threads := defaultWhisperThreads()
	modelArg, workDir := whisperModelArgument(p.ModelPath)
	args := []string{
		"--model", modelArg,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", p.Port),
		"--threads", strconv.Itoa(threads),
		"--inference-path", "/v1/audio/transcriptions",
	}
	// whisper.cpp uses GPU by default; only pass --no-gpu when explicitly disabled.
	// "auto" and "" mean let whisper.cpp decide (default behavior).
	if p.GPU == "cpu" {
		args = append(args, "--no-gpu")
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...) // #nosec G204 -- binaryPath is resolved by findWhisperBinary from bundle/managed locations or explicit dev opt-in.
	cmd.Dir = workDir
	configureHiddenProcess(cmd, subprocessPriorityLowered(p.LowerSubprocessPriority))
	cmd.Stdout = os.Stderr // whisper-server logs to stdout
	cmd.Stderr = os.Stderr

	// gpu_mode is the *requested* mode; whether inference actually runs on a
	// GPU depends on how the bundled whisper-server was built (a CPU-only
	// build silently ignores "auto"/"cuda" and transcription scales with
	// audio length). whisper-server's own startup banner on stderr names the
	// backend it actually initialised — check it when latency looks CPU-bound.
	slog.Info("starting whisper-server", "binary", binaryPath, "args", args, "dir", workDir, "threads", threads, "gpu_mode", p.GPU)
	if modelArg == p.ModelPath && !isASCII(modelArg) {
		slog.Warn("whisper-server model path contains non-ASCII characters that cannot be passed through argv safely; move the model to an ASCII-only path if startup fails", "model", p.ModelPath)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start whisper-server: %w", err)
	}
	// Hand the child to the OS so it cannot outlive this process when the
	// host dies without running its cleanup path (crash, taskkill, dev-loop
	// rebuild). Assignment failing does not make the child unusable.
	if err := procguard.Adopt(cmd); err != nil {
		slog.Warn("whisper-server not adopted into the kill-on-exit job", "error", err, "pid", cmd.Process.Pid)
	}
	processDone := make(chan struct{})
	p.processMu.Lock()
	if p.generation != generation {
		p.processMu.Unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("whisper-server startup generation changed unexpectedly")
	}
	p.cmd = cmd
	p.processDone = processDone
	p.processErr = nil
	stopAfterPublish := p.stopRequested || ctx.Err() != nil
	p.processMu.Unlock()
	go p.waitForProcess(ctx, cmd, processDone, generation)
	if stopAfterPublish {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server startup was stopped")
	}

	if err := p.waitForReady(ctx); err != nil {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server health probe never returned ready: %w", err)
	}
	if err := p.waitForInferenceReady(ctx); err != nil {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server inference probe never returned ready: %w", err)
	}

	if err := p.markProcessReady(generation, processDone); err != nil {
		p.stopStartedProcess(generation, cmd, processDone)
		return fmt.Errorf("whisper-server stopped before readiness: %w", err)
	}
	slog.Info("whisper-server ready", "url", p.BaseURL)
	return nil
}

func (p *Provider) beginProcessStart() (uint64, chan struct{}, error) {
	done := make(chan struct{})
	p.processMu.Lock()
	defer p.processMu.Unlock()
	switch {
	case p.stopping:
		return 0, nil, fmt.Errorf("whisper-server is stopping")
	case p.starting || p.cmd != nil:
		return 0, nil, fmt.Errorf("whisper-server is already starting or running")
	}
	p.generation++
	p.starting = true
	p.stopRequested = false
	p.processDone = nil
	p.processErr = nil
	p.startDone = done
	p.ready.Store(false)
	return p.generation, done, nil
}

func (p *Provider) finishProcessStart(generation uint64, done chan struct{}) {
	p.processMu.Lock()
	if p.generation == generation {
		p.starting = false
	}
	close(done)
	p.processMu.Unlock()
}

func (p *Provider) processStartCanceled(ctx context.Context, generation uint64) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("whisper-server startup canceled: %w", err)
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.generation != generation || p.stopRequested || p.stopping {
		return fmt.Errorf("whisper-server startup was stopped")
	}
	return nil
}

func (p *Provider) markProcessReady(generation uint64, done chan struct{}) error {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.generation != generation || p.processDone != done {
		return fmt.Errorf("whisper-server startup generation is no longer active")
	}
	select {
	case <-done:
		if p.processErr != nil {
			return p.processErr
		}
		return fmt.Errorf("whisper-server stopped")
	default:
	}
	if p.stopRequested || p.stopping {
		return fmt.Errorf("whisper-server startup was stopped")
	}
	p.ready.Store(true)
	return nil
}

func (p *Provider) waitForProcess(ctx context.Context, cmd *exec.Cmd, done chan struct{}, generation uint64) {
	waitErr := cmd.Wait()
	p.recordProcessExit(ctx, cmd, done, generation, waitErr)
}

func (p *Provider) recordProcessExit(ctx context.Context, cmd *exec.Cmd, done chan struct{}, generation uint64, waitErr error) {
	p.processMu.Lock()
	current := p.generation == generation && p.cmd == cmd && p.processDone == done
	expected := !current || p.stopRequested || ctx.Err() != nil
	if current {
		p.ready.Store(false)
		p.cmd = nil
		if expected {
			p.processErr = nil
		} else if waitErr != nil {
			if cause := describeProcessExit(waitErr); cause != "" {
				p.processErr = fmt.Errorf("whisper-server exited unexpectedly (%s): %w", cause, waitErr)
			} else {
				p.processErr = fmt.Errorf("whisper-server exited unexpectedly: %w", waitErr)
			}
		} else {
			p.processErr = fmt.Errorf("whisper-server exited unexpectedly")
		}
	}
	close(done)
	p.processMu.Unlock()
}

func (p *Provider) stopStartedProcess(generation uint64, cmd *exec.Cmd, done <-chan struct{}) {
	p.processMu.Lock()
	if p.generation == generation {
		p.stopRequested = true
	}
	p.processMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if done != nil {
		<-done
	}
}

func defaultWhisperThreads() int {
	if raw := strings.TrimSpace(os.Getenv("SPEECHKIT_WHISPER_THREADS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	if n > 8 {
		return 8
	}
	return n
}

func (p *Provider) waitForReady(ctx context.Context) error {
	healthURL := fmt.Sprintf("%s/health", p.BaseURL)
	for i := 0; i < whisperHealthRetries; i++ {
		if err := p.runtimeExitError(); err != nil {
			return err
		}
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

func (p *Provider) waitForInferenceReady(ctx context.Context) error {
	warmupCtx, cancel := context.WithTimeout(ctx, whisperWarmupTimeout)
	defer cancel()

	warmupClient := netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: whisperWarmupTimeout, DialValidation: &p.Validation})
	return p.waitForInferenceReadyWithClient(warmupCtx, warmupClient, whisperWarmupRetries, whisperWarmupInterval)
}

func (p *Provider) waitForInferenceReadyWithRetry(ctx context.Context, retries int, interval time.Duration) error {
	return p.waitForInferenceReadyWithClient(ctx, p.client, retries, interval)
}

func (p *Provider) waitForInferenceReadyWithClient(ctx context.Context, client *http.Client, retries int, interval time.Duration) error {
	if retries <= 0 {
		retries = 1
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	if client == nil {
		client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	}

	endpoint := fmt.Sprintf("%s/v1/audio/transcriptions", p.BaseURL)
	warmupAudio := buildWarmupWAV()
	var lastErr error

	for i := 0; i < retries; i++ {
		if err := p.runtimeExitError(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := p.probeInferenceReady(ctx, client, endpoint, warmupAudio)
		if err == nil {
			return nil
		}
		lastErr = err

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

func (p *Provider) probeInferenceReady(ctx context.Context, client *http.Client, endpoint string, audioData []byte) error {
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
		return fmt.Errorf("POST warmup request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, localMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read warmup response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("local warmup", resp.StatusCode, respBody)
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
func (p *Provider) StopServer() {
	p.stopMu.Lock()
	defer p.stopMu.Unlock()
	cmd, processDone, startDone, starting := p.beginProcessStop()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if processDone != nil {
		<-processDone
	}
	if starting && startDone != nil {
		<-startDone
	}
	p.finishProcessStop()
}

func (p *Provider) beginProcessStop() (*exec.Cmd, <-chan struct{}, <-chan struct{}, bool) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.stopping = true
	p.stopRequested = true
	p.ready.Store(false)
	cmd := p.cmd
	processDone := p.processDone
	startDone := p.startDone
	starting := p.starting
	return cmd, processDone, startDone, starting
}

func (p *Provider) finishProcessStop() {
	p.processMu.Lock()
	p.stopping = false
	p.processMu.Unlock()
}

// RuntimeDone closes whenever the owned whisper-server subprocess exits.
// RuntimeError then reports an unexpected exit and stays nil for an explicit
// StopServer call or process-context cancellation.
func (p *Provider) RuntimeDone() <-chan struct{} {
	if p == nil {
		return nil
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	return p.processDone
}

func (p *Provider) RuntimeError() error {
	if p == nil {
		return nil
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	return p.processErr
}

func (p *Provider) runtimeExitError() error {
	done := p.RuntimeDone()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		if err := p.RuntimeError(); err != nil {
			return err
		}
		return fmt.Errorf("whisper-server stopped")
	default:
		return nil
	}
}

func (p *Provider) Transcribe(ctx context.Context, audioData []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	if !p.ready.Load() {
		// If startup is in progress, wait for it to complete before failing.
		p.processMu.Lock()
		done := p.startDone
		p.processMu.Unlock()
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
	resolved := stt.ResolveTranscribeOptions("local", "stt.local.whispercpp", opts, nil, nil)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("write audio data: %w", err)
	}

	// whisper.cpp's server defaults to English, not auto-detect:
	// "-l LANG, --language LANG  [en] spoken language ('auto' for auto-detect)".
	// Omitting the field therefore pins English rather than leaving the model
	// free, so multilanguage has to be sent explicitly as the documented
	// "auto" value. This is why the multilanguage sentinel is translated per
	// provider instead of being normalized away everywhere.
	language := resolved.APILanguage()
	if language == "" {
		language = whisperCppAutoDetectLanguage
	}
	if err := writer.WriteField("language", language); err != nil {
		return nil, fmt.Errorf("write language field: %w", err)
	}
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return nil, fmt.Errorf("write model field: %w", err)
	}
	if resolved.Prompt != "" {
		if err := writer.WriteField("prompt", resolved.Prompt); err != nil {
			return nil, fmt.Errorf("write prompt field: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	requestTimeout := localTranscribeTimeout(audioData)
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, "POST", endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	start := time.Now()
	resp, err := transcribeHTTPClient(p.client, requestTimeout, &p.Validation).Do(req)
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
		return nil, netsec.ProviderStatusError("local", resp.StatusCode, respBody)
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// whisper.cpp does not report which language it detected, so the label can
	// only echo what was asked for. APILanguage() is empty for a multilanguage
	// session, and reporting a locale there would be an inference — the exact
	// thing the multilanguage rule forbids.
	lang := stt.FirstNonEmptyTrimmed(resolved.APILanguage(), stt.LanguageMulti)

	return &stt.Result{
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

func transcribeHTTPClient(base *http.Client, timeout time.Duration, validation *netsec.ValidationOptions) *http.Client {
	if timeout <= 0 {
		timeout = localMinTranscribeTimeout
	}
	if base == nil {
		if validation == nil {
			localValidation := netsec.ValidationOptions{AllowLoopback: true, AllowHTTP: true, RequireLocal: true}
			validation = &localValidation
		}
		return netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: timeout + 5*time.Second, DialValidation: validation})
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

func (p *Provider) Name() string {
	return "local"
}

func (p *Provider) displayModel() string {
	if p.ModelPath == "" {
		return ""
	}
	return filepath.Base(p.ModelPath)
}

// hasProcess reports whether a child process is currently published. It is the
// source of truth for "is there a server to talk to"; ready is only a cache of
// the last probe against it.
func (p *Provider) hasProcess() bool {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	return p.cmd != nil
}

func (p *Provider) Health(ctx context.Context) error {
	// Gate on the process, not on ready. Gating on ready made the flag a
	// one-way latch: a single transport blip cleared it, and nothing could set
	// it back, because ready is raised in exactly one place (markProcessReady,
	// reachable only from a fresh StartServer) while beginProcessStart refuses
	// to start again as long as the still-alive child is published. The
	// observable result was that local dictation stayed "not ready" until the
	// app restarted, even though whisper-server was healthy the whole time.
	if !p.hasProcess() {
		return fmt.Errorf("whisper-server not running")
	}

	healthURL := fmt.Sprintf("%s/health", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("build whisper-server health request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.ready.Store(false)
		return fmt.Errorf("local health: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// A server answering 503 is not serving either. Leaving ready set here
		// advertised a provider that would fail every transcription.
		p.ready.Store(false)
		return fmt.Errorf("local health: status %d", resp.StatusCode)
	}
	// The probe succeeded against a live child, so the provider is usable
	// again. This is the recovery edge that was missing.
	p.ready.Store(true)
	return nil
}

// IsReady returns true if the whisper-server subprocess is running and responding.
func (p *Provider) IsReady() bool {
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
func (p *Provider) VerifyInstallation() InstallStatus {
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
		} else if err := verifyReadableModelFile(p.ModelPath); err != nil {
			status.Problems = append(status.Problems, fmt.Sprintf("model file not readable: %s (%v)", p.ModelPath, err))
		} else {
			status.ModelFound = true
		}
	}

	return status
}

func verifyReadableModelFile(path string) error {
	file, err := os.Open(path) // #nosec G304 -- path is validated by ValidateModelPath before this helper is called.
	if err != nil {
		return err
	}
	return file.Close()
}

// FindWhisperBinary exposes the local whisper runtime lookup for callers that
// need to reflect runtime readiness without starting the subprocess.
func FindWhisperBinary() (string, error) {
	return findWhisperBinary()
}

// findWhisperBinary looks for the whisper-server executable in standard locations.
//
// Per the kernel/adapter discipline in CLAUDE.md, this kernel function
// must stay platform-neutral. Windows-specific binary names and
// install locations live in local_search_windows.go;
// local_search_unix.go is the no-op fallback for Linux/macOS where
// the Server-Target reads its whisper path from server settings.
func findWhisperBinary() (string, error) {
	names := whisperBinaryNames()

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

	// Check platform-specific managed install locations.
	for _, dir := range platformWhisperSearchDirs() {
		for _, name := range names {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil { // #nosec G703 -- path is app data dir, not user input.
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

// Capabilities reports the speech-to-text baseline every provider satisfies.
func (*Provider) Capabilities() []speechkit.Capability { return stt.BaseCapabilities() }
