//go:build cgo

// speechkit-wakeword is the sidecar binary that hosts SpeechKit's on-device
// keyword spotter outside the main desktop process.
//
// Build tag: requires cgo. The sherpa-onnx KWS engine is a native shared
// library bound via github.com/k2-fsa/sherpa-onnx-go, which cannot link
// when CGO_ENABLED=0. Production builds via scripts/build.ps1 enable cgo;
// for `go build ./...` clean compiles in cgo-disabled dev environments
// the package is correctly skipped.
//
// Why a sidecar: github.com/k2-fsa/sherpa-onnx-go works correctly when it
// is the only ONNX consumer in a process and gets to control thread setup
// at startup. In SpeechKit's Wails-alpha host process the runtime
// initialisation races with whatever Wails has already set up and aborts
// natively. LiveKit's own wake-word reference (livekit/livekit-wakeword)
// resolves the same class of problem the same way — wake-word runs as a
// sibling process to the agent, not in-process.
//
// IPC contract (stable across SpeechKit clients):
//
//   - stdout: JSON events, one per line. Schema in ipc.go (Event).
//   - stdin: JSON commands, one per line. Schema in ipc.go (Command).
//   - stderr: structured slog logs (json handler) — useful in CI and for
//     tail-fanning into the main app's log surface.
//
// Exit codes:
//
//	0 — clean shutdown after a shutdown command or stdin EOF.
//	2 — runtime / model error (host should restart with backoff).
//	3 — bad config (model assets missing; host should NOT restart until
//	    config changes).
//
// The Windows reference adapter in cmd/speechkit/desktop_wakeword.go owns
// the process lifecycle: it spawns this binary, parses stdout line-by-line,
// forwards detections into the desktop's hotkey-event bus, and respawns
// the sidecar on unexpected exit. Other client adapters (Local-Target CLI,
// future Android/iOS gomobile shells) follow the same pattern.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	_ "github.com/kombifyio/SpeechKit/internal/audio" // register WASAPI backend
	"github.com/kombifyio/SpeechKit/internal/wakeword"
)

// AppVersion is injected at build time via -ldflags. Defaults to "dev" for
// local builds. The host adapter reads this from the "ready" event to log
// which sidecar build is running.
var AppVersion = "dev"

func main() {
	cfg := parseFlags()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		emit(Event{Type: EventError, Msg: err.Error(), At: time.Now()})
		slog.Error("sidecar exited with error", "err", err)
		var bad badConfigError
		if errors.As(err, &bad) {
			os.Exit(3)
		}
		os.Exit(2)
	}
}

// runtime owns the wake-word detector + pipeline + audio session for the
// life of one sidecar run. Hot-reload tears it down and constructs a new
// one against the updated config.
type runtime struct {
	mu        sync.Mutex
	detector  *wakeword.Detector
	pipeline  *wakeword.Pipeline
	session   audio.Session
	cfg       sidecarConfig
	startedAt time.Time

	// Cumulative since last heartbeat reset; accessed via sync/atomic.
	bytesIn int64
	decodes int64
}

func run(ctx context.Context, cfg sidecarConfig) error {
	rt, err := startRuntime(cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	emit(Event{
		Type:      EventReady,
		Version:   AppVersion,
		Backend:   "sherpa-onnx-zipformer-kws",
		Phrase:    cfg.DisplayPhrase,
		PhraseID:  cfg.PhraseID,
		Mode:      cfg.DefaultMode,
		Threshold: cfg.Threshold,
		At:        time.Now(),
	})

	// Pump stdin commands into a goroutine that mutates the runtime.
	go pumpCommands(ctx, rt)

	// Heartbeat every 30s with bytes/decode stats. Useful for the host to
	// confirm the sidecar is actually pulling audio rather than silently
	// hung in malgo or sherpa.
	go pumpHeartbeats(ctx, rt)

	<-ctx.Done()
	emit(Event{Type: EventShutdown, At: time.Now()})
	return nil
}

func startRuntime(cfg sidecarConfig) (*runtime, error) {
	detector, err := wakeword.NewDetector(wakeword.DetectorConfig{
		Encoder:      cfg.Encoder,
		Decoder:      cfg.Decoder,
		Joiner:       cfg.Joiner,
		Tokens:       cfg.Tokens,
		KeywordsFile: cfg.KeywordsFile,
		NumThreads:   cfg.NumThreads,
		Threshold:    cfg.Threshold,
		Debug:        cfg.Debug,
	})
	if err != nil {
		if strings.Contains(err.Error(), "asset missing") || strings.Contains(err.Error(), "no such file") {
			return nil, badConfigError{err: err}
		}
		return nil, fmt.Errorf("detector init: %w", err)
	}

	rt := &runtime{cfg: cfg, detector: detector, startedAt: time.Now()}

	pipe, err := wakeword.NewPipeline(detector, sinkFunc(func(ev wakeword.DetectionEvent) {
		emit(Event{
			Type:        EventDetection,
			Keyword:     ev.Keyword,
			Phrase:      ev.Phrase,
			Mode:        ev.Mode,
			Probability: ev.Probability,
			At:          ev.At,
		})
	}), wakeword.Config{
		Enabled:              true,
		Phrase:               cfg.DisplayPhrase,
		KeywordsFile:         cfg.KeywordsFile,
		DefaultMode:          cfg.DefaultMode,
		Threshold:            cfg.Threshold,
		MinConsecutiveFrames: cfg.MinConsecutiveFrames,
		Cooldown:             cfg.Cooldown,
	})
	if err != nil {
		_ = detector.Close()
		return nil, fmt.Errorf("pipeline init: %w", err)
	}
	rt.pipeline = pipe

	session, err := audio.Open(audio.Config{
		Backend:     audio.Backend(cfg.AudioBackend),
		DeviceID:    cfg.AudioDeviceID,
		SampleRate:  wakeword.SampleRate,
		Channels:    wakeword.Channels,
		FrameSizeMs: 32,
		LatencyHint: "interactive",
	})
	if err != nil {
		_ = pipe.Close()
		_ = detector.Close()
		return nil, fmt.Errorf("audio open: %w", err)
	}
	rt.session = session

	emitDeviceEvent(cfg.AudioBackend, cfg.AudioDeviceID)

	session.SetPCMHandler(func(pcm []byte) {
		rt.mu.Lock()
		p := rt.pipeline
		rt.mu.Unlock()
		if p == nil {
			return
		}
		decodes, _, perr := p.FeedPCM(pcm)
		atomic.AddInt64(&rt.bytesIn, int64(len(pcm)))
		atomic.AddInt64(&rt.decodes, int64(decodes))
		if perr != nil {
			emit(Event{Type: EventError, Msg: fmt.Sprintf("feed pcm: %v", perr), At: time.Now()})
		}
	})

	if err := session.Start(); err != nil {
		_ = session.Close()
		_ = pipe.Close()
		_ = detector.Close()
		return nil, fmt.Errorf("audio start: %w", err)
	}
	return rt, nil
}

// stats helpers — accessed via atomic for the heartbeat goroutine.
func (rt *runtime) snapshotStats() (bytes, decodes int64) {
	return atomic.LoadInt64(&rt.bytesIn), atomic.LoadInt64(&rt.decodes)
}

func (rt *runtime) resetStats() {
	atomic.StoreInt64(&rt.bytesIn, 0)
	atomic.StoreInt64(&rt.decodes, 0)
}

func (rt *runtime) Close() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.session != nil {
		_, _ = rt.session.Stop()
		_ = rt.session.Close()
		rt.session = nil
	}
	if rt.pipeline != nil {
		_ = rt.pipeline.Close()
		rt.pipeline = nil
	}
	if rt.detector != nil {
		_ = rt.detector.Close()
		rt.detector = nil
	}
}

func pumpHeartbeats(ctx context.Context, rt *runtime) {
	// First heartbeat after 5 s so the desktop panel can flip from the
	// initial "Starting / waiting for audio" status to "Active" (or to
	// the explicit no-audio error) without leaving the user staring at
	// an unconfirmed green indicator for 30 s. Subsequent heartbeats
	// follow the steady-state 30 s cadence.
	initial := time.NewTimer(5 * time.Second)
	defer initial.Stop()
	select {
	case <-ctx.Done():
		return
	case now := <-initial.C:
		bytes, decodes := rt.snapshotStats()
		emit(Event{
			Type:      EventHeartbeat,
			BytesIn:   bytes,
			DecodesIn: decodes,
			UptimeSec: int64(now.Sub(rt.startedAt).Seconds()),
			At:        now,
		})
		rt.resetStats()
	}

	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			bytes, decodes := rt.snapshotStats()
			emit(Event{
				Type:      EventHeartbeat,
				BytesIn:   bytes,
				DecodesIn: decodes,
				UptimeSec: int64(now.Sub(rt.startedAt).Seconds()),
				At:        now,
			})
			rt.resetStats()
		}
	}
}

func pumpCommands(ctx context.Context, rt *runtime) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var cmd Command
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			emit(Event{Type: EventError, Msg: fmt.Sprintf("invalid command JSON: %v", err), At: time.Now()})
			continue
		}
		switch cmd.Type {
		case CommandShutdown:
			emit(Event{Type: EventLog, Level: "info", Msg: "shutdown command received", At: time.Now()})
			// Returning from this goroutine doesn't end the program; we
			// need the main run() to observe ctx.Done. Signal it by
			// raising SIGINT on ourselves.
			_ = sendSelfSignal()
			return
		default:
			emit(Event{Type: EventLog, Level: "warn", Msg: fmt.Sprintf("unknown command type %q", cmd.Type), At: time.Now()})
		}
		if ctx.Err() != nil {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emit(Event{Type: EventLog, Level: "warn", Msg: fmt.Sprintf("stdin scanner: %v", err), At: time.Now()})
	}
}

// sidecarConfig is the resolved view of the CLI flags. Kept separate from
// the flag parsing so tests can construct one directly.
type sidecarConfig struct {
	Encoder       string
	Decoder       string
	Joiner        string
	Tokens        string
	KeywordsFile  string
	NumThreads    int
	Threshold     float32
	DefaultMode   string
	DisplayPhrase string
	PhraseID      string

	MinConsecutiveFrames int
	Cooldown             time.Duration

	AudioBackend  string
	AudioDeviceID string

	// Debug toggles sherpa-onnx C++ verbose logging via DetectorConfig.Debug.
	// The host adapter sets this when [wakeword] debug_mode = true. Off by
	// default — the C++ side is chatty.
	Debug bool
}

// emitDeviceEvent looks up the configured audio device name and emits a
// "device" event so the host can show "you wanted X, we opened Y" in the
// user log. Non-fatal — if enumeration fails the sidecar continues with
// the default device and just logs a warning.
func emitDeviceEvent(backend, requestedID string) {
	devices, err := audio.ListCaptureDevices(audio.Config{Backend: audio.Backend(backend)})
	if err != nil {
		emit(Event{
			Type:       EventDevice,
			DeviceKind: "enumeration-failed",
			DeviceID:   requestedID,
			Msg:        fmt.Sprintf("audio device enumeration failed: %v", err),
			At:         time.Now(),
		})
		return
	}
	requested := strings.TrimSpace(requestedID)
	if requested == "" {
		for _, d := range devices {
			if d.IsDefault {
				emit(Event{Type: EventDevice, DeviceKind: "default", DeviceID: d.ID, DeviceName: d.Name, At: time.Now()})
				return
			}
		}
		emit(Event{Type: EventDevice, DeviceKind: "default", DeviceID: "", DeviceName: "(system default — none flagged)", At: time.Now()})
		return
	}
	for _, d := range devices {
		if strings.EqualFold(strings.TrimSpace(d.ID), requested) {
			emit(Event{Type: EventDevice, DeviceKind: "requested", DeviceID: d.ID, DeviceName: d.Name, At: time.Now()})
			return
		}
	}
	// Requested ID not in enumeration → malgo will fall back to default.
	// Build a short list of what IS available so the user can pick a valid one.
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, d.Name)
	}
	emit(Event{
		Type:       EventDevice,
		DeviceKind: "default-fallback",
		DeviceID:   requested,
		DeviceName: "(system default — requested ID not in enumeration)",
		Msg:        fmt.Sprintf("requested capture id not found; %d devices available: %s", len(devices), strings.Join(names, " | ")),
		At:         time.Now(),
	})
}

func parseFlags() sidecarConfig {
	var (
		modelDir      = flag.String("model-dir", "", "directory holding encoder/decoder/joiner ONNX + tokens.txt + keywords.txt")
		keywordsFile  = flag.String("keywords-file", "", "override keywords.txt path (default <model-dir>/keywords.txt)")
		phraseID      = flag.String("phrase-id", "", "stable catalog ID for the active phrase, surfaced in events")
		phrase        = flag.String("phrase", "", "display name for the active phrase (e.g. \"Hey Quby\")")
		defaultMode   = flag.String("default-mode", "voice_agent", "mode to trigger on detection (dictate|assist|voice_agent)")
		threshold     = flag.Float64("threshold", 0.25, "minimum acoustic probability for sherpa-onnx to emit a hit")
		numThreads    = flag.Int("num-threads", 1, "CPU threads for the KWS engine (capped at 4)")
		minFrames     = flag.Int("min-consecutive-frames", 1, "consecutive decode windows the same keyword must fire in before emitting")
		cooldownMs    = flag.Int("cooldown-ms", 1500, "minimum interval between two emitted detections for the same keyword")
		audioBackend  = flag.String("audio-backend", "windows-wasapi-malgo", "audio capture backend identifier")
		audioDeviceID = flag.String("audio-device-id", "", "audio capture device ID (empty = default)")
		debug         = flag.Bool("debug", false, "enable sherpa-onnx C++ verbose logging (ModelConfig.Debug=1)")
		showVersion   = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(AppVersion)
		os.Exit(0)
	}

	if *modelDir == "" {
		fmt.Fprintln(os.Stderr, "speechkit-wakeword: --model-dir is required")
		os.Exit(3)
	}

	cfg := sidecarConfig{
		Encoder:              fileIn(*modelDir, "encoder-epoch-12-avg-2-chunk-16-left-64.onnx"),
		Decoder:              fileIn(*modelDir, "decoder-epoch-12-avg-2-chunk-16-left-64.onnx"),
		Joiner:               fileIn(*modelDir, "joiner-epoch-12-avg-2-chunk-16-left-64.onnx"),
		Tokens:               fileIn(*modelDir, "tokens.txt"),
		KeywordsFile:         *keywordsFile,
		NumThreads:           *numThreads,
		Threshold:            float32(*threshold),
		DefaultMode:          *defaultMode,
		DisplayPhrase:        *phrase,
		PhraseID:             *phraseID,
		MinConsecutiveFrames: *minFrames,
		Cooldown:             time.Duration(*cooldownMs) * time.Millisecond,
		AudioBackend:         *audioBackend,
		AudioDeviceID:        *audioDeviceID,
		Debug:                *debug,
	}
	if cfg.KeywordsFile == "" {
		cfg.KeywordsFile = fileIn(*modelDir, "keywords.txt")
	}
	return cfg
}

func fileIn(dir, name string) string { return dir + string(os.PathSeparator) + name }

// sinkFunc adapts a plain function to wakeword.Sink without importing the
// interface explicitly — keeps the import surface small in this file.
type sinkFunc func(wakeword.DetectionEvent)

func (f sinkFunc) Emit(ev wakeword.DetectionEvent) { f(ev) }

// badConfigError signals an unrecoverable configuration problem (missing
// model files, malformed paths). The host process should not auto-restart
// the sidecar until the user updates the config.
type badConfigError struct{ err error }

func (e badConfigError) Error() string { return e.err.Error() }
func (e badConfigError) Unwrap() error { return e.err }

// sendSelfSignal triggers the main run loop's signal.NotifyContext to
// observe shutdown. Falls back to os.Exit(0) on platforms that don't
// support SIGINT.
func sendSelfSignal() error {
	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		os.Exit(0)
		return err
	}
	if err := p.Signal(os.Interrupt); err != nil {
		os.Exit(0)
		return err
	}
	return nil
}
