package cascaded

import (
	"encoding/binary"
	"math"
)

// ChunkRMS computes the RMS level (0.0-1.0) of an S16LE PCM buffer.
// Matches the formula in internal/audio/pcm.PCMLevel; duplicated here to
// avoid pulling internal/audio into this package's import set.
func ChunkRMS(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	samples := len(pcm) / 2
	var sumSq float64
	for i := 0; i+1 < len(pcm); i += 2 {
		// #nosec G115 -- bit-pattern reinterpretation of an S16LE PCM
		// sample (uint16 -> int16). The encoding-binary.Uint16 path is
		// the only stdlib way to read a little-endian 16-bit value;
		// the cast preserves the bit pattern, which is what we want.
		s := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		v := float64(s) / 32768.0
		sumSq += v * v
	}
	level := math.Sqrt(sumSq / float64(samples))
	if level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}

// PCMDurationMs returns the duration of a 16 kHz S16 mono PCM buffer in
// milliseconds.
func PCMDurationMs(pcm []byte) int64 {
	return int64(len(pcm)) * 1000 / 32000
}

// ChunkAudio splits data into chunks of at most size bytes. Returns a
// single-element slice when data fits in one chunk.
func ChunkAudio(data []byte, size int) [][]byte {
	if size <= 0 || len(data) <= size {
		return [][]byte{data}
	}
	out := make([][]byte, 0, (len(data)+size-1)/size)
	for i := 0; i < len(data); i += size {
		end := i + size
		if end > len(data) {
			end = len(data)
		}
		out = append(out, data[i:end])
	}
	return out
}
