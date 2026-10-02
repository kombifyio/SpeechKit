package pipeline

import (
	"testing"
	"time"
)

// cycleVAD reports speech for speechFrames frames, then silence for
// silenceFrames frames, repeatedly, so the segmenter exercises both its speech
// accumulation and its pause-driven segment cut.
type cycleVAD struct {
	speechFrames, silenceFrames, n int
}

func (v *cycleVAD) ProcessFrame([]int16) (float32, error) {
	pos := v.n % (v.speechFrames + v.silenceFrames)
	v.n++
	if pos < v.speechFrames {
		return 0.9, nil
	}
	return 0.05, nil
}

func (v *cycleVAD) Reset() { v.n = 0 }

// BenchmarkDictationSegmenterFeedPCM measures feeding capture-sized chunks
// (about 640 ms, 20 detector frames) through VAD framing and segment cutting,
// then draining ready segments as the recording controller does.
func BenchmarkDictationSegmenterFeedPCM(b *testing.B) {
	seg := NewDictationSegmenter(&cycleVAD{speechFrames: 60, silenceFrames: 30}, 300*time.Millisecond)
	chunk := make([]byte, 20*dictationFrameBytes)
	b.ReportAllocs()
	b.SetBytes(int64(len(chunk)))
	for b.Loop() {
		if err := seg.FeedPCM(chunk); err != nil {
			b.Fatalf("FeedPCM: %v", err)
		}
		_ = seg.DrainReadySegments()
	}
}
