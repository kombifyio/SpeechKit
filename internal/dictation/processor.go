package dictation

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/vad"
)

const (
	defaultPause      = 500 * time.Millisecond
	defaultMinSegment = 100 * time.Millisecond
	speechThreshold   = 0.5
)

// Config controls pause-based dictation segmentation.
type Config struct {
	PauseThreshold time.Duration
	MinSegment     time.Duration
	Padding        time.Duration
	Overlap        time.Duration
}

// Segment is a transcribable utterance extracted from a dictation session.
type Segment struct {
	PCM       []byte
	Duration  time.Duration
	Paragraph bool
	Final     bool
}

// Processor segments PCM audio into speech chunks using a VAD.
type Processor struct {
	vad vad.Detector
	cfg Config

	mu          sync.Mutex
	pending     []byte
	preRoll     []byte
	active      []byte
	tailSilence []byte
	inSpeech    bool
	silenceTime time.Duration
	emittedAny  bool
}

// NewProcessor creates a dictation processor with sane defaults.
func NewProcessor(detector vad.Detector, cfg Config) *Processor {
	if cfg.PauseThreshold <= 0 {
		cfg.PauseThreshold = defaultPause
	}
	if cfg.MinSegment <= 0 {
		cfg.MinSegment = defaultMinSegment
	}

	return &Processor{
		vad: detector,
		cfg: cfg,
	}
}

// FeedPCM ingests raw S16 mono PCM and returns any segments flushed while processing.
func (p *Processor) FeedPCM(pcm []byte) ([]Segment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.pending = append(p.pending, pcm...)

	var out []Segment
	for len(p.pending) >= vad.FrameBytes {
		frame := p.pending[:vad.FrameBytes]
		p.pending = p.pending[vad.FrameBytes:]

		segments, err := p.feedFrame(frame)
		if err != nil {
			return nil, err
		}
		out = append(out, segments...)
	}

	return out, nil
}

// Flush returns the trailing buffered segment, if any, and resets session state.
func (p *Processor) Flush() ([]Segment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var out []Segment
	if p.inSpeech && len(p.pending) > 0 {
		p.active = append(p.active, p.pending...)
	}
	p.pending = nil
	if p.inSpeech && len(p.active) > 0 {
		segment := p.buildSegment(p.active, true)
		if segment != nil {
			out = append(out, *segment)
		}
	}

	if p.vad != nil {
		p.vad.Reset()
	}
	p.resetSession()
	return out, nil
}

// Reset clears the current dictation session and VAD state.
func (p *Processor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resetSession()
	if p.vad != nil {
		p.vad.Reset()
	}
}

func (p *Processor) feedFrame(frame []byte) ([]Segment, error) {
	samples := make([]int16, vad.FrameSize)
	for i := 0; i < vad.FrameSize; i++ {
		offset := i * vad.BytesPerSample
		samples[i] = int16(binary.LittleEndian.Uint16(frame[offset : offset+vad.BytesPerSample])) //nolint:gosec // Windows API integer conversion, value fits
	}

	prob, err := p.vad.ProcessFrame(samples)
	if err != nil {
		return nil, fmt.Errorf("vad frame: %w", err)
	}

	speaking := prob > speechThreshold
	frameDur := frameDuration()

	if speaking {
		if !p.inSpeech {
			p.inSpeech = true
			p.silenceTime = 0
			if len(p.preRoll) > 0 {
				p.active = append(p.active, p.preRoll...)
				p.preRoll = nil
			}
		}
		if len(p.tailSilence) > 0 {
			p.active = append(p.active, p.tailSilence...)
			p.tailSilence = nil
		}
		p.active = append(p.active, frame...)
		p.silenceTime = 0
		return nil, nil
	}

	if !p.inSpeech {
		p.appendPreRoll(frame)
		return nil, nil
	}

	p.silenceTime += frameDur
	p.appendTailSilence(frame)
	if p.silenceTime < p.cfg.PauseThreshold {
		return nil, nil
	}

	segmentPCM := p.active
	if len(p.tailSilence) > 0 {
		segmentPCM = append(segmentPCM, p.tailSilence...)
	}
	segment := p.buildSegment(segmentPCM, false)
	if segment != nil {
		out := []Segment{*segment}
		p.active = nil
		p.tailSilence = nil
		p.inSpeech = false
		p.silenceTime = 0
		return out, nil
	}

	p.active = nil
	p.tailSilence = nil
	p.inSpeech = false
	p.silenceTime = 0
	return nil, nil
}

func (p *Processor) buildSegment(pcm []byte, final bool) *Segment {
	if len(pcm) == 0 {
		return nil
	}

	duration := time.Duration(len(pcm)) * time.Second / (vad.SampleRate * vad.BytesPerSample)
	if duration < p.cfg.MinSegment {
		return nil
	}

	segment := &Segment{
		PCM:       append([]byte(nil), pcm...),
		Duration:  duration,
		Paragraph: p.emittedAny,
		Final:     final,
	}

	p.emittedAny = true
	p.appendOverlapTail(pcm)
	return segment
}

func (p *Processor) appendPreRoll(frame []byte) {
	if limit := maxBytes(p.cfg.Padding, p.cfg.Overlap); limit > 0 {
		p.preRoll = append(p.preRoll, frame...)
		p.preRoll = trimLeft(p.preRoll, limit)
	}
}

func (p *Processor) appendOverlapTail(pcm []byte) {
	limit := bytesForDuration(p.cfg.Overlap)
	if limit <= 0 {
		p.preRoll = nil
		return
	}
	p.preRoll = append(p.preRoll[:0], tailBytes(pcm, limit)...)
}

func (p *Processor) appendTailSilence(frame []byte) {
	limit := bytesForDuration(p.cfg.Padding)
	if limit <= 0 {
		p.tailSilence = nil
		return
	}
	p.tailSilence = append(p.tailSilence, frame...)
	p.tailSilence = trimLeft(p.tailSilence, limit)
}

func (p *Processor) resetSession() {
	p.pending = nil
	p.preRoll = nil
	p.active = nil
	p.tailSilence = nil
	p.inSpeech = false
	p.silenceTime = 0
	p.emittedAny = false
}

func frameDuration() time.Duration {
	return time.Duration(vad.FrameSize) * time.Second / vad.SampleRate
}

func bytesForDuration(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d * time.Duration(vad.SampleRate*vad.BytesPerSample) / time.Second)
}

func maxBytes(a, b time.Duration) int {
	if a >= b {
		return bytesForDuration(a)
	}
	return bytesForDuration(b)
}

func trimLeft(buf []byte, limit int) []byte {
	if limit <= 0 || len(buf) <= limit {
		return append([]byte(nil), buf...)
	}
	return append([]byte(nil), buf[len(buf)-limit:]...)
}

func tailBytes(buf []byte, limit int) []byte {
	if limit <= 0 || len(buf) <= limit {
		return append([]byte(nil), buf...)
	}
	return append([]byte(nil), buf[len(buf)-limit:]...)
}
