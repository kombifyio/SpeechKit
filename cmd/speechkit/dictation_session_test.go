package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/vad"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeSessionVAD struct {
	probs []float32
	idx   int
}

func (f *fakeSessionVAD) ProcessFrame([]int16) (float32, error) {
	if f.idx >= len(f.probs) {
		return 0, nil
	}
	prob := f.probs[f.idx]
	f.idx++
	return prob, nil
}

func (f *fakeSessionVAD) Reset() {
	f.idx = 0
}

func TestDictationCaptureSessionFallsBackToWholePCMWithoutDetector(t *testing.T) {
	session := newDictationCaptureSession(nil, &config.Config{})
	if session != nil {
		t.Fatalf("expected nil session without detector")
	}
	segments := speechkit.FallbackDictationSegments(framePCM(1))
	if len(segments) != 1 {
		t.Fatalf("expected 1 fallback segment, got %d", len(segments))
	}
	if !segments[0].Final {
		t.Fatalf("fallback segment should be final")
	}
}

func TestDictationCaptureSessionCollectsBufferedSegmentsOnStop(t *testing.T) {
	session := newDictationCaptureSession(&fakeSessionVAD{probs: []float32{0.9, 0.9, 0.1, 0.1}}, &config.Config{
		General: config.GeneralConfig{
			AutoStopSilenceMs: 64,
		},
	})
	if session == nil {
		t.Fatalf("expected session")
	}

	if err := session.FeedPCM(joinFrames(speechFrame(), speechFrame(), silenceFrame(), silenceFrame())); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}

	segments, err := session.CollectStopSegments(joinFrames(speechFrame(), speechFrame()))
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0].Paragraph {
		t.Fatalf("first segment should not start a paragraph")
	}
}

func speechFrame() []byte {
	return framePCM(1)
}

func silenceFrame() []byte {
	return framePCM(0)
}

func framePCM(sample int16) []byte {
	pcm := make([]byte, vad.FrameBytes)
	for i := 0; i < vad.FrameSize; i++ {
		pcm[i*vad.BytesPerSample] = byte(sample)
		pcm[i*vad.BytesPerSample+1] = byte(sample >> 8)
	}
	return pcm
}

func joinFrames(frames ...[]byte) []byte {
	total := 0
	for _, frame := range frames {
		total += len(frame)
	}
	out := make([]byte, 0, total)
	for _, frame := range frames {
		out = append(out, frame...)
	}
	return out
}

var _ vad.Detector = (*fakeSessionVAD)(nil)
