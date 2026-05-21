//go:build cgo

// speechkit-openwakeword hosts the openWakeWord-compatible ONNX frontend in a
// sibling process. Keeping it outside SpeechKit.exe avoids loading a second
// ONNX runtime into the Wails host process while still making the LiveKit /
// openWakeWord detector path selectable for field testing.
package main

import (
	"bufio"
	"context"
	"encoding/binary"
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
	_ "github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
	ort "github.com/yalue/onnxruntime_go"
)

var AppVersion = "dev"

func main() {
	cfg := parseFlags()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		emit(Event{Type: EventError, Msg: err.Error(), At: time.Now()})
		slog.Error("openwakeword sidecar exited with error", "err", err)
		var bad badConfigError
		if errors.As(err, &bad) {
			os.Exit(3)
		}
		os.Exit(2)
	}
}

type runtime struct {
	mu        sync.Mutex
	detector  *openWakeWordDetector
	session   audio.Session
	cfg       sidecarConfig
	startedAt time.Time

	bytesIn int64
	decodes int64

	consecutive int
	lastTrigger time.Time
}

func run(ctx context.Context, cfg sidecarConfig) error {
	ort.SetSharedLibraryPath(cfg.OnnxRuntimePath)
	if err := ort.InitializeEnvironment(ort.WithLogLevelWarning()); err != nil {
		return fmt.Errorf("onnxruntime init: %w", err)
	}
	defer func() {
		if err := ort.DestroyEnvironment(); err != nil {
			slog.Warn("onnxruntime cleanup failed", "err", err)
		}
	}()

	rt, err := startRuntime(cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	emit(Event{
		Type:      EventReady,
		Version:   AppVersion,
		Backend:   "livekit-openwakeword-onnx",
		Phrase:    cfg.DisplayPhrase,
		PhraseID:  cfg.PhraseID,
		Mode:      cfg.DefaultMode,
		Threshold: cfg.Threshold,
		At:        time.Now(),
	})

	go pumpCommands(ctx)
	go pumpHeartbeats(ctx, rt)

	<-ctx.Done()
	emit(Event{Type: EventShutdown, At: time.Now()})
	return nil
}

func startRuntime(cfg sidecarConfig) (*runtime, error) {
	detector, err := newOpenWakeWordDetector(cfg)
	if err != nil {
		if isMissingAssetError(err) {
			return nil, badConfigError{err: err}
		}
		return nil, fmt.Errorf("detector init: %w", err)
	}
	rt := &runtime{cfg: cfg, detector: detector, startedAt: time.Now()}

	session, err := audio.Open(audio.Config{
		Backend:     audio.Backend(cfg.AudioBackend),
		DeviceID:    cfg.AudioDeviceID,
		SampleRate:  wakeword.SampleRate,
		Channels:    wakeword.Channels,
		FrameSizeMs: 32,
		LatencyHint: "interactive",
	})
	if err != nil {
		_ = detector.Close()
		return nil, fmt.Errorf("audio open: %w", err)
	}
	rt.session = session
	emitDeviceEvent(cfg.AudioBackend, cfg.AudioDeviceID)
	session.SetPCMHandler(rt.handlePCM)

	if err := session.Start(); err != nil {
		_ = session.Close()
		_ = detector.Close()
		return nil, fmt.Errorf("audio start: %w", err)
	}
	return rt, nil
}

func (rt *runtime) handlePCM(pcm []byte) {
	rt.mu.Lock()
	d := rt.detector
	rt.mu.Unlock()
	if d == nil {
		return
	}
	decodes, score, err := d.FeedPCM(pcm)
	atomic.AddInt64(&rt.bytesIn, int64(len(pcm)))
	atomic.AddInt64(&rt.decodes, int64(decodes))
	if err != nil {
		emit(Event{Type: EventError, Msg: fmt.Sprintf("feed pcm: %v", err), At: time.Now()})
		return
	}
	if rt.cfg.Debug && decodes > 0 {
		emit(Event{Type: EventScore, Score: score, At: time.Now()})
	}
	rt.maybeEmitDetection(score)
}

func (rt *runtime) maybeEmitDetection(score float32) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if score < rt.cfg.Threshold || score == 0 {
		rt.consecutive = 0
		return
	}
	rt.consecutive++
	if rt.consecutive < rt.cfg.MinConsecutiveFrames {
		return
	}
	now := time.Now()
	if !rt.lastTrigger.IsZero() && now.Sub(rt.lastTrigger) < rt.cfg.Cooldown {
		return
	}
	rt.lastTrigger = now
	rt.consecutive = 0
	emit(Event{
		Type:        EventDetection,
		Keyword:     rt.cfg.PhraseID,
		Phrase:      rt.cfg.DisplayPhrase,
		Mode:        rt.cfg.DefaultMode,
		Probability: score,
		At:          now,
	})
}

func (rt *runtime) snapshotStats() (int64, int64) {
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
	if rt.detector != nil {
		_ = rt.detector.Close()
		rt.detector = nil
	}
}

type openWakeWordDetector struct {
	melspecSession   *ort.DynamicAdvancedSession
	embeddingSession *ort.DynamicAdvancedSession
	wakeSession      *ort.DynamicAdvancedSession

	rawBuffer     []float32
	rawRemainder  []float32
	accumulated   int
	melBuffer     []float32
	featureBuffer []float32
	predictions   []float32
}

func newOpenWakeWordDetector(cfg sidecarConfig) (*openWakeWordDetector, error) {
	for label, p := range map[string]string{
		"melspec":     cfg.MelspecModelPath,
		"embedding":   cfg.EmbeddingModelPath,
		"phrase":      cfg.WakeModelPath,
		"onnxruntime": cfg.OnnxRuntimePath,
	} {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("%s path is required", label)
		}
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("%s asset missing %s: %w", label, p, err)
		}
	}

	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy() //nolint:errcheck // best-effort native option cleanup
	_ = opts.SetIntraOpNumThreads(1)
	_ = opts.SetInterOpNumThreads(1)

	mel, err := ort.NewDynamicAdvancedSession(cfg.MelspecModelPath, []string{"input"}, []string{"output"}, opts)
	if err != nil {
		return nil, fmt.Errorf("melspec session: %w", err)
	}
	emb, err := ort.NewDynamicAdvancedSession(cfg.EmbeddingModelPath, []string{"input_1"}, []string{"conv2d_19"}, opts)
	if err != nil {
		_ = mel.Destroy()
		return nil, fmt.Errorf("embedding session: %w", err)
	}
	wake, err := ort.NewDynamicAdvancedSession(cfg.WakeModelPath, []string{"embeddings"}, []string{"score"}, opts)
	if err != nil {
		_ = emb.Destroy()
		_ = mel.Destroy()
		return nil, fmt.Errorf("wake session: %w", err)
	}

	d := &openWakeWordDetector{
		melspecSession:   mel,
		embeddingSession: emb,
		wakeSession:      wake,
		melBuffer:        make([]float32, 76*32),
		featureBuffer:    make([]float32, 16*96),
	}
	for i := range d.melBuffer {
		d.melBuffer[i] = 1
	}
	return d, nil
}

func (d *openWakeWordDetector) FeedPCM(pcm []byte) (int, float32, error) {
	if len(pcm) == 0 {
		return 0, 0, nil
	}
	if len(pcm)%audio.BytesPerSample != 0 {
		return 0, 0, fmt.Errorf("pcm len %d not S16-aligned", len(pcm))
	}
	samples := make([]float32, len(pcm)/audio.BytesPerSample)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*audio.BytesPerSample : i*audio.BytesPerSample+2]))) //nolint:gosec // S16LE sample bit pattern
	}
	return d.feedSamples(samples)
}

func (d *openWakeWordDetector) feedSamples(samples []float32) (int, float32, error) {
	if len(d.rawRemainder) > 0 {
		joined := make([]float32, 0, len(d.rawRemainder)+len(samples))
		joined = append(joined, d.rawRemainder...)
		joined = append(joined, samples...)
		samples = joined
		d.rawRemainder = nil
	}

	if d.accumulated+len(samples) >= wakeword.FrameSamples {
		remainder := (d.accumulated + len(samples)) % wakeword.FrameSamples
		if remainder != 0 {
			even := samples[:len(samples)-remainder]
			d.bufferRaw(even)
			d.accumulated += len(even)
			d.rawRemainder = append(d.rawRemainder[:0], samples[len(samples)-remainder:]...)
		} else {
			d.bufferRaw(samples)
			d.accumulated += len(samples)
		}
	} else {
		d.bufferRaw(samples)
		d.accumulated += len(samples)
	}

	if d.accumulated < wakeword.FrameSamples || d.accumulated%wakeword.FrameSamples != 0 {
		return 0, 0, nil
	}

	processed := d.accumulated
	if err := d.streamingMelspectrogram(processed); err != nil {
		return 0, 0, err
	}
	framesAdded := processed / wakeword.FrameSamples
	for i := framesAdded - 1; i >= 0; i-- {
		end := d.melFrameCount() - 8*i
		if i == 0 {
			end = d.melFrameCount()
		}
		start := end - 76
		if start < 0 || end > d.melFrameCount() {
			continue
		}
		embedding, err := d.embedding(d.melWindow(start, end))
		if err != nil {
			return 0, 0, err
		}
		d.appendFeature(embedding)
	}
	d.accumulated = 0
	d.trimBuffers()

	var peak float32
	for i := framesAdded - 1; i >= 0; i-- {
		start := d.featureFrameCount() - 16 - i
		if i == 0 {
			start = d.featureFrameCount() - 16
		}
		if start < 0 {
			continue
		}
		score, err := d.score(d.featureWindow(start, start+16))
		if err != nil {
			return 0, 0, err
		}
		if len(d.predictions) < 5 {
			score = 0
		}
		d.predictions = append(d.predictions, score)
		if len(d.predictions) > 30 {
			d.predictions = d.predictions[len(d.predictions)-30:]
		}
		if score > peak {
			peak = score
		}
	}
	return framesAdded, peak, nil
}

func (d *openWakeWordDetector) bufferRaw(samples []float32) {
	d.rawBuffer = append(d.rawBuffer, samples...)
	max := wakeword.SampleRate * 10
	if len(d.rawBuffer) > max {
		d.rawBuffer = append([]float32(nil), d.rawBuffer[len(d.rawBuffer)-max:]...)
	}
}

func (d *openWakeWordDetector) streamingMelspectrogram(nSamples int) error {
	window := nSamples + 160*3
	if window > len(d.rawBuffer) {
		window = len(d.rawBuffer)
	}
	if window < 400 {
		return fmt.Errorf("not enough audio for melspectrogram: %d samples", window)
	}
	spec, _, err := d.melspectrogram(d.rawBuffer[len(d.rawBuffer)-window:])
	if err != nil {
		return err
	}
	d.melBuffer = append(d.melBuffer, spec...)
	return nil
}

func (d *openWakeWordDetector) melspectrogram(samples []float32) ([]float32, int, error) {
	out, _, err := runFloatSession(d.melspecSession, []int64{1, int64(len(samples))}, samples)
	if err != nil {
		return nil, 0, err
	}
	for i := range out {
		out[i] = out[i]/10 + 2
	}
	return out, len(out) / 32, nil
}

func (d *openWakeWordDetector) embedding(window []float32) ([]float32, error) {
	out, _, err := runFloatSession(d.embeddingSession, []int64{1, 76, 32, 1}, window)
	if err != nil {
		return nil, err
	}
	if len(out) < 96 {
		return nil, fmt.Errorf("embedding output too short: %d", len(out))
	}
	return out[:96], nil
}

func (d *openWakeWordDetector) score(features []float32) (float32, error) {
	out, _, err := runFloatSession(d.wakeSession, []int64{1, 16, 96}, features)
	if err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, fmt.Errorf("wake model produced empty score")
	}
	return out[0], nil
}

func runFloatSession(session *ort.DynamicAdvancedSession, shape []int64, data []float32) ([]float32, []int64, error) {
	input, err := ort.NewTensor(ort.NewShape(shape...), data)
	if err != nil {
		return nil, nil, err
	}
	defer input.Destroy() //nolint:errcheck // best-effort native tensor cleanup
	outputs := []ort.Value{nil}
	if err := session.Run([]ort.Value{input}, outputs); err != nil {
		return nil, nil, err
	}
	defer outputs[0].Destroy() //nolint:errcheck // best-effort native tensor cleanup
	tensor, ok := outputs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, nil, fmt.Errorf("unexpected ONNX output type %T", outputs[0])
	}
	out := append([]float32(nil), tensor.GetData()...)
	outShape := append([]int64(nil), tensor.GetShape()...)
	return out, outShape, nil
}

func (d *openWakeWordDetector) melFrameCount() int { return len(d.melBuffer) / 32 }

func (d *openWakeWordDetector) featureFrameCount() int { return len(d.featureBuffer) / 96 }

func (d *openWakeWordDetector) melWindow(start, end int) []float32 {
	out := make([]float32, (end-start)*32)
	copy(out, d.melBuffer[start*32:end*32])
	return out
}

func (d *openWakeWordDetector) featureWindow(start, end int) []float32 {
	out := make([]float32, (end-start)*96)
	copy(out, d.featureBuffer[start*96:end*96])
	return out
}

func (d *openWakeWordDetector) appendFeature(feature []float32) {
	if len(feature) < 96 {
		return
	}
	d.featureBuffer = append(d.featureBuffer, feature[:96]...)
}

func (d *openWakeWordDetector) trimBuffers() {
	if frames := d.melFrameCount(); frames > 970 {
		d.melBuffer = append([]float32(nil), d.melBuffer[(frames-970)*32:]...)
	}
	if frames := d.featureFrameCount(); frames > 120 {
		d.featureBuffer = append([]float32(nil), d.featureBuffer[(frames-120)*96:]...)
	}
}

func (d *openWakeWordDetector) Close() error {
	var err error
	if d.wakeSession != nil {
		err = errors.Join(err, d.wakeSession.Destroy())
		d.wakeSession = nil
	}
	if d.embeddingSession != nil {
		err = errors.Join(err, d.embeddingSession.Destroy())
		d.embeddingSession = nil
	}
	if d.melspecSession != nil {
		err = errors.Join(err, d.melspecSession.Destroy())
		d.melspecSession = nil
	}
	return err
}

func pumpHeartbeats(ctx context.Context, rt *runtime) {
	// First heartbeat fires after 5 s so the desktop adapter can flip the
	// panel status from "Starting / waiting for audio" to "Active" (or to
	// the explicit "no audio reaching detector" error) without making the
	// user stare at a misleading green indicator for 30 s. Subsequent
	// heartbeats follow the steady-state 30 s cadence.
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

func pumpCommands(ctx context.Context) {
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
		if cmd.Type == CommandShutdown {
			emit(Event{Type: EventLog, Level: "info", Msg: "shutdown command received", At: time.Now()})
			_ = sendSelfSignal()
			return
		}
		emit(Event{Type: EventLog, Level: "warn", Msg: fmt.Sprintf("unknown command type %q", cmd.Type), At: time.Now()})
		if ctx.Err() != nil {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emit(Event{Type: EventLog, Level: "warn", Msg: fmt.Sprintf("stdin scanner: %v", err), At: time.Now()})
	}
}

type sidecarConfig struct {
	MelspecModelPath   string
	EmbeddingModelPath string
	WakeModelPath      string
	OnnxRuntimePath    string
	Threshold          float32
	DefaultMode        string
	DisplayPhrase      string
	PhraseID           string

	MinConsecutiveFrames int
	Cooldown             time.Duration

	AudioBackend  string
	AudioDeviceID string

	// Debug emits a "score" event for every decode window so the host can
	// surface the raw detector score in the user log feed. Off by default —
	// this fires ~12×/s per active model.
	Debug bool
}

// emitDeviceEvent — see speechkit-wakeword/main.go for rationale. Identical
// behaviour kept inline rather than shared so each sidecar stays a single
// self-contained binary (the IPC contract is the share point, not the impl).
func emitDeviceEvent(backend, requestedID string) {
	devices, err := audio.ListCaptureDevices(audio.Config{Backend: audio.Backend(backend)})
	if err != nil {
		emit(Event{Type: EventDevice, DeviceKind: "enumeration-failed", DeviceID: requestedID, Msg: fmt.Sprintf("audio device enumeration failed: %v", err), At: time.Now()})
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
		melspec       = flag.String("melspec-model", "", "openWakeWord melspectrogram ONNX model path")
		embedding     = flag.String("embedding-model", "", "openWakeWord embedding ONNX model path")
		wakeModel     = flag.String("wake-model", "", "trained wake phrase ONNX model path")
		onnxRuntime   = flag.String("onnxruntime", "onnxruntime.dll", "ONNX Runtime shared library path")
		phraseID      = flag.String("phrase-id", "", "stable catalog ID for the active phrase")
		phrase        = flag.String("phrase", "", "display name for the active phrase")
		defaultMode   = flag.String("default-mode", "voice_agent", "mode to trigger on detection")
		threshold     = flag.Float64("threshold", 0.35, "minimum openWakeWord score for a hit")
		minFrames     = flag.Int("min-consecutive-frames", 1, "consecutive 80ms windows over threshold before emitting")
		cooldownMs    = flag.Int("cooldown-ms", 1500, "minimum interval between detections")
		audioBackend  = flag.String("audio-backend", "windows-wasapi-malgo", "audio capture backend identifier")
		audioDeviceID = flag.String("audio-device-id", "", "audio capture device ID")
		debug         = flag.Bool("debug", false, "emit a score event per decode window for tuning")
		showVersion   = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *showVersion {
		fmt.Println(AppVersion)
		os.Exit(0)
	}
	if *threshold <= 0 || *threshold > 1 {
		*threshold = 0.35
	}
	return sidecarConfig{
		MelspecModelPath:     *melspec,
		EmbeddingModelPath:   *embedding,
		WakeModelPath:        *wakeModel,
		OnnxRuntimePath:      *onnxRuntime,
		PhraseID:             *phraseID,
		DisplayPhrase:        *phrase,
		DefaultMode:          *defaultMode,
		Threshold:            float32(*threshold),
		MinConsecutiveFrames: maxInt(*minFrames, 1),
		Cooldown:             time.Duration(maxInt(*cooldownMs, 1500)) * time.Millisecond,
		AudioBackend:         *audioBackend,
		AudioDeviceID:        *audioDeviceID,
		Debug:                *debug,
	}
}

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

type badConfigError struct{ err error }

func (e badConfigError) Error() string { return e.err.Error() }
func (e badConfigError) Unwrap() error { return e.err }

func isMissingAssetError(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "asset missing") || strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "path is required"))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
