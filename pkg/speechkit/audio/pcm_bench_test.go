package audio

import "testing"

// benchPCM returns one second of a deterministic 16 kHz S16 mono ramp, the
// size of a typical capture chunk handed to level meters and WAV framing.
func benchPCM() []byte {
	pcm := make([]byte, SampleRate*BytesPerSample)
	for i := 0; i+1 < len(pcm); i += BytesPerSample {
		v := uint16(i/BytesPerSample%2000) + 32000
		pcm[i] = byte(v)
		pcm[i+1] = byte(v >> 8)
	}
	return pcm
}

// BenchmarkPCMLevel measures RMS level math, run on every captured chunk to
// drive the recording overlay.
func BenchmarkPCMLevel(b *testing.B) {
	pcm := benchPCM()
	b.ReportAllocs()
	b.SetBytes(int64(len(pcm)))
	for b.Loop() {
		_ = PCMLevel(pcm)
	}
}

// BenchmarkPCMToWAV measures WAV framing of one second of capture, run before
// every STT upload.
func BenchmarkPCMToWAV(b *testing.B) {
	pcm := benchPCM()
	b.ReportAllocs()
	b.SetBytes(int64(len(pcm)))
	for b.Loop() {
		_ = PCMToWAV(pcm)
	}
}

// BenchmarkPCMDurationSecs measures the duration math the router uses to pick
// between local and cloud providers.
func BenchmarkPCMDurationSecs(b *testing.B) {
	pcm := benchPCM()
	b.ReportAllocs()
	for b.Loop() {
		_ = PCMDurationSecs(pcm)
	}
}
