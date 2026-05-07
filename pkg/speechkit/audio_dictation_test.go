package speechkit

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"
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

func TestPCMToWAVWritesCanonicalHeaderAndDuration(t *testing.T) {
	pcm := []byte{0x01, 0x00, 0x02, 0x00}
	wav := PCMToWAV(pcm)

	if len(wav) != 44+len(pcm) {
		t.Fatalf("wav len = %d, want %d", len(wav), 44+len(pcm))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[36:40]) != "data" {
		t.Fatalf("invalid wav header: %q %q %q", wav[0:4], wav[8:12], wav[36:40])
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != AudioSampleRate {
		t.Fatalf("sample rate = %d, want %d", got, AudioSampleRate)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != AudioBitsPerSample {
		t.Fatalf("bits per sample = %d, want %d", got, AudioBitsPerSample)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(pcm)) {
		t.Fatalf("data size = %d, want %d", got, len(pcm))
	}
	if got := PCMDurationSecs(make([]byte, AudioSampleRate*AudioBytesPerSample)); got != 1 {
		t.Fatalf("one second PCM duration = %f", got)
	}
}

func TestDictationSegmenterEmitsSpeechSegmentAndResetsOnStop(t *testing.T) {
	probs := append([]float32{0, 0}, repeatProb(0.8, 40)...)
	probs = append(probs, repeatProb(0, 4)...)
	vad := &fakeVAD{probs: probs}
	segmenter := NewDictationSegmenter(vad, 64*time.Millisecond)
	if segmenter == nil {
		t.Fatal("expected segmenter")
	}

	if err := segmenter.FeedPCM(repeatFrame(46)); err != nil {
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
	if segments[0].Final {
		t.Fatal("pause-emitted segment should not be marked final")
	}
	if !vad.reset {
		t.Fatal("detector should reset when stop segments are collected")
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

func repeatProb(prob float32, n int) []float32 {
	values := make([]float32, n)
	for i := range values {
		values[i] = prob
	}
	return values
}

func repeatFrame(n int) []byte {
	pcm := make([]byte, n*dictationFrameBytes)
	for i := 0; i < n*dictationFrameSize; i++ {
		binary.LittleEndian.PutUint16(pcm[i*AudioBytesPerSample:], uint16(i+1))
	}
	return pcm
}
