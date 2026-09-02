//go:build !cgo

package wakeword

import "time"

const (
	defaultMinConsecutiveFrames = 1
	defaultCooldown             = 1500 * time.Millisecond
)

// Pipeline is the no-cgo placeholder for the cgo build's Pipeline. It keeps
// the exported surface identical so hosts compile with CGO_ENABLED=0 and
// discover the missing native detector at runtime through ErrCgoRequired.
type Pipeline struct {
	cfg Config
}

// NewPipeline returns ErrCgoRequired in the no-cgo build.
func NewPipeline(_ *Detector, _ Sink, _ Config) (*Pipeline, error) {
	return nil, ErrCgoRequired
}

// FeedPCM returns ErrCgoRequired in the no-cgo build.
func (*Pipeline) FeedPCM([]byte) (decodes int, peakProb float32, err error) {
	return 0, 0, ErrCgoRequired
}

// Reset is a no-op on the stub.
func (*Pipeline) Reset() {}

// Pause is a no-op on the stub.
func (*Pipeline) Pause() {}

// Resume is a no-op on the stub.
func (*Pipeline) Resume() {}

// Paused always reports false on the stub.
func (*Pipeline) Paused() bool { return false }

// Close is a no-op on the stub.
func (*Pipeline) Close() error { return nil }

// Config returns the resolved pipeline config.
func (p *Pipeline) Config() Config {
	if p == nil {
		return normalizeConfig(Config{})
	}
	return p.cfg
}

func normalizeConfig(cfg Config) Config {
	if cfg.MinConsecutiveFrames <= 0 {
		cfg.MinConsecutiveFrames = defaultMinConsecutiveFrames
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = defaultCooldown
	}
	return cfg
}
