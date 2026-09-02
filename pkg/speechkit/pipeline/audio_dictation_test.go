package pipeline

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeVAD struct {
	probs []float32
	errAt int
	calls int
	reset bool
}

func (v *fakeVAD) ProcessFrame([]int16) (float32, error) {
	v.calls++
	if v.errAt > 0 && v.calls == v.errAt {
		return 0, errors.New("vad failed")
	}
	if len(v.probs) == 0 {
		return 0, nil
	}
	prob := v.probs[0]
	v.probs = v.probs[1:]
	return prob, nil
}

func (v *fakeVAD) Reset() {
	v.reset = true
}

func TestDictationSegmenterHoldsShortPauseUntilStop(t *testing.T) {
	probs := append([]float32{0, 0}, repeatProb(0.8, 40)...)
	probs = append(probs, repeatProb(0, 4)...)
	probs = append(probs, repeatProb(0.8, 10)...)
	probs = append(probs, repeatProb(0, 4)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 64*time.Millisecond)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	if err := segmenter.FeedPCM(repeatFrame(60)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	segments, err := segmenter.CollectStopSegments(repeatFrame(1))
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}

	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	if segments[0].Duration < DefaultDictationMinSegment {
		t.Fatalf("segment duration = %s, want at least %s", segments[0].Duration, DefaultDictationMinSegment)
	}
	if segments[0].Paragraph {
		t.Fatal("first emitted segment should not request paragraph prefix")
	}
	if !segments[0].Final {
		t.Fatal("short pause should be flushed only as final segment on stop")
	}
	if !vad.reset {
		t.Fatal("detector should reset when stop segments are collected")
	}
}

func TestDictationSegmenterCollectStopSegmentsIncludesUnfedStopTail(t *testing.T) {
	liveFrames := framesForDuration(DefaultDictationMinSegment + dictationFrameDuration())
	tailFrames := 3
	vad := &fakeVAD{probs: repeatProb(0.8, liveFrames)}
	segmenter := NewDictationSegmenter(vad, 0)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	livePCM := bytes.Repeat([]byte{0x11}, liveFrames*dictationFrameBytes)
	pendingPCM := bytes.Repeat([]byte{0x22}, dictationFrameBytes/2)
	tailPCM := bytes.Repeat([]byte{0x33}, tailFrames*dictationFrameBytes)
	fedPCM := append(append([]byte(nil), livePCM...), pendingPCM...)
	fullPCM := append(append([]byte(nil), fedPCM...), tailPCM...)
	if err := segmenter.FeedPCM(fedPCM); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	segments, err := segmenter.CollectStopSegments(fullPCM)
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}

	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	if got, want := len(segments[0].PCM), len(fullPCM); got != want {
		t.Fatalf("segment PCM length = %d, want %d", got, want)
	}
	if !bytes.Equal(segments[0].PCM, fullPCM) {
		t.Fatalf("segment PCM does not preserve pending-before-stop-tail order")
	}
	if !segments[0].Final {
		t.Fatal("stop tail should be flushed as a final segment")
	}
}

func TestDictationSegmenterEmitsLongIntermediateSegmentAndResetsOnStop(t *testing.T) {
	speechFrames := framesForDuration(DefaultDictationMinIntermediateSegment + dictationFrameDuration())
	probs := append([]float32{0, 0}, repeatProb(0.8, speechFrames)...)
	probs = append(probs, repeatProb(0, 4)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 64*time.Millisecond)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	if err := segmenter.FeedPCM(repeatFrame(speechFrames + 6)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	segments, err := segmenter.CollectStopSegments(repeatFrame(1))
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}

	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
	if segments[0].Duration < DefaultDictationMinIntermediateSegment {
		t.Fatalf("segment duration = %s, want at least %s", segments[0].Duration, DefaultDictationMinIntermediateSegment)
	}
	if segments[0].Paragraph {
		t.Fatal("first emitted segment should not request paragraph prefix")
	}
	if segments[0].Final {
		t.Fatal("pause-emitted long segment should not be marked final")
	}
	if !vad.reset {
		t.Fatal("detector should reset when stop segments are collected")
	}
}

func TestDictationSegmenterDrainReadySegmentsPreventsStopFallbackDuplicate(t *testing.T) {
	speechFrames := framesForDuration(DefaultDictationMinIntermediateSegment + dictationFrameDuration())
	probs := append(repeatProb(0.8, speechFrames), repeatProb(0, 4)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 64*time.Millisecond)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	pcm := repeatFrame(speechFrames + 4)
	if err := segmenter.FeedPCM(pcm); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	ready := segmenter.DrainReadySegments()
	if len(ready) != 1 {
		t.Fatalf("ready segments = %d, want 1", len(ready))
	}
	if ready[0].Final {
		t.Fatal("drained pause segment should be intermediate")
	}

	remaining, err := segmenter.CollectStopSegments(pcm)
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining stop segments = %d, want none after drain; first duration=%s", len(remaining), remaining[0].Duration)
	}
	if !vad.reset {
		t.Fatal("detector should reset when stop segments are collected")
	}
}

func TestDictationSegmenterFiveMinuteLongPauseFixtureMaintainsOrderedSegments(t *testing.T) {
	speechFrames := framesForDuration(70 * time.Second)
	pauseFrames := framesForDuration(2 * time.Second)
	longPauseFrames := framesForDuration(DefaultDictationParagraphPause + 2*time.Second)
	segmentBytes := []byte{0x11, 0x22, 0x33, 0x44}

	var probs []float32
	var pcm []byte
	for i, marker := range segmentBytes {
		probs = append(probs, repeatProb(0.8, speechFrames)...)
		pcm = append(pcm, repeatMarkedFrame(speechFrames, marker)...)
		silenceFrames := pauseFrames
		if i == 1 {
			silenceFrames = longPauseFrames
		}
		probs = append(probs, repeatProb(0, silenceFrames)...)
		pcm = append(pcm, make([]byte, silenceFrames*dictationFrameBytes)...)
	}

	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 500*time.Millisecond)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	if err := segmenter.FeedPCM(pcm); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	ready := segmenter.DrainReadySegments()
	if len(ready) != len(segmentBytes) {
		t.Fatalf("ready segments = %d, want %d", len(ready), len(segmentBytes))
	}
	for i, segment := range ready {
		if segment.Final {
			t.Fatalf("ready segment %d Final = true, want intermediate segment", i)
		}
		if segment.Duration < DefaultDictationMinIntermediateSegment {
			t.Fatalf("ready segment %d duration = %s, want at least %s", i, segment.Duration, DefaultDictationMinIntermediateSegment)
		}
		if got, want := dominantNonZeroByte(segment.PCM), segmentBytes[i]; got != want {
			t.Fatalf("ready segment %d dominant marker = 0x%x, want 0x%x", i, got, want)
		}
	}
	if !ready[2].Paragraph {
		t.Fatal("segment after long pause should request paragraph prefix")
	}
	for _, idx := range []int{0, 1, 3} {
		if ready[idx].Paragraph {
			t.Fatalf("segment %d should not request paragraph prefix", idx)
		}
	}

	remaining, err := segmenter.CollectStopSegments(pcm)
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining stop segments = %d, want none after drained five-minute fixture", len(remaining))
	}
}

func TestDictationSegmenterDoesNotStartParagraphAfterShortPause(t *testing.T) {
	speechFrames := framesForDuration(DefaultDictationMinIntermediateSegment + dictationFrameDuration())
	probs := repeatProb(0.8, speechFrames)
	probs = append(probs, repeatProb(0, 2)...)
	probs = append(probs, repeatProb(0.8, speechFrames)...)
	probs = append(probs, repeatProb(0, 2)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 64*time.Millisecond)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	if err := segmenter.FeedPCM(repeatFrame(len(probs))); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	segments, err := segmenter.CollectStopSegments(repeatFrame(1))
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}

	if len(segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(segments))
	}
	if segments[0].Paragraph {
		t.Fatal("first emitted segment should not request paragraph prefix")
	}
	if segments[1].Paragraph {
		t.Fatal("second segment should not request paragraph prefix after a short pause")
	}
}

func TestDictationSegmenterStartsParagraphAfterLongPause(t *testing.T) {
	firstSpeechFrames := framesForDuration(DefaultDictationMinIntermediateSegment + dictationFrameDuration())
	idleFrames := framesForDuration(DefaultDictationParagraphPause + dictationFrameDuration())
	secondSpeechFrames := framesForDuration(DefaultDictationMinSegment + dictationFrameDuration())
	probs := repeatProb(0.8, firstSpeechFrames)
	probs = append(probs, repeatProb(0, 2)...)
	probs = append(probs, repeatProb(0, idleFrames)...)
	probs = append(probs, repeatProb(0.8, secondSpeechFrames)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 64*time.Millisecond)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	if err := segmenter.FeedPCM(repeatFrame(len(probs))); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	segments, err := segmenter.CollectStopSegments(repeatFrame(1))
	if err != nil {
		t.Fatalf("CollectStopSegments: %v", err)
	}

	if len(segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(segments))
	}
	if segments[0].Paragraph {
		t.Fatal("first emitted segment should not request paragraph prefix")
	}
	if !segments[1].Paragraph {
		t.Fatal("second segment should request paragraph prefix after a long pause")
	}
	if !segments[1].Final {
		t.Fatal("second segment should be flushed as final on stop")
	}
}

func TestDictationSegmenterFallsBackOnEmptyOrErrors(t *testing.T) {
	fullPCM := repeatFrame(2)
	if got := FallbackDictationSegments(nil); got != nil {
		t.Fatalf("empty fallback segments = %#v, want nil", got)
	}
	if got, err := NewDictationSegmenter(nil, 0).CollectStopSegments(fullPCM); err != nil || len(got) != 1 {
		t.Fatalf("nil segmenter fallback = %#v err=%v", got, err)
	}

	vad := &fakeVAD{probs: []float32{0.8}, errAt: 1}
	segmenter := NewDictationSegmenter(vad, 0)
	if err := segmenter.FeedPCM(repeatFrame(1)); err == nil {
		t.Fatal("expected VAD error from FeedPCM")
	}
}

func TestDictationSegmenterIdleAudioAccumulatesFrameTime(t *testing.T) {
	frame := dictationFrameDuration()
	probs := repeatProb(0, 5)
	probs = append(probs, repeatProb(0.8, 40)...)
	probs = append(probs, repeatProb(0, 3)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 2*frame)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	// Leading silence before any speech accumulates per processed frame.
	if err := segmenter.FeedPCM(repeatFrame(5)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	silence, lastFrame := segmenter.IdleAudio()
	if want := 5 * frame; silence != want {
		t.Fatalf("leading silence = %s, want %s", silence, want)
	}
	if lastFrame.IsZero() {
		t.Fatal("lastFrame should be stamped after FeedPCM")
	}

	// Active speech reports zero silence.
	if err := segmenter.FeedPCM(repeatFrame(40)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	silence, _ = segmenter.IdleAudio()
	if silence != 0 {
		t.Fatalf("silence during speech = %s, want 0", silence)
	}

	// Post-speech pause: 2 frames trip the pause threshold (credited to
	// idle silence), the 3rd accumulates on top.
	if err := segmenter.FeedPCM(repeatFrame(3)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	silence, _ = segmenter.IdleAudio()
	if want := 3 * frame; silence != want {
		t.Fatalf("post-speech silence = %s, want %s", silence, want)
	}
}

func TestDictationSegmenterIdleAudioFreezesWithoutFrames(t *testing.T) {
	frame := dictationFrameDuration()
	probs := repeatProb(0.8, 40)
	probs = append(probs, repeatProb(0, 3)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 2*frame)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	current := time.Unix(1000, 0)
	segmenter.nowFunc = func() time.Time { return current }

	if err := segmenter.FeedPCM(repeatFrame(43)); err != nil {
		t.Fatalf("FeedPCM: %v", err)
	}
	silenceBefore, lastFrameBefore := segmenter.IdleAudio()
	if silenceBefore == 0 {
		t.Fatal("expected accumulated post-speech silence")
	}

	// Wall clock advances 10s with NO frames delivered (CPU-starvation
	// stall). Audio-anchored silence must not move.
	current = current.Add(10 * time.Second)
	silenceAfter, lastFrameAfter := segmenter.IdleAudio()
	if silenceAfter != silenceBefore {
		t.Fatalf("silence advanced during frame stall: %s -> %s", silenceBefore, silenceAfter)
	}
	if !lastFrameAfter.Equal(lastFrameBefore) {
		t.Fatalf("lastFrame changed without frames: %s -> %s", lastFrameBefore, lastFrameAfter)
	}
}

func repeatProb(prob float32, n int) []float32 {
	values := make([]float32, n)
	for i := range values {
		values[i] = prob
	}
	return values
}

func framesForDuration(d time.Duration) int {
	frame := dictationFrameDuration()
	return int((d + frame - time.Nanosecond) / frame)
}

func repeatFrame(n int) []byte {
	pcm := make([]byte, n*dictationFrameBytes)
	for i := 0; i < n*dictationFrameSize; i++ {
		binary.LittleEndian.PutUint16(pcm[i*speechkit.AudioBytesPerSample:], uint16(i+1))
	}
	return pcm
}

func repeatMarkedFrame(n int, marker byte) []byte {
	pcm := make([]byte, n*dictationFrameBytes)
	for i := range pcm {
		pcm[i] = marker
	}
	return pcm
}

func dominantNonZeroByte(pcm []byte) byte {
	counts := map[byte]int{}
	var best byte
	for _, value := range pcm {
		if value == 0 {
			continue
		}
		counts[value]++
		if counts[value] > counts[best] {
			best = value
		}
	}
	return best
}
