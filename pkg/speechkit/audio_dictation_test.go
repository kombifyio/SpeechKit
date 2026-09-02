package speechkit

import (
	"encoding/binary"
	"testing"
)

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
