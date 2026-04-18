package audio

import (
	"encoding/binary"
	"math"
)

const (
	SampleRate     = 16000
	Channels       = 1
	BitsPerSample  = 16
	BytesPerSample = BitsPerSample / 8
)

// PCMToWAV wraps raw 16kHz S16 Mono PCM data in a WAV header.
func PCMToWAV(pcm []byte) []byte {
	dataSize := uint32(len(pcm)) //nolint:gosec // Windows API integer conversion, value fits
	out := make([]byte, 44+len(pcm))

	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], 36+dataSize)
	copy(out[8:], "WAVE")

	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], Channels)
	binary.LittleEndian.PutUint32(out[24:], SampleRate)
	binary.LittleEndian.PutUint32(out[28:], SampleRate*Channels*BytesPerSample)
	binary.LittleEndian.PutUint16(out[32:], Channels*BytesPerSample)
	binary.LittleEndian.PutUint16(out[34:], BitsPerSample)

	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], dataSize)
	copy(out[44:], pcm)

	return out
}

// PCMDurationSecs returns the duration of PCM audio in seconds.
func PCMDurationSecs(pcm []byte) float64 {
	samples := len(pcm) / BytesPerSample
	return float64(samples) / float64(SampleRate)
}

// PCMLevel estimates a normalized RMS level from 16-bit PCM samples.
func PCMLevel(pcm []byte) float64 {
	if len(pcm) < BytesPerSample {
		return 0
	}

	sampleCount := len(pcm) / BytesPerSample
	if sampleCount == 0 {
		return 0
	}

	var sumSquares float64
	for i := 0; i+1 < len(pcm); i += BytesPerSample {
		sample := int16(binary.LittleEndian.Uint16(pcm[i : i+2])) //nolint:gosec // Windows API integer conversion, value fits
		normalized := float64(sample) / 32768.0
		sumSquares += normalized * normalized
	}

	level := math.Sqrt(sumSquares / float64(sampleCount))
	if level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}
