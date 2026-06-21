package audio

import "testing"

func TestPCMToWAVHeader(t *testing.T) {
	pcm := make([]byte, 32000) // 1s @ 16 kHz mono S16
	wav := PCMToWAV(pcm)
	if len(wav) != 44+len(pcm) {
		t.Fatalf("len = %d, want %d", len(wav), 44+len(pcm))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[36:40]) != "data" {
		t.Fatalf("bad WAV header: %q %q %q", wav[0:4], wav[8:12], wav[36:40])
	}
}

func TestPCMDurationSecs(t *testing.T) {
	pcm := make([]byte, SampleRate*BytesPerSample) // exactly one second
	if got := PCMDurationSecs(pcm); got != 1.0 {
		t.Fatalf("duration = %v, want 1.0", got)
	}
}

func TestPCMLevelBounds(t *testing.T) {
	if got := PCMLevel(nil); got != 0 {
		t.Fatalf("level(nil) = %v, want 0", got)
	}
	pcm := []byte{0xff, 0x7f, 0xff, 0x7f} // near full-scale S16LE samples
	if got := PCMLevel(pcm); got < 0 || got > 1 {
		t.Fatalf("level out of [0,1]: %v", got)
	}
}
