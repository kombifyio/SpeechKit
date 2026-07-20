//go:build cgo

package wakeword

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

const (
	defaultMinConsecutiveFrames = 1
	defaultCooldown             = 1500 * time.Millisecond
	// defaultKeywordThreshold mirrors sherpa-onnx's built-in KWS threshold,
	// used as the reported Probability lower bound when no explicit threshold
	// is configured. The sherpa binding exposes no exact per-detection score.
	defaultKeywordThreshold = 0.25
)

// Pipeline streams PCM audio into a sherpa-onnx KeywordSpotter.
type Pipeline struct {
	detector *Detector
	sink     Sink
	cfg      Config
	now      func() time.Time

	paused atomic.Bool

	mu              sync.Mutex
	stream          *sherpa.OnlineStream
	lastTrigger     map[string]time.Time
	consecutiveHits map[string]int
}

// NewPipeline wires a Detector and Sink together.
func NewPipeline(detector *Detector, sink Sink, cfg Config) (*Pipeline, error) {
	if detector == nil {
		return nil, errors.New("wakeword: nil detector")
	}
	if sink == nil {
		return nil, errors.New("wakeword: nil sink")
	}
	cfg = normalizeConfig(cfg)
	stream := sherpa.NewKeywordStream(detector.spotter)
	if stream == nil {
		return nil, fmt.Errorf("wakeword: NewKeywordStream returned nil")
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

// FeedPCM ingests raw S16 mono PCM at SampleRate.
func (p *Pipeline) FeedPCM(pcm []byte) (decodes int, peakProb float32, err error) {
	if len(pcm) == 0 {
		return 0, 0, nil
	}
	if len(pcm)%BytesPerSample != 0 {
		return 0, 0, fmt.Errorf("wakeword: pcm len %d not S16-aligned", len(pcm))
	}
	// While paused (e.g. during TTS playback) drop audio instead of feeding the
	// stream, so the box's own output cannot self-trigger the wakeword.
	if p.paused.Load() {
		return 0, 0, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.detector == nil || p.stream == nil {
		return 0, 0, errors.New("wakeword: pipeline closed")
	}

	samples := make([]float32, len(pcm)/BytesPerSample)
	for i := range samples {
		s := int16(binary.LittleEndian.Uint16(pcm[i*BytesPerSample : (i+1)*BytesPerSample])) // #nosec G115 -- S16LE PCM decoding reinterprets identical-width sample bits.
		samples[i] = float32(s) / 32768.0
	}
	p.stream.AcceptWaveform(SampleRate, samples)

	prob := p.detectionProbability()
	for p.detector.spotter.IsReady(p.stream) {
		p.detector.spotter.Decode(p.stream)
		res := p.detector.spotter.GetResult(p.stream)
		decodes++
		if res.Keyword == "" {
			if len(p.consecutiveHits) > 0 {
				p.consecutiveHits = make(map[string]int)
			}
			continue
		}
		keyword := strings.TrimSpace(res.Keyword)
		for kw := range p.consecutiveHits {
			if kw != keyword {
				delete(p.consecutiveHits, kw)
			}
		}
		p.consecutiveHits[keyword]++
		if p.consecutiveHits[keyword] < p.cfg.MinConsecutiveFrames {
			continue
		}
		p.detector.spotter.Reset(p.stream)
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

// Reset clears rolling detector state and debounce maps.
func (p *Pipeline) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetLocked()
}

func (p *Pipeline) resetLocked() {
	if p.detector == nil || p.stream == nil {
		return
	}
	p.detector.spotter.Reset(p.stream)
	p.consecutiveHits = make(map[string]int)
}

// Pause suppresses detection and stops feeding audio to the stream. It is safe
// to call from any goroutine and is idempotent. Use it around TTS playback (or
// any host self-audio) to prevent barge-in self-triggering.
func (p *Pipeline) Pause() { p.paused.Store(true) }

// Resume re-enables detection and clears any partial match / debounce state so
// audio buffered during the pause cannot produce a stale trigger.
func (p *Pipeline) Resume() {
	p.mu.Lock()
	p.resetLocked()
	p.mu.Unlock()
	p.paused.Store(false)
}

// Paused reports whether detection is currently suppressed.
func (p *Pipeline) Paused() bool { return p.paused.Load() }

// Close releases the streaming handle.
func (p *Pipeline) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream != nil {
		sherpa.DeleteOnlineStream(p.stream)
		p.stream = nil
	}
	return nil
}

// Config returns a copy of the resolved pipeline config.
func (p *Pipeline) Config() Config {
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

// detectionProbability returns the confidence lower bound reported on a
// detection event. The sherpa-onnx KWS binding exposes no exact per-detection
// score, so we report the effective keyword threshold the detection cleared:
// the configured Threshold when set, else the detector's, else sherpa's default.
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
