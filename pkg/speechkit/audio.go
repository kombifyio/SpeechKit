package speechkit

import "encoding/binary"

const (
	AudioSampleRate     = 16000
	AudioChannels       = 1
	AudioBitsPerSample  = 16
	AudioBytesPerSample = AudioBitsPerSample / 8
)

// PCMToWAV wraps raw 16kHz S16 mono PCM data in a WAV header.
func PCMToWAV(pcm []byte) []byte {
	dataSize := uint32(len(pcm)) //nolint:gosec // PCM buffers are bounded by capture duration.
	out := make([]byte, 44+len(pcm))

	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], 36+dataSize)
	copy(out[8:], "WAVE")

	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], AudioChannels)
	binary.LittleEndian.PutUint32(out[24:], AudioSampleRate)
	binary.LittleEndian.PutUint32(out[28:], AudioSampleRate*AudioChannels*AudioBytesPerSample)
	binary.LittleEndian.PutUint16(out[32:], AudioChannels*AudioBytesPerSample)
	binary.LittleEndian.PutUint16(out[34:], AudioBitsPerSample)

	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], dataSize)
	copy(out[44:], pcm)

	return out
}

// PCMDurationSecs returns the duration of 16kHz S16 mono PCM audio in seconds.
func PCMDurationSecs(pcm []byte) float64 {
	samples := len(pcm) / AudioBytesPerSample
	return float64(samples) / float64(AudioSampleRate)
}
