//go:build cgo

package wakeword

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// DetectorConfig bundles file-system inputs for the sherpa-onnx KWS engine.
type DetectorConfig struct {
	Encoder      string
	Decoder      string
	Joiner       string
	Tokens       string
	KeywordsFile string
	Keywords     []string
	NumThreads   int
	Threshold    float32
	Debug        bool
}

// Detector owns the sherpa-onnx KeywordSpotter.
type Detector struct {
	spotter *sherpa.KeywordSpotter
	cfg     DetectorConfig
}

// NewDetector loads the sherpa-onnx KWS model.
func NewDetector(cfg DetectorConfig) (*Detector, error) {
	if cfg.Encoder == "" || cfg.Decoder == "" || cfg.Joiner == "" || cfg.Tokens == "" {
		return nil, errors.New("wakeword: encoder/decoder/joiner/tokens paths all required")
	}
	for _, p := range []string{cfg.Encoder, cfg.Decoder, cfg.Joiner, cfg.Tokens} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("wakeword: model asset missing %s: %w", p, err)
		}
	}

	kwFile := cfg.KeywordsFile
	if kwFile == "" && len(cfg.Keywords) > 0 {
		tmp, err := os.CreateTemp("", "speechkit-wakeword-keywords-*.txt")
		if err != nil {
			return nil, fmt.Errorf("wakeword: stage inline keywords: %w", err)
		}
		if _, err := tmp.WriteString(strings.Join(cfg.Keywords, "\n") + "\n"); err != nil {
			_ = tmp.Close()
			return nil, fmt.Errorf("wakeword: write keywords tmp file: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return nil, fmt.Errorf("wakeword: close keywords tmp file: %w", err)
		}
		kwFile = tmp.Name()
	}
	if kwFile == "" {
		return nil, errors.New("wakeword: KeywordsFile or Keywords must be set")
	}
	if _, err := os.Stat(kwFile); err != nil {
		return nil, fmt.Errorf("wakeword: keywords file missing %s: %w", kwFile, err)
	}

	sc := sherpa.KeywordSpotterConfig{}
	sc.ModelConfig.Transducer.Encoder = cfg.Encoder
	sc.ModelConfig.Transducer.Decoder = cfg.Decoder
	sc.ModelConfig.Transducer.Joiner = cfg.Joiner
	sc.ModelConfig.Tokens = cfg.Tokens
	sc.ModelConfig.NumThreads = numThreads(cfg.NumThreads)
	if cfg.Debug {
		sc.ModelConfig.Debug = 1
	}
	sc.KeywordsFile = kwFile
	if cfg.Threshold > 0 && cfg.Threshold <= 1 {
		sc.KeywordsThreshold = cfg.Threshold
	}

	spotter := sherpa.NewKeywordSpotter(&sc)
	if spotter == nil {
		return nil, fmt.Errorf("wakeword: sherpa NewKeywordSpotter returned nil (check model assets at %s)", filepath.Dir(cfg.Encoder))
	}
	return &Detector{spotter: spotter, cfg: cfg}, nil
}

// Close releases the underlying KeywordSpotter.
func (d *Detector) Close() error {
	if d == nil || d.spotter == nil {
		return nil
	}
	sherpa.DeleteKeywordSpotter(d.spotter)
	d.spotter = nil
	return nil
}

func numThreads(requested int) int {
	if requested <= 0 {
		return 1
	}
	if requested > 4 {
		return 4
	}
	return requested
}
