//go:build cgo

package wakeword

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

const (
	defaultMinConsecutiveFrames = 1
	defaultCooldown             = 1500 * time.Millisecond
)

// Pipeline streams PCM audio into a sherpa-onnx KeywordSpotter.
type Pipeline struct {
	detector *Detector
	sink     Sink
	cfg      Config
	now      func() time.Time

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
			Probability: 1.0,
			At:          now,
		})
		peakProb = 1.0
	}
	return decodes, peakProb, nil
}

// Reset clears rolling detector state and debounce maps.
func (p *Pipeline) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.detector == nil || p.stream == nil {
		return
	}
	p.detector.spotter.Reset(p.stream)
	p.consecutiveHits = make(map[string]int)
}

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
	cfg.MinConsecutiveFrames = defaultMinConsecutiveFrames
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = defaultCooldown
	}
	return cfg
}

func (p *Pipeline) displayPhrase(keyword string) string {
	if p.cfg.Phrase != "" {
		return p.cfg.Phrase
	}
	return keyword
}
