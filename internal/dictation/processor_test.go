package dictation

import (
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/vad"
)

type fakeVAD struct {
	probs      []float32
	idx        int
	resetCalls int
}

var _ vad.Detector = (*fakeVAD)(nil)

func (f *fakeVAD) ProcessFrame(pcm []int16) (float32, error) {
	if f.idx >= len(f.probs) {
		return 0, nil
	}
	prob := f.probs[f.idx]
	f.idx++
	return prob, nil
}

func (f *fakeVAD) Reset() {
	f.resetCalls++
	f.idx = 0
}

func TestProcessor_EmitsSegmentAfterPause(t *testing.T) {
	p := NewProcessor(&fakeVAD{probs: []float32{0.9, 0.9, 0.1, 0.1}}, Config{
		PauseThreshold: 64 * time.Millisecond,
		MinSegment:     64 * time.Millisecond,
	})

	segments, err := p.FeedPCM(joinFrames(speechFrame(), speechFrame(), silenceFrame(), silenceFrame()))
	if err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0].Final {
		t.Fatalf("expected partial segment, got final")
	}
	if segments[0].Paragraph {
		t.Fatalf("first segment should not start a new paragraph")
	}
	if got, want := len(segments[0].PCM), len(joinFrames(speechFrame(), speechFrame())); got != want {
		t.Fatalf("segment PCM length = %d, want %d", got, want)
	}
}

func TestProcessor_IgnoresTooShortSegments(t *testing.T) {
	p := NewProcessor(&fakeVAD{probs: []float32{0.9, 0.1, 0.1}}, Config{
		PauseThreshold: 64 * time.Millisecond,
		MinSegment:     96 * time.Millisecond,
	})

	segments, err := p.FeedPCM(joinFrames(speechFrame(), silenceFrame(), silenceFrame()))
	if err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("expected 0 segments, got %d", len(segments))
	}
	if flushed, err := p.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	} else if len(flushed) != 0 {
		t.Fatalf("expected no flushed segment, got %d", len(flushed))
	}
}

func TestProcessor_StartsNewParagraphAfterPause(t *testing.T) {
	p := NewProcessor(&fakeVAD{probs: []float32{
		0.9, 0.9, 0.1, 0.1,
		0.9, 0.9, 0.1, 0.1,
	}}, Config{
		PauseThreshold: 64 * time.Millisecond,
		MinSegment:     64 * time.Millisecond,
	})

	segments, err := p.FeedPCM(joinFrames(
		speechFrame(), speechFrame(), silenceFrame(), silenceFrame(),
		speechFrame(), speechFrame(), silenceFrame(), silenceFrame(),
	))
	if err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0].Paragraph {
		t.Fatalf("first segment should not start a paragraph")
	}
	if !segments[1].Paragraph {
		t.Fatalf("second segment should start a new paragraph")
	}
}

func TestProcessor_FlushesTrailingAudioOnStop(t *testing.T) {
	fake := &fakeVAD{probs: []float32{0.9, 0.9}}
	p := NewProcessor(fake, Config{
		PauseThreshold: 64 * time.Millisecond,
		MinSegment:     64 * time.Millisecond,
	})

	if segments, err := p.FeedPCM(joinFrames(speechFrame(), speechFrame())); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	} else if len(segments) != 0 {
		t.Fatalf("expected no early segments, got %d", len(segments))
	}

	flushed, err := p.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(flushed) != 1 {
		t.Fatalf("expected 1 flushed segment, got %d", len(flushed))
	}
	if !flushed[0].Final {
		t.Fatalf("expected final segment")
	}
	if fake.resetCalls != 1 {
		t.Fatalf("expected detector reset once, got %d", fake.resetCalls)
	}
}

func TestProcessor_FlushesTrailingPartialFrameOnStop(t *testing.T) {
	p := NewProcessor(&fakeVAD{probs: []float32{0.9, 0.9, 0.9}}, Config{
		PauseThreshold: 64 * time.Millisecond,
		MinSegment:     64 * time.Millisecond,
	})

	partial := speechFrame()[:len(speechFrame())/2]
	if segments, err := p.FeedPCM(append(joinFrames(speechFrame(), speechFrame()), partial...)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	} else if len(segments) != 0 {
		t.Fatalf("expected no early segments, got %d", len(segments))
	}

	flushed, err := p.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if len(flushed) != 1 {
		t.Fatalf("expected 1 flushed segment, got %d", len(flushed))
	}
	if got, want := len(flushed[0].PCM), len(joinFrames(speechFrame(), speechFrame()))+len(partial); got != want {
		t.Fatalf("flushed PCM length = %d, want %d", got, want)
	}
}

func TestProcessor_TrimsTrailingSilenceToConfiguredRightPadding(t *testing.T) {
	p := NewProcessor(&fakeVAD{probs: []float32{0.9, 0.9, 0.1, 0.1}}, Config{
		PauseThreshold: 64 * time.Millisecond,
		MinSegment:     64 * time.Millisecond,
		Padding:        32 * time.Millisecond,
	})

	segments, err := p.FeedPCM(joinFrames(speechFrame(), speechFrame(), silenceFrame(), silenceFrame()))
	if err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}

	want := len(joinFrames(speechFrame(), speechFrame(), silenceFrame()))
	if got := len(segments[0].PCM); got != want {
		t.Fatalf("segment PCM length = %d, want %d", got, want)
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
