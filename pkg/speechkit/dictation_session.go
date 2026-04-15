package speechkit

import (
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/dictation"
	"github.com/kombifyio/SpeechKit/internal/vad"
)

const (
	DefaultDictationMinSegment = 1200 * time.Millisecond
	DefaultDictationPadding    = 160 * time.Millisecond
	DefaultDictationOverlap    = 200 * time.Millisecond
)

// DictationSegmenter implements [SegmentCollector] using VAD-based pause
// detection to split continuous speech into discrete segments.
type DictationSegmenter struct {
	processor *dictation.Processor
	segments  []dictation.Segment
}

func NewDictationSegmenter(detector vad.Detector, pauseThreshold time.Duration) *DictationSegmenter {
	if detector == nil {
		return nil
	}
	if pauseThreshold <= 0 {
		pauseThreshold = 700 * time.Millisecond
	}

	return &DictationSegmenter{
		processor: dictation.NewProcessor(detector, dictation.Config{
			PauseThreshold: pauseThreshold,
			MinSegment:     DefaultDictationMinSegment,
			Padding:        DefaultDictationPadding,
			Overlap:        DefaultDictationOverlap,
		}),
	}
}

func (s *DictationSegmenter) FeedPCM(pcm []byte) error {
	if s == nil || s.processor == nil || len(pcm) == 0 {
		return nil
	}

	segments, err := s.processor.FeedPCM(pcm)
	if err != nil {
		return err
	}
	s.segments = append(s.segments, segments...)
	return nil
}

func (s *DictationSegmenter) CollectStopSegments(fullPCM []byte) ([]dictation.Segment, error) {
	if s == nil || s.processor == nil {
		return FallbackDictationSegments(fullPCM), nil
	}

	flushed, err := s.processor.Flush()
	if err != nil {
		return nil, err
	}

	segments := append([]dictation.Segment{}, s.segments...)
	segments = append(segments, flushed...)
	s.segments = nil

	if len(segments) == 0 {
		return FallbackDictationSegments(fullPCM), nil
	}
	return segments, nil
}

// FallbackDictationSegments wraps all of fullPCM in a single segment.
// Used when VAD-based segmentation is unavailable or produces no output.
func FallbackDictationSegments(fullPCM []byte) []dictation.Segment {
	if len(fullPCM) == 0 {
		return nil
	}

	return []dictation.Segment{{
		PCM:      append([]byte(nil), fullPCM...),
		Duration: time.Duration(audio.PCMDurationSecs(fullPCM) * float64(time.Second)),
		Final:    true,
	}}
}
