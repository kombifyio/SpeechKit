package wakeword

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMinConsecutiveFrames = 1
	defaultCooldown             = 1500 * time.Millisecond
	// defaultKeywordThreshold mirrors sherpa-onnx's built-in KWS threshold,
	// used as the reported Probability lower bound when neither the pipeline
	// nor the detector configures one. Engines expose no exact per-detection
	// score.
	defaultKeywordThreshold = 0.25
)

// Pipeline streams PCM audio through a Detector's engine and emits a
// DetectionEvent via Sink whenever the engine spots a keyword
// Config.MinConsecutiveFrames decodes in a row, outside the current cooldown
// window. It is pure Go: PCM conversion, debounce, cooldown, pause and the
// reported probability live here, so every Engine behaves the same.
//
// Pipeline is safe for concurrent use. One Pipeline backs one audio source.
type Pipeline struct {
	detector *Detector
	sink     Sink
	cfg      Config
	now      func() time.Time

	paused atomic.Bool

	mu              sync.Mutex
	stream          EngineStream
	lastTrigger     map[string]time.Time
	consecutiveHits map[string]int
}

// NewPipeline wires a Detector and Sink together. Invalid Config fields are
// coerced to defaults so hosts do not mirror the normalisation.
func NewPipeline(detector *Detector, sink Sink, cfg Config) (*Pipeline, error) {
	if detector == nil {
		return nil, errors.New("wakeword: nil detector")
	}
	if sink == nil {
		return nil, errors.New("wakeword: nil sink")
	}
	if detector.engine == nil {
		return nil, errors.New("wakeword: detector closed")
	}
	cfg = normalizeConfig(cfg)
	stream, err := detector.engine.NewStream()
	if err != nil {
		return nil, fmt.Errorf("wakeword: open engine stream: %w", err)
	}
	if stream == nil {
		return nil, errors.New("wakeword: engine returned no stream")
	}
	return &Pipeline{
		detector:        detector,
		sink:            sink,
		cfg:             cfg,
		now:             time.Now,
		stream:          stream,
		lastTrigger:     make(map[string]time.Time),
		consecutiveHits: make(map[string]int),
	}, nil
}

// FeedPCM ingests raw S16LE mono PCM at SampleRate, drains every ready decode
// window and returns the number of decode steps plus the probability lower
// bound of the last detection emitted during this call (0 if none).
func (p *Pipeline) FeedPCM(pcm []byte) (decodes int, peakProb float32, err error) {
	if len(pcm) == 0 {
		return 0, 0, nil
	}
	if len(pcm)%BytesPerSample != 0 {
		return 0, 0, fmt.Errorf("wakeword: pcm len %d not S16-aligned", len(pcm))
	}
	// While paused (e.g. during TTS playback) drop audio instead of feeding the
	// stream, so the host's own output cannot self-trigger the wakeword.
	if p.paused.Load() {
		return 0, 0, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream == nil {
		return 0, 0, errors.New("wakeword: pipeline closed")
	}

	p.stream.AcceptWaveform(pcmToFloat32(pcm))

	prob := p.detectionProbability()
	for p.stream.IsReady() {
		keyword := strings.TrimSpace(p.stream.Decode())
		decodes++
		if keyword == "" {
			if len(p.consecutiveHits) > 0 {
				p.consecutiveHits = make(map[string]int)
			}
			continue
		}
		for kw := range p.consecutiveHits {
			if kw != keyword {
				delete(p.consecutiveHits, kw)
			}
		}
		p.consecutiveHits[keyword]++
		if p.consecutiveHits[keyword] < p.cfg.MinConsecutiveFrames {
			continue
		}
		p.stream.Reset()
		p.consecutiveHits[keyword] = 0
		now := p.now()
		if last, ok := p.lastTrigger[keyword]; ok && now.Sub(last) < p.cfg.Cooldown {
			continue
		}
		p.lastTrigger[keyword] = now
		p.sink.Emit(DetectionEvent{
			Phrase:      p.displayPhrase(keyword),
			Keyword:     keyword,
			Mode:        p.cfg.DefaultMode,
			Probability: prob,
			At:          now,
		})
		peakProb = prob
	}
	return decodes, peakProb, nil
}

// Reset clears rolling engine state and the debounce maps. Hosts call it
// after a detection-triggered mode change so the same utterance cannot
// re-fire once the cooldown lapses.
func (p *Pipeline) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetLocked()
}

func (p *Pipeline) resetLocked() {
	if p.stream == nil {
		return
	}
	p.stream.Reset()
	p.consecutiveHits = make(map[string]int)
}

// Pause suppresses detection and stops feeding audio to the engine. It is
// safe to call from any goroutine and is idempotent. Use it around TTS
// playback (or any host self-audio) to prevent barge-in self-triggering.
func (p *Pipeline) Pause() { p.paused.Store(true) }

// Resume re-enables detection and clears any partial match / debounce state
// so audio buffered during the pause cannot produce a stale trigger.
func (p *Pipeline) Resume() {
	p.mu.Lock()
	p.resetLocked()
	p.mu.Unlock()
	p.paused.Store(false)
}

// Paused reports whether detection is currently suppressed.
func (p *Pipeline) Paused() bool { return p.paused.Load() }

// Close releases the engine stream. Subsequent FeedPCM calls error. The
// Detector is not closed: the host that constructed both owns its lifetime.
func (p *Pipeline) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream == nil {
		return nil
	}
	err := p.stream.Close()
	p.stream = nil
	return err
}

// Config returns a copy of the resolved pipeline config (with defaults
// applied).
func (p *Pipeline) Config() Config {
	if p == nil {
		return normalizeConfig(Config{})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
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

// pcmToFloat32 converts S16LE PCM to float32 samples in [-1, 1].
func pcmToFloat32(pcm []byte) []float32 {
	samples := make([]float32, len(pcm)/BytesPerSample)
	for i := range samples {
		s := int16(binary.LittleEndian.Uint16(pcm[i*BytesPerSample : (i+1)*BytesPerSample])) // #nosec G115 -- S16LE PCM decoding reinterprets identical-width sample bits.
		samples[i] = float32(s) / 32768.0
	}
	return samples
}

// detectionProbability returns the confidence lower bound reported on a
// detection event. Engines expose no exact per-detection score, so the
// pipeline reports the effective keyword threshold the detection cleared:
// the configured Threshold when set, else the detector's, else the engine
// default.
func (p *Pipeline) detectionProbability() float32 {
	if p.cfg.Threshold > 0 {
		return p.cfg.Threshold
	}
	if t := p.detector.Threshold(); t > 0 {
		return t
	}
	return defaultKeywordThreshold
}

func (p *Pipeline) displayPhrase(keyword string) string {
	if p.cfg.Phrase != "" {
		return p.cfg.Phrase
	}
	return keyword
}
