package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/procguard"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

// ProviderSherpaPiper is the provider name of the bundled on-device Piper
// voices that run through the speechkit-tts-sherpa sidecar.
const ProviderSherpaPiper = "sherpa_piper"

// ProfileSherpaPiper is the catalog profile that pins [ProviderSherpaPiper].
const ProfileSherpaPiper = "tts.local.sherpa-piper"

const sherpaRuntimeDir = "sherpa-runtime"

// sherpaReadyTimeout bounds how long the first synthesis waits for the
// sidecar to load both voices and answer /health.
const sherpaReadyTimeout = 45 * time.Second

// SherpaPiperOpts configures the bundled on-device TTS sidecar.
type SherpaPiperOpts struct {
	// Binary is speechkit-tts-sherpa. Empty looks it up next to this process
	// and in the managed install locations, the same places the speech-to-text
	// sidecar is found.
	Binary string
	// VoicesDir is the models/tts directory holding both Piper VITS voices and
	// espeak-ng-data. Empty looks for models/tts next to the binary and this
	// process.
	VoicesDir string
	// Timeout caps one synthesis, including a cold start. Zero means 45 s.
	Timeout time.Duration
}

// SherpaPiper speaks German and English on device through the
// speechkit-tts-sherpa sidecar. It needs no network and no API key. The
// sidecar starts on the first synthesis and stays up for the life of the
// provider.
type SherpaPiper struct {
	binary    string
	voicesDir string
	timeout   time.Duration

	mu      sync.Mutex
	baseURL string
	cmd     *exec.Cmd
	client  *http.Client
}

// NewSherpaPiper validates that the sidecar and the voice directory exist.
// The sidecar process starts on the first [SherpaPiper.Synthesize] or
// [SherpaPiper.Health], not at construction, so a host that never speaks pays
// nothing.
func NewSherpaPiper(opts SherpaPiperOpts) (*SherpaPiper, error) {
	binaryPath := strings.TrimSpace(opts.Binary)
	if binaryPath == "" {
		found, err := findSherpaTTSBinary()
		if err != nil {
			return nil, err
		}
		binaryPath = found
	} else if info, err := os.Stat(binaryPath); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sherpa tts: sidecar %s is not a file", binaryPath)
	}
	voices := strings.TrimSpace(opts.VoicesDir)
	if voices == "" {
		found, err := findSherpaTTSVoices(binaryPath)
		if err != nil {
			return nil, err
		}
		voices = found
	} else if err := sherpaVoicesPresent(voices); err != nil {
		return nil, err
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = sherpaReadyTimeout
	}
	return &SherpaPiper{
		binary:    binaryPath,
		voicesDir: voices,
		timeout:   timeout,
		client:    &http.Client{Timeout: timeout},
	}, nil
}

// Name returns [ProviderSherpaPiper].
func (*SherpaPiper) Name() string { return ProviderSherpaPiper }

// Kind reports [ProviderKindLocalBuiltIn]: the sidecar and the voices run on
// this device and need no network.
func (*SherpaPiper) Kind() ProviderKind { return ProviderKindLocalBuiltIn }

// Health starts the sidecar if it is not running and checks /health.
func (s *SherpaPiper) Health(ctx context.Context) error {
	if s == nil {
		return errors.New("sherpa tts: nil provider")
	}
	base, err := s.ensure(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sherpa tts: health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sherpa tts: health status %d", resp.StatusCode)
	}
	return nil
}

// Synthesize returns a WAV of text spoken by the voice locale selects. German
// locales use Ramona; everything else uses LJSpeech.
func (s *SherpaPiper) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if s == nil {
		return nil, errors.New("sherpa tts: nil provider")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("sherpa tts: empty text")
	}
	resolved := ResolveSynthesizeOptions(ProviderSherpaPiper, ProfileSherpaPiper, opts, provideropts.Values{}, provideropts.Values{})
	base, err := s.ensure(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{
		"text":   text,
		"locale": resolved.Locale,
		"voice":  resolved.Voice,
		"speed":  resolved.Speed,
	})
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sherpa tts: synthesize: %w", err)
	}
	defer resp.Body.Close()
	audio, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("sherpa tts: read audio: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sherpa tts: synthesize status %d: %s", resp.StatusCode, strings.TrimSpace(string(audio)))
	}
	if len(audio) < 44 {
		return nil, errors.New("sherpa tts: response is not a WAV")
	}
	sampleRate := readWAVSampleRate(audio)
	voice := resp.Header.Get("X-SpeechKit-Voice")
	if voice == "" {
		voice = ProviderSherpaPiper
	}
	return &Result{
		Audio:      audio,
		Format:     "wav",
		SampleRate: sampleRate,
		Duration:   estimateWAVDuration(audio, sampleRate),
		Provider:   ProviderSherpaPiper,
		Voice:      voice,
	}, nil
}

// Close stops the sidecar. A later synthesis starts a new one.
func (s *SherpaPiper) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *SherpaPiper) ensure(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.baseURL != "" && s.cmd != nil && s.cmd.ProcessState == nil {
		return s.baseURL, nil
	}
	if err := procguard.RequireSpeechConfinement(); err != nil {
		return "", err
	}
	port, err := sherpaFreePort(ctx)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, s.binary, // #nosec G204 -- binary is the bundled sidecar resolved by findSherpaTTSBinary or an explicit path checked at construction.
		"--voices", s.voicesDir,
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
	)
	cmd.Dir = filepath.Dir(s.binary)
	cmd.Env = procguard.HostSidecarEnv()
	procguard.ConfigureHiddenProcess(cmd, true)
	cmd.Stderr = procguard.ForwardOutput(os.Stderr)
	if err := procguard.Start(cmd); err != nil {
		return "", fmt.Errorf("sherpa tts: start sidecar: %w", err)
	}
	procguard.AdoptOrReport(cmd, func(err error) {
		logutil.Resolve(nil).Warn("sherpa tts sidecar not adopted into the kill-on-exit job", "error", err)
	})
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	if err := sherpaWaitReady(ctx, s.client, base); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", err
	}
	s.cmd = cmd
	s.baseURL = base
	return base, nil
}

func (s *SherpaPiper) stopLocked() {
	if s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
	s.cmd = nil
	s.baseURL = ""
}

func sherpaFreePort(ctx context.Context) (int, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("sherpa tts: choose port: %w", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("sherpa tts: unexpected listener address")
	}
	return addr.Port, nil
}

func sherpaWaitReady(ctx context.Context, client *http.Client, base string) error {
	deadline := time.Now().Add(sherpaReadyTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	var last error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if last == nil {
		last = errors.New("timed out")
	}
	return fmt.Errorf("sherpa tts: sidecar did not become ready: %w", last)
}

func findSherpaTTSBinary() (string, error) {
	name := "speechkit-tts-sherpa"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), sherpaRuntimeDir))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		root := filepath.Join(local, "SpeechKit")
		dirs = append(dirs, root, filepath.Join(root, sherpaRuntimeDir), filepath.Join(root, "bin"), filepath.Join(root, "bin", sherpaRuntimeDir))
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", errors.New("sherpa tts: speechkit-tts-sherpa not found next to the app or in the managed install location")
}

func findSherpaTTSVoices(binaryPath string) (string, error) {
	var roots []string
	if binaryPath != "" {
		// The sidecar lives in sherpa-runtime/; the voices live in models/tts
		// next to the app, one directory up.
		roots = append(roots, filepath.Dir(filepath.Dir(binaryPath)), filepath.Dir(binaryPath))
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		roots = append(roots, filepath.Join(local, "SpeechKit"), filepath.Join(local, "SpeechKit", "bin"))
	}
	var tried []string
	for _, root := range roots {
		dir := filepath.Join(root, "models", "tts")
		tried = append(tried, dir)
		if err := sherpaVoicesPresent(dir); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("sherpa tts: bundled voices not found (looked in %s)", strings.Join(tried, ", "))
}

func sherpaVoicesPresent(dir string) error {
	for _, name := range []string{
		filepath.Join("vits-piper-de_DE-ramona-low", "de_DE-ramona-low.onnx"),
		filepath.Join("vits-piper-en_US-ljspeech-medium", "en_US-ljspeech-medium.onnx"),
		"espeak-ng-data",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("sherpa tts: voices in %s are incomplete (%s missing)", dir, name)
		}
	}
	return nil
}
