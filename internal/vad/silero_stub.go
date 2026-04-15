//go:build !cgo

package vad

import "fmt"

const (
	SampleRate     = 16000
	FrameSize      = 512
	BytesPerSample = 2
	FrameBytes     = FrameSize * BytesPerSample
)

// Detector is the speech-probability contract consumed by dictation processors.
type Detector interface {
	ProcessFrame([]int16) (float32, error)
	Reset()
}

type SileroVAD struct{}

func NewSileroVAD(modelPath string) (*SileroVAD, error) {
	return nil, fmt.Errorf("silero vad requires cgo")
}

func (v *SileroVAD) ProcessFrame(pcm []int16) (float32, error) {
	return 0, fmt.Errorf("silero vad requires cgo")
}

func (v *SileroVAD) Reset() {}

func (v *SileroVAD) Close() {}
