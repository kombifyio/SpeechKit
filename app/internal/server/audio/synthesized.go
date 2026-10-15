//go:build linux

package audio

import skAudio "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"

// SynthesizedToPCM16Mono shares the public SDK speech-output normalization.
func SynthesizedToPCM16Mono(raw []byte, format string, sourceRate, dstRate int) ([]byte, error) {
	return skAudio.SynthesizedToPCM16Mono(raw, format, sourceRate, dstRate)
}
