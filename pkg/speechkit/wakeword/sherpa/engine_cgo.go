//go:build cgo

package sherpa

import (
	"errors"
	"fmt"
	"path/filepath"

	sherpaonnx "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

func init() {
	if err := wakeword.RegisterEngine(EngineName, newEngine); err != nil {
		panic(err)
	}
}

// NewDetector loads the sherpa-onnx KWS model through wakeword.NewDetector
// with the sherpa-onnx engine selected.
var NewDetector = func(cfg DetectorConfig) (*Detector, error) {
	cfg.Engine = EngineName
	return wakeword.NewDetector(cfg)
}

// engine owns the sherpa-onnx KeywordSpotter for the lifetime of a Detector.
type engine struct {
	spotter *sherpaonnx.KeywordSpotter
}

func newEngine(cfg wakeword.DetectorConfig) (wakeword.Engine, error) {
	sc := sherpaonnx.KeywordSpotterConfig{}
	sc.ModelConfig.Transducer.Encoder = cfg.Encoder
	sc.ModelConfig.Transducer.Decoder = cfg.Decoder
	sc.ModelConfig.Transducer.Joiner = cfg.Joiner
	sc.ModelConfig.Tokens = cfg.Tokens
	sc.ModelConfig.NumThreads = numThreads(cfg.NumThreads)
	if cfg.Debug {
		sc.ModelConfig.Debug = 1
	}
	sc.KeywordsFile = cfg.KeywordsFile
	if cfg.Threshold > 0 && cfg.Threshold <= 1 {
		sc.KeywordsThreshold = cfg.Threshold
	}

	spotter := sherpaonnx.NewKeywordSpotter(&sc)
	if spotter == nil {
		return nil, fmt.Errorf("sherpa: NewKeywordSpotter returned nil (check model assets at %s)", filepath.Dir(cfg.Encoder))
	}
	return &engine{spotter: spotter}, nil
}

func (e *engine) NewStream() (wakeword.EngineStream, error) {
	if e.spotter == nil {
		return nil, errors.New("sherpa: engine closed")
	}
	s := sherpaonnx.NewKeywordStream(e.spotter)
	if s == nil {
		return nil, errors.New("sherpa: NewKeywordStream returned nil")
	}
	return &stream{spotter: e.spotter, stream: s}, nil
}

func (e *engine) Close() error {
	if e.spotter != nil {
		sherpaonnx.DeleteKeywordSpotter(e.spotter)
		e.spotter = nil
	}
	return nil
}

// stream is one sherpa-onnx OnlineStream bound to its KeywordSpotter.
type stream struct {
	spotter *sherpaonnx.KeywordSpotter
	stream  *sherpaonnx.OnlineStream
}

func (s *stream) AcceptWaveform(samples []float32) {
	s.stream.AcceptWaveform(wakeword.SampleRate, samples)
}

func (s *stream) IsReady() bool { return s.spotter.IsReady(s.stream) }

func (s *stream) Decode() string {
	s.spotter.Decode(s.stream)
	return s.spotter.GetResult(s.stream).Keyword
}

func (s *stream) Reset() { s.spotter.Reset(s.stream) }

func (s *stream) Close() error {
	if s.stream != nil {
		sherpaonnx.DeleteOnlineStream(s.stream)
		s.stream = nil
	}
	return nil
}

// numThreads bounds the KWS engine to 1..4 threads: the model is small
// (~3 M parameters) and never benefits from more on desktop hardware.
func numThreads(requested int) int {
	if requested <= 0 {
		return 1
	}
	if requested > 4 {
		return 4
	}
	return requested
}
