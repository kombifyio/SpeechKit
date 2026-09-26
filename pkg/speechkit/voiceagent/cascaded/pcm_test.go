package cascaded

import (
	"testing"
)

func TestChunkRMS(t *testing.T) {
	silent := silenceChunk(100)
	if rms := ChunkRMS(silent); rms != 0 {
		t.Fatalf("silent buffer RMS = %v, want 0", rms)
	}
	loud := sineChunk(100, 30000)
	if rms := ChunkRMS(loud); rms < 0.4 {
		t.Fatalf("loud sine RMS = %v, want >= 0.4", rms)
	}
}

func TestChunkAudio(t *testing.T) {
	out := ChunkAudio([]byte("1234567890"), 3)
	if len(out) != 4 {
		t.Fatalf("ChunkAudio split into %d chunks, want 4", len(out))
	}
	if string(out[3]) != "0" {
		t.Fatalf("last chunk = %q, want '0'", out[3])
	}
}

func TestPCMDurationMs(t *testing.T) {
	// 16 kHz S16 mono -> 32000 bytes per second.
	cases := []struct {
		bytes int
		ms    int64
	}{
		{0, 0},
		{32000, 1000},
		{16000, 500},
	}
	for _, c := range cases {
		got := PCMDurationMs(make([]byte, c.bytes))
		if got != c.ms {
			t.Fatalf("PCMDurationMs(%d bytes) = %d, want %d", c.bytes, got, c.ms)
		}
	}
}

// -- speaker stream reconnect ----------------------------------------
