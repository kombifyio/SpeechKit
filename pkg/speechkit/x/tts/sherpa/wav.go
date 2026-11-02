package sherpa

import (
	"encoding/binary"
	"math"
)

// WAVSampleRate is the rate the bundled Piper medium voices emit.
const WAVSampleRate = 22050

// EncodeWAV wraps mono float32 samples in [-1, 1] as a 16-bit PCM WAV.
func EncodeWAV(samples []float32, sampleRate int) []byte {
	if sampleRate <= 0 {
		sampleRate = WAVSampleRate
	}
	const (
		channels   = 1
		bits       = 16
		blockAlign = channels * bits / 8
	)
	dataBytes := len(samples) * blockAlign
	buf := make([]byte, 44+dataBytes)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataBytes))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:24], channels)
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(sampleRate*blockAlign))
	binary.LittleEndian.PutUint16(buf[32:34], blockAlign)
	binary.LittleEndian.PutUint16(buf[34:36], bits)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataBytes))
	for i, sample := range samples {
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		binary.LittleEndian.PutUint16(buf[44+i*2:46+i*2], uint16(int16(math.Round(float64(sample)*32767))))
	}
	return buf
}
