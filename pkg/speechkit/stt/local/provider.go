// Package local is the built-in speech-to-text provider: it runs a
// whisper.cpp whisper-server child process on loopback and speaks its
// OpenAI-compatible transcription route, so audio never leaves the machine.
// It needs the whisper-server binary next to the executable or in a managed
// install location (no cgo; PATH is consulted only with
// SPEECHKIT_ALLOW_WHISPER_PATH=1) and a ggml-*.bin model file. The host owns
// the process lifecycle through [Provider.StartServer] and
// [Provider.StopServer].
//
// Stability: Beta — API-checked; may change with a changelog callout.
package local

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

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
	// Logger receives this provider's diagnostics. Nil falls back to
	// slog.Default() at log time.
	Logger        *slog.Logger
	cmd           *exec.Cmd
	ready         atomic.Bool
	startDone     chan struct{} // closed when the current StartServer call completes (nil = never started)
	stopMu        sync.Mutex
	processMu     sync.Mutex
	processDone   chan struct{}
	processErr    error
	starting      bool
	stopping      bool
	stopRequested bool
	generation    uint64
	client        *http.Client
	// activeBaseURL is where the current child listens when Port is 0 and a
	// port was chosen at start; empty means BaseURL.
	activeBaseURL string
	// identity is the current child's proof of identity (nil when the
	// platform could not set one up).
	identity *serverIdentity
}

// Options configures [New].
type Options struct {
	// Port is the loopback port the whisper.cpp server listens on. 0 picks a
	// free ephemeral loopback port at every start, which another local
	// process cannot predict and bind first. A fixed port that is already in
	// use fails the start instead of trusting whatever listens there.
	Port int
	// ModelPath is the path to the whisper.cpp model file.
	ModelPath string
	// GPU selects the GPU backend passed to the server. Empty leaves it unset.
	GPU string
}

// New creates the built-in whisper.cpp provider. The process is
// not started; lifecycle stays with the host.
func New(opts Options) *Provider {
	p := &Provider{
		BaseURL:   fmt.Sprintf("http://127.0.0.1:%d", opts.Port),
		Port:      opts.Port,
		ModelPath: opts.ModelPath,
		GPU:       opts.GPU,
		Validation: netsec.ValidationOptions{
			AllowLoopback: true,
			AllowHTTP:     true,
			RequireLocal:  true,
		},
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &p.Validation})
	return p
}

// Name returns "local".
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

// Health reports an error when no whisper-server child is running; otherwise
// it probes the child's /health endpoint and updates [Provider.IsReady] with
// the outcome, so a provider that lost readiness recovers on the next probe.
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

	healthURL := fmt.Sprintf("%s/health", p.serverURL())
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
		return stt.HTTPError("local health", resp, nil)
	}
	// The probe succeeded against a live child, so the provider is usable
	// again. This is the recovery edge that was missing.
	p.ready.Store(true)
	return nil
}

// serverURL is the base URL of the current child.
func (p *Provider) serverURL() string {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.activeBaseURL != "" {
		return p.activeBaseURL
	}
	return p.BaseURL
}

// IsReady returns true if the whisper-server subprocess is running and responding.
func (p *Provider) IsReady() bool {
	return p.ready.Load()
}

// Capabilities reports the speech-to-text baseline every provider satisfies.
func (*Provider) Capabilities() []speechkit.Capability { return stt.BaseCapabilities() }

// log returns the provider's logger, or the default logger when none is set.
func (p *Provider) log() *slog.Logger { return logutil.Resolve(p.Logger) }
