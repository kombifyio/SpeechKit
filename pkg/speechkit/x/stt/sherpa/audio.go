package sherpa

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

// SampleRate is the rate every model of this package expects.
const SampleRate = 16000

// DecodeWAV turns a 16-bit PCM WAV of any rate and channel count into 16 kHz
// mono samples in [-1, 1].
func DecodeWAV(raw []byte) ([]float32, error) {
	pcm, rate, channels, err := audio.ParseWAV(raw)
	if err != nil {
		return nil, err
	}
	// A writer that stopped mid-frame leaves a stray byte or two; drop them
	// rather than refuse the recording.
	if frame := 2 * channels; frame > 0 {
		pcm = pcm[:len(pcm)-len(pcm)%frame]
	}
	if len(pcm) == 0 {
		return nil, nil
	}
	mono, err := audio.NormalizePCM16Mono(pcm, rate, channels, SampleRate)
	if err != nil {
		return nil, fmt.Errorf("sherpa: %w", err)
	}
	samples := make([]float32, len(mono)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(mono[2*i:]))) / 32768
	}
	return samples, nil
}

// Span is a half-open sample range [Start, End) of a recording.
type Span struct {
	Start int
	End   int
}

// Segmentation bounds. Parakeet's int8 export transcribes up to about 30
// seconds reliably in one pass; longer passes cost quadratic attention memory
// and drop words. Recordings are cut at the quietest point between
// minSegmentSeconds and maxSegmentSeconds into each segment.
const (
	maxSegmentSeconds = 28
	minSegmentSeconds = 15
	frameMillis       = 20
	// quietWindowFrames smooths the frame energy over 200 ms so a cut lands
	// in a pause rather than between two syllables.
	quietWindowFrames = 10
)

// SplitAtPauses returns the spans a recording of n samples at SampleRate is
// decoded in: one span when it fits a single pass, otherwise consecutive spans
// of at most maxSegmentSeconds, each ending at the quietest 200 ms window
// after minSegmentSeconds.
func SplitAtPauses(samples []float32) []Span {
	maxLen := maxSegmentSeconds * SampleRate
	if len(samples) <= maxLen {
		return []Span{{Start: 0, End: len(samples)}}
	}
	frameLen := SampleRate * frameMillis / 1000
	energy := frameEnergy(samples, frameLen)
	var spans []Span
	start := 0
	for len(samples)-start > maxLen {
		cut := quietestCut(energy, frameLen, start+minSegmentSeconds*SampleRate, start+maxLen)
		spans = append(spans, Span{Start: start, End: cut})
		start = cut
	}
	return append(spans, Span{Start: start, End: len(samples)})
}

// frameEnergy is the mean square of each frameLen-sample frame.
func frameEnergy(samples []float32, frameLen int) []float64 {
	n := (len(samples) + frameLen - 1) / frameLen
	energy := make([]float64, n)
	for f := range energy {
		lo, hi := f*frameLen, min((f+1)*frameLen, len(samples))
		var sum float64
		for _, s := range samples[lo:hi] {
			sum += float64(s) * float64(s)
		}
		energy[f] = sum / float64(hi-lo)
	}
	return energy
}

// quietestCut returns the sample index, between lo and hi, at the center of
// the quietest quietWindowFrames window.
func quietestCut(energy []float64, frameLen, lo, hi int) int {
	firstFrame := lo / frameLen
	lastFrame := min(hi/frameLen, len(energy)) - quietWindowFrames
	best, bestEnergy := -1, math.Inf(1)
	for f := firstFrame; f <= lastFrame; f++ {
		var sum float64
		for _, e := range energy[f : f+quietWindowFrames] {
			sum += e
		}
		if sum < bestEnergy {
			best, bestEnergy = f, sum
		}
	}
	if best < 0 {
		return hi
	}
	return min((best+quietWindowFrames/2)*frameLen, hi)
}
