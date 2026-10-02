package wakeword

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// benchEngine is an engine whose stream never buffers audio: every accepted
// frame makes it ready to decode the scripted keywords once.
type benchEngine struct{ script []string }

type benchStream struct {
	script []string
	queued []string
}

func (e *benchEngine) NewStream() (EngineStream, error) { return &benchStream{script: e.script}, nil }
func (e *benchEngine) Close() error                     { return nil }

func (s *benchStream) AcceptWaveform([]float32) { s.queued = s.script }
func (s *benchStream) IsReady() bool            { return len(s.queued) > 0 }
func (s *benchStream) Decode() string {
	kw := s.queued[0]
	s.queued = s.queued[1:]
	return kw
}
func (s *benchStream) Reset()       { s.queued = nil }
func (s *benchStream) Close() error { return nil }

func newBenchPipeline(b *testing.B, name string, script []string, cfg Config) *Pipeline {
	b.Helper()
	engineName := "bench-" + name
	if err := RegisterEngine(engineName, func(DetectorConfig) (Engine, error) {
		return &benchEngine{script: script}, nil
	}); err != nil {
		b.Fatalf("RegisterEngine: %v", err)
	}
	b.Cleanup(func() { unregisterEngineForTest(engineName) })

	dir := b.TempDir()
	dc := DetectorConfig{Engine: engineName, Tokens: testTokens, Threshold: 0.4}
	for _, asset := range []struct {
		field *string
		name  string
	}{{&dc.Encoder, "encoder.onnx"}, {&dc.Decoder, "decoder.onnx"}, {&dc.Joiner, "joiner.onnx"}} {
		p := filepath.Join(dir, asset.name)
		if err := os.WriteFile(p, []byte("stub"), 0o600); err != nil {
			b.Fatalf("write %s: %v", asset.name, err)
		}
		*asset.field = p
	}
	dc.KeywordsFile = filepath.Join(dir, "keywords.txt")
	if err := os.WriteFile(dc.KeywordsFile, []byte("▁HE Y ▁KOM B IF Y @hey_kombify\n"), 0o600); err != nil {
		b.Fatalf("write keywords: %v", err)
	}
	det, err := NewDetector(dc)
	if err != nil {
		b.Fatalf("NewDetector: %v", err)
	}
	b.Cleanup(func() { _ = det.Close() })

	pipe, err := NewPipeline(det, SinkFunc(func(DetectionEvent) {}), cfg)
	if err != nil {
		b.Fatalf("NewPipeline: %v", err)
	}
	b.Cleanup(func() { _ = pipe.Close() })
	return pipe
}

// BenchmarkPCMToFloat32 measures the S16LE to float32 conversion applied to
// every frame before it reaches the engine.
func BenchmarkPCMToFloat32(b *testing.B) {
	frame := make([]byte, FrameBytes)
	for i := range frame {
		frame[i] = byte(i)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	for b.Loop() {
		_ = pcmToFloat32(frame)
	}
}

// BenchmarkPipelineFeedPCM measures one frame through the pipeline with a fake
// engine: silence (decode only), and keywords that are debounced then held by
// the cooldown, which is the steady state while a wake phrase is repeated.
func BenchmarkPipelineFeedPCM(b *testing.B) {
	frame := make([]byte, FrameBytes)
	cases := []struct {
		name   string
		script []string
		cfg    Config
	}{
		{"silence", []string{""}, Config{}},
		{"keyword-cooldown", []string{"", "hey_kombify"}, Config{Cooldown: time.Hour}},
		{"keyword-debounce", []string{"hey_kombify"}, Config{Cooldown: time.Hour, MinConsecutiveFrames: 3}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			pipe := newBenchPipeline(b, tc.name, tc.script, tc.cfg)
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := pipe.FeedPCM(frame); err != nil {
					b.Fatalf("FeedPCM: %v", err)
				}
			}
		})
	}
}
