package pipeline

import (
	"fmt"
	"hash/fnv"
	"strings"
	"testing"
	"time"
)

// TestDictationSegmenterStreamBoundariesStable feeds a fixed synthetic stream
// in irregular chunks and pins the emitted segments (size, flags, content
// hash), guarding segment decisions against buffer-handling refactors.
func TestDictationSegmenterStreamBoundariesStable(t *testing.T) {
	// Alternating speech/silence run lengths in detector frames.
	runs := []int{40, 12, 25, 60, 30, 5, 70, 140, 20, 90}
	var speech []bool
	for i, n := range runs {
		for j := 0; j < n; j++ {
			speech = append(speech, i%2 == 0)
		}
	}
	pcm := make([]byte, len(speech)*dictationFrameBytes)
	x := uint32(12345)
	for i := range pcm {
		x = x*1664525 + 1013904223
		pcm[i] = byte(x >> 24)
	}

	seg := NewDictationSegmenter(&scriptVAD{speech: speech}, 300*time.Millisecond)
	seg.SetMinIntermediateSegment(600 * time.Millisecond)
	seg.SetMaxUtterance(1500 * time.Millisecond)

	var got []string
	add := func(prefix string, data []byte, para, final bool, d time.Duration) {
		h := fnv.New64a()
		_, _ = h.Write(data)
		got = append(got, fmt.Sprintf("%s len=%d dur=%s para=%t final=%t h=%x", prefix, len(data), d, para, final, h.Sum64()))
	}
	sizes := []int{1000, 333, 20480, 7, 4097, 15000}
	limit := len(pcm) - 3000 // leave an unfed tail for CollectStopSegments
	fed := 0
	for i := 0; fed < limit; i++ {
		n := min(sizes[i%len(sizes)], limit-fed)
		if err := seg.FeedPCM(pcm[fed : fed+n]); err != nil {
			t.Fatal(err)
		}
		fed += n
		for _, s := range seg.DrainReadySegments() {
			add("ready", s.PCM, s.Paragraph, s.Final, s.Duration)
		}
	}
	stop, err := seg.CollectStopSegments(pcm)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stop {
		add("stop", s.PCM, s.Paragraph, s.Final, s.Duration)
	}

	const want = `ready len=51200 dur=1.6s para=false final=false h=6ddc42ff2dec4915
ready len=44288 dur=1.384s para=false final=false h=b968ec2b8bd3a666
ready len=52224 dur=1.632s para=false final=false h=462247af7e86f8de
ready len=48128 dur=1.504s para=false final=false h=ada4ecb3aaa919e4
ready len=46080 dur=1.44s para=true final=false h=c089b7e82c372f30`
	if joined := strings.Join(got, "\n"); joined != want {
		t.Fatalf("segments changed:\n%s", joined)
	}
}

type scriptVAD struct {
	speech []bool
	n      int
}

func (v *scriptVAD) ProcessFrame([]int16) (float32, error) {
	i := v.n
	v.n++
	if i < len(v.speech) && v.speech[i] {
		return 0.9, nil
	}
	return 0.05, nil
}

func (v *scriptVAD) Reset() { v.n = 0 }
