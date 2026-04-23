package speechkit

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultDictationMinSegment = 1200 * time.Millisecond
	DefaultDictationPadding    = 160 * time.Millisecond
	DefaultDictationOverlap    = 200 * time.Millisecond

	dictationFrameSize       = 512
	dictationFrameBytes      = dictationFrameSize * AudioBytesPerSample
	dictationSpeechThreshold = 0.5
)

// AudioSegment is a transcribable utterance extracted from a dictation
// recording. PCM is raw 16kHz S16 mono audio.
type AudioSegment struct {
	PCM       []byte
	Duration  time.Duration
	Paragraph bool
	Final     bool
}

// VoiceActivityDetector is the public VAD contract consumed by
// DictationSegmenter. It intentionally matches SpeechKit's internal Silero
// detector shape without exposing internal packages.
type VoiceActivityDetector interface {
	ProcessFrame([]int16) (float32, error)
	Reset()
}

// DictationSegmenter implements [SegmentCollector] using VAD-based pause
// detection to split continuous speech into discrete segments.
type DictationSegmenter struct {
	detector VoiceActivityDetector

	mu          sync.Mutex
	pause       time.Duration
	minSegment  time.Duration
	padding     time.Duration
	overlap     time.Duration
	pending     []byte
	preRoll     []byte
	active      []byte
	tailSilence []byte
	inSpeech    bool
	silenceTime time.Duration
	emittedAny  bool
	segments    []AudioSegment
}

func NewDictationSegmenter(detector VoiceActivityDetector, pauseThreshold time.Duration) *DictationSegmenter {
	if detector == nil {
		return nil
	}
	if pauseThreshold <= 0 {
		pauseThreshold = 700 * time.Millisecond
	}

	return &DictationSegmenter{
		detector:   detector,
		pause:      pauseThreshold,
		minSegment: DefaultDictationMinSegment,
		padding:    DefaultDictationPadding,
		overlap:    DefaultDictationOverlap,
	}
}

func (s *DictationSegmenter) FeedPCM(pcm []byte) error {
	if s == nil || s.detector == nil || len(pcm) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending = append(s.pending, pcm...)
	for len(s.pending) >= dictationFrameBytes {
		frame := s.pending[:dictationFrameBytes]
		s.pending = s.pending[dictationFrameBytes:]

		segments, err := s.feedFrame(frame)
		if err != nil {
			return err
		}
		s.segments = append(s.segments, segments...)
	}
	return nil
}

func (s *DictationSegmenter) CollectStopSegments(fullPCM []byte) ([]AudioSegment, error) {
	if s == nil || s.detector == nil {
		return FallbackDictationSegments(fullPCM), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var flushed []AudioSegment
	if s.inSpeech && len(s.pending) > 0 {
		s.active = append(s.active, s.pending...)
	}
	s.pending = nil
	if s.inSpeech && len(s.active) > 0 {
		if segment := s.buildSegment(s.active, true); segment != nil {
			flushed = append(flushed, *segment)
		}
	}

	segments := append([]AudioSegment{}, s.segments...)
	segments = append(segments, flushed...)
	s.segments = nil

	s.detector.Reset()
	s.resetSession()

	if len(segments) == 0 {
		return FallbackDictationSegments(fullPCM), nil
	}
	return segments, nil
}

// FallbackDictationSegments wraps all of fullPCM in a single segment.
// Used when VAD-based segmentation is unavailable or produces no output.
func FallbackDictationSegments(fullPCM []byte) []AudioSegment {
	if len(fullPCM) == 0 {
		return nil
	}

	return []AudioSegment{{
		PCM:      append([]byte(nil), fullPCM...),
		Duration: time.Duration(PCMDurationSecs(fullPCM) * float64(time.Second)),
		Final:    true,
	}}
}

func (s *DictationSegmenter) feedFrame(frame []byte) ([]AudioSegment, error) {
	samples := make([]int16, dictationFrameSize)
	for i := 0; i < dictationFrameSize; i++ {
		offset := i * AudioBytesPerSample
		samples[i] = int16(binary.LittleEndian.Uint16(frame[offset : offset+AudioBytesPerSample])) //nolint:gosec // PCM sample conversion.
	}

	prob, err := s.detector.ProcessFrame(samples)
	if err != nil {
		return nil, fmt.Errorf("vad frame: %w", err)
	}

	speaking := prob > dictationSpeechThreshold
	frameDur := dictationFrameDuration()

	if speaking {
		if !s.inSpeech {
			s.inSpeech = true
			s.silenceTime = 0
			if len(s.preRoll) > 0 {
				s.active = append(s.active, s.preRoll...)
				s.preRoll = nil
			}
		}
		if len(s.tailSilence) > 0 {
			s.active = append(s.active, s.tailSilence...)
			s.tailSilence = nil
		}
		s.active = append(s.active, frame...)
		s.silenceTime = 0
		return nil, nil
	}

	if !s.inSpeech {
		s.appendPreRoll(frame)
		return nil, nil
	}

	s.silenceTime += frameDur
	s.appendTailSilence(frame)
	if s.silenceTime < s.pause {
		return nil, nil
	}

	segmentPCM := s.active
	if len(s.tailSilence) > 0 {
		segmentPCM = append(segmentPCM, s.tailSilence...)
	}
	segment := s.buildSegment(segmentPCM, false)

	s.active = nil
	s.tailSilence = nil
	s.inSpeech = false
	s.silenceTime = 0

	if segment == nil {
		return nil, nil
	}
	return []AudioSegment{*segment}, nil
}

func (s *DictationSegmenter) buildSegment(pcm []byte, final bool) *AudioSegment {
	if len(pcm) == 0 {
		return nil
	}

	duration := time.Duration(len(pcm)) * time.Second / (AudioSampleRate * AudioBytesPerSample)
	if duration < s.minSegment {
		return nil
	}

	segment := &AudioSegment{
		PCM:       append([]byte(nil), pcm...),
		Duration:  duration,
		Paragraph: s.emittedAny,
		Final:     final,
	}

	s.emittedAny = true
	s.appendOverlapTail(pcm)
	return segment
}

func (s *DictationSegmenter) appendPreRoll(frame []byte) {
	if limit := maxDictationBytes(s.padding, s.overlap); limit > 0 {
		s.preRoll = append(s.preRoll, frame...)
		s.preRoll = trimLeftDictationBytes(s.preRoll, limit)
	}
}

func (s *DictationSegmenter) appendOverlapTail(pcm []byte) {
	limit := dictationBytesForDuration(s.overlap)
	if limit <= 0 {
		s.preRoll = nil
		return
	}
	s.preRoll = append(s.preRoll[:0], tailDictationBytes(pcm, limit)...)
}

func (s *DictationSegmenter) appendTailSilence(frame []byte) {
	limit := dictationBytesForDuration(s.padding)
	if limit <= 0 {
		s.tailSilence = nil
		return
	}
	s.tailSilence = append(s.tailSilence, frame...)
	s.tailSilence = trimLeftDictationBytes(s.tailSilence, limit)
}

func (s *DictationSegmenter) resetSession() {
	s.pending = nil
	s.preRoll = nil
	s.active = nil
	s.tailSilence = nil
	s.inSpeech = false
	s.silenceTime = 0
	s.emittedAny = false
}

func dictationFrameDuration() time.Duration {
	return time.Duration(dictationFrameSize) * time.Second / AudioSampleRate
}

func dictationBytesForDuration(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d * time.Duration(AudioSampleRate*AudioBytesPerSample) / time.Second)
}

func maxDictationBytes(a, b time.Duration) int {
	if a >= b {
		return dictationBytesForDuration(a)
	}
	return dictationBytesForDuration(b)
}

func trimLeftDictationBytes(buf []byte, limit int) []byte {
	if limit <= 0 || len(buf) <= limit {
		return append([]byte(nil), buf...)
	}
	return append([]byte(nil), buf[len(buf)-limit:]...)
}

func tailDictationBytes(buf []byte, limit int) []byte {
	if limit <= 0 || len(buf) <= limit {
		return append([]byte(nil), buf...)
	}
	return append([]byte(nil), buf[len(buf)-limit:]...)
}
