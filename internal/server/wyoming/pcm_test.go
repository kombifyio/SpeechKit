package wyoming

import (
	"bytes"
	"testing"
)

func TestPCMToWAVRoundTrip(t *testing.T) {
	pcm := make([]byte, 640) // 20 ms @ 16 kHz mono S16LE
	for i := range pcm {
		pcm[i] = byte(i % 251)
	}
	wav := pcmToWAV(pcm, 16000, 1, 2)

	got, rate, channels, width, err := parseWAV(wav)
	if err != nil {
		t.Fatalf("parseWAV: %v", err)
	}
	if rate != 16000 || channels != 1 || width != 2 {
		t.Errorf("fmt = rate %d ch %d width %d, want 16000/1/2", rate, channels, width)
	}
	if !bytes.Equal(got, pcm) {
		t.Error("round-tripped PCM does not match the source")
	}
}

func TestParseWAVStereo48k(t *testing.T) {
	pcm := make([]byte, 4*100) // 100 stereo S16 frames
	wav := pcmToWAV(pcm, 48000, 2, 2)
	got, rate, channels, width, err := parseWAV(wav)
	if err != nil {
		t.Fatalf("parseWAV: %v", err)
	}
	if rate != 48000 || channels != 2 || width != 2 {
		t.Errorf("fmt = rate %d ch %d width %d, want 48000/2/2", rate, channels, width)
	}
	if len(got) != len(pcm) {
		t.Errorf("data len = %d, want %d", len(got), len(pcm))
	}
}

func TestParseWAVRejectsNonRIFF(t *testing.T) {
	if _, _, _, _, err := parseWAV([]byte("not a wav at all")); err == nil {
		t.Fatal("expected error for non-RIFF input")
	}
}
