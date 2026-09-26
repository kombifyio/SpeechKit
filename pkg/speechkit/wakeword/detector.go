package wakeword

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// DetectorConfig bundles the file-system inputs of a keyword-spotting model.
// All paths must be absolute; NewDetector never resolves them against a
// model directory so host wiring stays explicit.
type DetectorConfig struct {
	Encoder      string   // encoder ONNX
	Decoder      string   // decoder ONNX
	Joiner       string   // joiner ONNX
	Tokens       string   // tokens.txt (BPE token table)
	KeywordsFile string   // BPE-tokenised keywords file; takes precedence over Keywords
	Keywords     []string // raw phrases, BPE-encoded by NewDetector via EncodeKeywords

	// NumThreads bounds the engine's CPU parallelism; zero lets the engine
	// choose.
	NumThreads int

	// Threshold is the keyword detection threshold in (0, 1]; zero keeps the
	// engine default.
	Threshold float32

	// Debug enables the engine's verbose native logging.
	Debug bool

	// Engine selects a registered engine by name (see RegisterEngine). Empty
	// uses the first registered engine.
	Engine string
}

// Detector is a loaded keyword-spotting model, ready to back Pipeline
// streams. It is engine-neutral: NewDetector resolves the registered Engine
// and the Detector only owns its lifetime. The struct stays comparable (no
// slices or maps) so it remains a drop-in for the previous no-cgo surface.
type Detector struct {
	engine    Engine
	threshold float32
}

// NewDetector validates cfg, stages inline Keywords as a BPE-tokenised
// keywords file, and loads the model through the selected registered Engine.
// It returns ErrEngineUnavailable when no engine is registered: import
// pkg/speechkit/wakeword/sherpa (or register your own engine) first.
func NewDetector(cfg DetectorConfig) (*Detector, error) {
	factory, err := lookupEngine(cfg.Engine)
	if err != nil {
		return nil, err
	}
	if cfg.Encoder == "" || cfg.Decoder == "" || cfg.Joiner == "" || cfg.Tokens == "" {
		return nil, errors.New("wakeword: encoder/decoder/joiner/tokens paths all required")
	}
	for _, p := range []string{cfg.Encoder, cfg.Decoder, cfg.Joiner, cfg.Tokens} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("wakeword: model asset missing %s: %w", p, err)
		}
	}
	kwFile, err := resolveKeywordsFile(cfg)
	if err != nil {
		return nil, err
	}
	cfg.KeywordsFile = kwFile

	engine, err := factory(cfg)
	if err != nil {
		return nil, err
	}
	if engine == nil {
		return nil, errors.New("wakeword: engine returned no model")
	}
	return &Detector{engine: engine, threshold: cfg.Threshold}, nil
}

// resolveKeywordsFile returns the BPE-tokenised keywords file the engine
// should load: cfg.KeywordsFile when set, else inline cfg.Keywords encoded
// into a temp file. Either way the file is validated, because a raw-text
// keywords file is the most common silent wakeword misconfiguration.
func resolveKeywordsFile(cfg DetectorConfig) (string, error) {
	kwFile := cfg.KeywordsFile
	if kwFile == "" && len(cfg.Keywords) > 0 {
		// Inline keywords are raw phrases; BPE-encode them so the engine can
		// match them (raw text silently never matches). See keywords.go.
		encoded, err := EncodeKeywords(cfg.Tokens, cfg.Keywords)
		if err != nil {
			return "", err
		}
		tmp, err := os.CreateTemp("", "speechkit-wakeword-keywords-*.txt")
		if err != nil {
			return "", fmt.Errorf("wakeword: stage inline keywords: %w", err)
		}
		if _, err := tmp.WriteString(strings.Join(encoded, "\n") + "\n"); err != nil {
			_ = tmp.Close()
			return "", fmt.Errorf("wakeword: write keywords tmp file: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return "", fmt.Errorf("wakeword: close keywords tmp file: %w", err)
		}
		kwFile = tmp.Name()
	}
	if kwFile == "" {
		return "", errors.New("wakeword: KeywordsFile or Keywords must be set")
	}
	if _, err := os.Stat(kwFile); err != nil {
		return "", fmt.Errorf("wakeword: keywords file missing %s: %w", kwFile, err)
	}
	if err := ValidateKeywordsFile(kwFile); err != nil {
		return "", err
	}
	return kwFile, nil
}

// Threshold returns the configured keyword detection threshold (0 if unset).
func (d *Detector) Threshold() float32 {
	if d == nil {
		return 0
	}
	return d.threshold
}

// Close releases the engine's model. Safe to call more than once.
func (d *Detector) Close() error {
	if d == nil || d.engine == nil {
		return nil
	}
	err := d.engine.Close()
	d.engine = nil
	return err
}
