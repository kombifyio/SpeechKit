package wakeword

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEngine is a scripted Engine: the test queues keywords on its stream and
// each Decode step pops one, so the root pipeline's debounce, cooldown, pause
// and probability logic is exercised without any native model.
type fakeEngine struct {
	loaded bool
	cfg    DetectorConfig
	closed int
	stream *fakeStream
}

type fakeStream struct {
	pending  []string
	accepted []float32
	resets   int
	closed   int
}

func (e *fakeEngine) NewStream() (EngineStream, error) {
	e.stream = &fakeStream{}
	return e.stream, nil
}

func (e *fakeEngine) Close() error {
	e.closed++
	return nil
}

func (s *fakeStream) AcceptWaveform(samples []float32) { s.accepted = append(s.accepted, samples...) }
func (s *fakeStream) IsReady() bool                    { return len(s.pending) > 0 }
func (s *fakeStream) Decode() string {
	kw := s.pending[0]
	s.pending = s.pending[1:]
	return kw
}
func (s *fakeStream) Reset()       { s.resets++ }
func (s *fakeStream) Close() error { s.closed++; return nil }

// registerFakeEngine registers a fake engine under a test-unique name and
// unregisters it when the test ends.
func registerFakeEngine(t *testing.T) (string, *fakeEngine) {
	t.Helper()
	name := "fake-" + strings.ToLower(t.Name())
	eng := &fakeEngine{}
	err := RegisterEngine(name, func(cfg DetectorConfig) (Engine, error) {
		eng.loaded = true
		eng.cfg = cfg
		return eng, nil
	})
	if err != nil {
		t.Fatalf("RegisterEngine: %v", err)
	}
	t.Cleanup(func() { unregisterEngineForTest(name) })
	return name, eng
}

// modelAssets writes placeholder model files and returns a DetectorConfig
// pointing at them and at the shared testdata token table.
func modelAssets(t *testing.T) DetectorConfig {
	t.Helper()
	dir := t.TempDir()
	cfg := DetectorConfig{Tokens: testTokens}
	for _, asset := range []struct {
		field *string
		name  string
	}{
		{&cfg.Encoder, "encoder.onnx"},
		{&cfg.Decoder, "decoder.onnx"},
		{&cfg.Joiner, "joiner.onnx"},
	} {
		p := filepath.Join(dir, asset.name)
		if err := os.WriteFile(p, []byte("stub"), 0o600); err != nil {
			t.Fatalf("write %s: %v", asset.name, err)
		}
		*asset.field = p
	}
	return cfg
}

// newFakeDetector loads a Detector over a fresh fake engine.
func newFakeDetector(t *testing.T, threshold float32) (*Detector, *fakeEngine) {
	t.Helper()
	name, eng := registerFakeEngine(t)
	cfg := modelAssets(t)
	cfg.Engine = name
	cfg.Threshold = threshold
	cfg.KeywordsFile = filepath.Join(filepath.Dir(cfg.Encoder), "keywords.txt")
	if err := os.WriteFile(cfg.KeywordsFile, []byte("▁HE Y ▁KOM B IF Y @hey_kombify\n"), 0o600); err != nil {
		t.Fatalf("write keywords: %v", err)
	}
	det, err := NewDetector(cfg)
	if err != nil {
		t.Fatalf("NewDetector: %v", err)
	}
	t.Cleanup(func() { _ = det.Close() })
	return det, eng
}

func discard() Sink { return SinkFunc(func(DetectionEvent) {}) }

func TestNewDetectorRequiresRegisteredEngine(t *testing.T) {
	if _, err := NewDetector(DetectorConfig{}); !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("NewDetector without engines err = %v, want ErrEngineUnavailable", err)
	}
	registerFakeEngine(t)
	if _, err := NewDetector(DetectorConfig{Engine: "does-not-exist"}); !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("NewDetector with unknown engine err = %v, want ErrEngineUnavailable", err)
	}
}

func TestRegisterEngineRejectsInvalidRegistrations(t *testing.T) {
	name, _ := registerFakeEngine(t)
	if err := RegisterEngine("", func(DetectorConfig) (Engine, error) { return nil, nil }); err == nil {
		t.Fatal("RegisterEngine accepted an empty name")
	}
	if err := RegisterEngine("nil-factory", nil); err == nil {
		t.Fatal("RegisterEngine accepted a nil factory")
	}
	if err := RegisterEngine(name, func(DetectorConfig) (Engine, error) { return nil, nil }); err == nil {
		t.Fatal("RegisterEngine accepted a duplicate name")
	}
}

func TestNewDetectorValidatesInputsBeforeLoadingEngine(t *testing.T) {
	name, eng := registerFakeEngine(t)

	_, err := NewDetector(DetectorConfig{Engine: name})
	if err == nil || !strings.Contains(err.Error(), "paths all required") {
		t.Fatalf("NewDetector without paths err = %v, want path validation error", err)
	}

	cfg := modelAssets(t)
	cfg.Engine = name
	_, err = NewDetector(cfg)
	if err == nil || !strings.Contains(err.Error(), "KeywordsFile or Keywords") {
		t.Fatalf("NewDetector without keywords err = %v, want keywords error", err)
	}

	cfg.KeywordsFile = filepath.Join(filepath.Dir(cfg.Encoder), "raw.txt")
	if err := os.WriteFile(cfg.KeywordsFile, []byte("hey kombify\n"), 0o600); err != nil {
		t.Fatalf("write raw keywords: %v", err)
	}
	_, err = NewDetector(cfg)
	if err == nil || !strings.Contains(err.Error(), "raw text") {
		t.Fatalf("NewDetector with raw-text keywords err = %v, want validation error", err)
	}
	if eng.loaded {
		t.Fatal("engine factory ran although the inputs were invalid")
	}
}

func TestNewDetectorEncodesInlineKeywordsForEngine(t *testing.T) {
	name, eng := registerFakeEngine(t)
	cfg := modelAssets(t)
	cfg.Engine = name
	cfg.Keywords = []string{"hey kombify @hey_kombify"}

	det, err := NewDetector(cfg)
	if err != nil {
		t.Fatalf("NewDetector: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(eng.cfg.KeywordsFile) })

	data, err := os.ReadFile(eng.cfg.KeywordsFile)
	if err != nil {
		t.Fatalf("engine received no readable keywords file: %v", err)
	}
	if got, want := string(data), "▁HE Y ▁KOM B IF Y @hey_kombify\n"; got != want {
		t.Fatalf("staged keywords = %q, want %q", got, want)
	}
	if eng.cfg.Engine != name || eng.cfg.Tokens != testTokens {
		t.Fatalf("engine cfg = %+v, want engine %q with the tokens table", eng.cfg, name)
	}

	if err := det.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := det.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if eng.closed != 1 {
		t.Fatalf("engine closed %d times, want exactly once", eng.closed)
	}
}

func TestPipelineEmitsThresholdBoundAndHonoursCooldown(t *testing.T) {
	det, eng := newFakeDetector(t, 0.4)
	var events []DetectionEvent
	pipe, err := NewPipeline(det, SinkFunc(func(ev DetectionEvent) { events = append(events, ev) }), Config{
		Phrase:      "Hey Kombify",
		DefaultMode: "assist",
		Cooldown:    time.Second,
	})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer pipe.Close()
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	pipe.now = func() time.Time { return now }
	frame := make([]byte, FrameBytes)

	eng.stream.pending = []string{"", " hey_kombify "}
	decodes, peak, err := pipe.FeedPCM(frame)
	if err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	if decodes != 2 || peak != 0.4 {
		t.Fatalf("FeedPCM = (%d, %v), want (2, 0.4)", decodes, peak)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	want := DetectionEvent{Phrase: "Hey Kombify", Keyword: "hey_kombify", Mode: "assist", Probability: 0.4, At: now}
	if events[0] != want {
		t.Fatalf("event = %+v, want %+v", events[0], want)
	}
	if eng.stream.resets != 1 {
		t.Fatalf("stream resets = %d, want 1 after a detection", eng.stream.resets)
	}

	// Same keyword inside the cooldown window is swallowed.
	eng.stream.pending = []string{"hey_kombify"}
	decodes, peak, err = pipe.FeedPCM(frame)
	if err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	if decodes != 1 || peak != 0 || len(events) != 1 {
		t.Fatalf("inside cooldown: decodes=%d peak=%v events=%d, want 1/0/1", decodes, peak, len(events))
	}

	now = now.Add(2 * time.Second)
	eng.stream.pending = []string{"hey_kombify"}
	if _, _, err := pipe.FeedPCM(frame); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	if len(events) != 2 || !events[1].At.Equal(now) {
		t.Fatalf("after cooldown: events = %+v, want a second detection at %v", events, now)
	}
}

func TestPipelineProbabilityFallsBackToDetectorThenEngineDefault(t *testing.T) {
	det, eng := newFakeDetector(t, 0)
	var probs []float32
	sink := SinkFunc(func(ev DetectionEvent) { probs = append(probs, ev.Probability) })

	pipe, err := NewPipeline(det, sink, Config{Threshold: 0.6, Cooldown: time.Nanosecond})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	eng.stream.pending = []string{"kw"}
	if _, _, err := pipe.FeedPCM(make([]byte, FrameBytes)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	_ = pipe.Close()

	pipe, err = NewPipeline(det, sink, Config{Cooldown: time.Nanosecond})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	eng.stream.pending = []string{"kw"}
	if _, _, err := pipe.FeedPCM(make([]byte, FrameBytes)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	_ = pipe.Close()

	if len(probs) != 2 || probs[0] != 0.6 || probs[1] != defaultKeywordThreshold {
		t.Fatalf("probabilities = %v, want [0.6 %v]", probs, defaultKeywordThreshold)
	}
}

func TestPipelineMinConsecutiveFramesDebounce(t *testing.T) {
	det, eng := newFakeDetector(t, 0)
	count := 0
	pipe, err := NewPipeline(det, SinkFunc(func(DetectionEvent) { count++ }), Config{MinConsecutiveFrames: 2})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer pipe.Close()
	frame := make([]byte, FrameBytes)

	eng.stream.pending = []string{"kw", ""}
	if _, _, err := pipe.FeedPCM(frame); err != nil || count != 0 {
		t.Fatalf("blank decode must reset the streak: err=%v count=%d", err, count)
	}
	eng.stream.pending = []string{"kw", "other"}
	if _, _, err := pipe.FeedPCM(frame); err != nil || count != 0 {
		t.Fatalf("different keyword must reset the streak: err=%v count=%d", err, count)
	}
	// The streak survives across FeedPCM calls: the second consecutive
	// "other" arrives in the next frame and completes the pair.
	eng.stream.pending = []string{"other"}
	if _, _, err := pipe.FeedPCM(frame); err != nil || count != 1 {
		t.Fatalf("second consecutive hit across frames must fire once: err=%v count=%d", err, count)
	}
	eng.stream.pending = []string{"kw", "kw"}
	if _, _, err := pipe.FeedPCM(frame); err != nil || count != 2 {
		t.Fatalf("two consecutive hits in one frame must fire once: err=%v count=%d", err, count)
	}
}

func TestPipelinePauseDropsAudioAndResumeResets(t *testing.T) {
	det, eng := newFakeDetector(t, 0)
	pipe, err := NewPipeline(det, discard(), Config{})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	defer pipe.Close()

	frame := make([]byte, FrameBytes)
	binary.LittleEndian.PutUint16(frame[0:], 16384)
	binary.LittleEndian.PutUint16(frame[2:], 0x8000)

	pipe.Pause()
	if !pipe.Paused() {
		t.Fatal("Paused() = false after Pause")
	}
	eng.stream.pending = []string{"kw"}
	decodes, _, err := pipe.FeedPCM(frame)
	if err != nil || decodes != 0 || len(eng.stream.accepted) != 0 || len(eng.stream.pending) != 1 {
		t.Fatalf("paused pipeline fed the engine: err=%v decodes=%d accepted=%d pending=%d", err, decodes, len(eng.stream.accepted), len(eng.stream.pending))
	}

	pipe.Resume()
	if pipe.Paused() || eng.stream.resets != 1 {
		t.Fatalf("Resume: paused=%v resets=%d, want false/1", pipe.Paused(), eng.stream.resets)
	}
	decodes, _, err = pipe.FeedPCM(frame)
	if err != nil || decodes != 1 {
		t.Fatalf("resumed FeedPCM = (%d, %v), want 1 decode", decodes, err)
	}
	if len(eng.stream.accepted) != FrameSamples || eng.stream.accepted[0] != 0.5 || eng.stream.accepted[1] != -1 {
		t.Fatalf("PCM conversion: got %d samples, first two %v %v; want %d samples starting 0.5 -1", len(eng.stream.accepted), eng.stream.accepted[0], eng.stream.accepted[1], FrameSamples)
	}
}

func TestPipelineRejectsBadInputAndFailsAfterClose(t *testing.T) {
	if _, err := NewPipeline(nil, discard(), Config{}); err == nil {
		t.Fatal("NewPipeline accepted a nil detector")
	}
	det, eng := newFakeDetector(t, 0)
	if _, err := NewPipeline(det, nil, Config{}); err == nil {
		t.Fatal("NewPipeline accepted a nil sink")
	}
	pipe, err := NewPipeline(det, discard(), Config{})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	if cfg := pipe.Config(); cfg.MinConsecutiveFrames != 1 || cfg.Cooldown != defaultCooldown {
		t.Fatalf("Config() = %+v, want defaults applied", cfg)
	}
	if _, _, err := pipe.FeedPCM([]byte{1}); err == nil {
		t.Fatal("FeedPCM accepted unaligned PCM")
	}
	if _, _, err := pipe.FeedPCM(nil); err != nil {
		t.Fatalf("FeedPCM(nil) = %v, want nil", err)
	}

	if err := pipe.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := pipe.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if eng.stream.closed != 1 {
		t.Fatalf("stream closed %d times, want exactly once", eng.stream.closed)
	}
	if _, _, err := pipe.FeedPCM(make([]byte, FrameBytes)); err == nil {
		t.Fatal("FeedPCM succeeded on a closed pipeline")
	}

	if err := det.Close(); err != nil {
		t.Fatalf("Detector.Close: %v", err)
	}
	if _, err := NewPipeline(det, discard(), Config{}); err == nil {
		t.Fatal("NewPipeline succeeded on a closed detector")
	}
}
