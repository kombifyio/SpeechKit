package speechkit

import (
	"time"
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
