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

// DetectorConfig bundles the file-system inputs the sherpa-onnx KWS engine
// needs. The Windows reference adapter resolves these from Config.ModelDir
// against a layout produced by scripts/prepare-wakeword-model.ps1.
//
// All paths must be absolute. Empty Encoder/Decoder/Joiner/Tokens fields
// fail with a clear error from NewDetector — they are not auto-resolved
// inside this package so adapter wiring stays explicit.
type DetectorConfig struct {
	Encoder      string   // encoder ONNX (zipformer KWS encoder-…onnx)
	Decoder      string   // decoder ONNX
	Joiner       string   // joiner ONNX
	Tokens       string   // tokens.txt (BPE token table)
	KeywordsFile string   // BPE-tokenised keywords, sherpa-onnx format
	Keywords     []string // alternative to KeywordsFile (joined with \n)

	// NumThreads bounds CPU parallelism. Zero defaults to 1 — the KWS model
	// is intentionally small (~3 M parameters) and never benefits from more
	// than a couple of threads on desktop hardware.
	NumThreads int

	// Threshold mirrors Config.Threshold; below 0 or above 1 falls back to
	// the sherpa-onnx default (~0.25).
	Threshold float32
}

// Detector owns the sherpa-onnx KeywordSpotter for the lifetime of the
// process. Construction loads the model graph; Close releases it. Callers
// do not call Detector directly — the Pipeline owns one Detector and the
// corresponding sherpa stream, and exposes FeedPCM / Close to the audio
// adapter.
type Detector struct {
	spotter *sherpa.KeywordSpotter
	cfg     DetectorConfig
}

// NewDetector loads the sherpa-onnx KWS model and prepares it for
// streaming inference. Returns an error if any required file is missing or
// the native library refuses to construct the spotter.
//
// The caller is responsible for Close() — typically wired into the desktop
// cleanup stack so the native handles are released on shutdown.
func NewDetector(cfg DetectorConfig) (*Detector, error) {
	if cfg.Encoder == "" || cfg.Decoder == "" || cfg.Joiner == "" || cfg.Tokens == "" {
		return nil, errors.New("wakeword: encoder/decoder/joiner/tokens paths all required")
	}
	for _, p := range []string{cfg.Encoder, cfg.Decoder, cfg.Joiner, cfg.Tokens} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("wakeword: model asset missing %s: %w", p, err)
		}
	}

	// sherpa-onnx accepts keywords either via a path or by writing them
	// into a temp file when callers pass them inline. Materialise inline
	// keywords here so the rest of the package can treat both the same.
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
	sc.ModelConfig.Debug = 0
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

// Close releases the underlying KeywordSpotter. Safe to call multiple
// times — subsequent calls are no-ops.
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
