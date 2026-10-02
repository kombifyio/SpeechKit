package audio

import (
	"encoding/binary"
	"testing"
)

func putSample(pcm []byte, offset int, sample int16) {
	binary.LittleEndian.PutUint16(pcm[offset:offset+2], uint16(sample))
}

func TestPCMToWAV_Header(t *testing.T) {
	pcm := make([]byte, 32000) // 1 second at 16kHz S16 Mono
	wav := PCMToWAV(pcm)

	if len(wav) != 44+len(pcm) {
		t.Fatalf("expected %d bytes, got %d", 44+len(pcm), len(wav))
	}

	// RIFF header
	if string(wav[0:4]) != "RIFF" {
		t.Fatal("missing RIFF header")
	}
	if string(wav[8:12]) != "WAVE" {
		t.Fatal("missing WAVE marker")
	}
	if string(wav[12:16]) != "fmt " {
		t.Fatal("missing fmt chunk")
	}
	if string(wav[36:40]) != "data" {
		t.Fatal("missing data chunk")
	}
}

func TestPCMToWAV_EmptyInput(t *testing.T) {
	wav := PCMToWAV(nil)
	if len(wav) != 44 {
		t.Fatalf("empty PCM should produce 44-byte WAV header, got %d", len(wav))
	}
}

func TestPCMDurationSecs(t *testing.T) {
	tests := []struct {
		name     string
		pcmLen   int
		expected float64
	}{
		{"1 second", SampleRate * BytesPerSample, 1.0},
		{"0.5 seconds", SampleRate * BytesPerSample / 2, 0.5},
		{"empty", 0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pcm := make([]byte, tt.pcmLen)
			got := PCMDurationSecs(pcm)
			if got != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, got)
			}
		})
	}
}

func TestPCMLevel(t *testing.T) {
	t.Run("silence", func(t *testing.T) {
		if got := PCMLevel(make([]byte, 32)); got != 0 {
			t.Fatalf("silence level = %f, want 0", got)
		}
	})

	t.Run("mid level", func(t *testing.T) {
		pcm := make([]byte, 4)
		putSample(pcm, 0, 16384)
		putSample(pcm, 2, -16384)
		if got := PCMLevel(pcm); got < 0.45 || got > 0.55 {
			t.Fatalf("mid level = %f, want about 0.5", got)
		}
	})

	t.Run("clamped", func(t *testing.T) {
		pcm := make([]byte, 4)
		putSample(pcm, 0, 32767)
		putSample(pcm, 2, -32768)
		if got := PCMLevel(pcm); got < 0.99 || got > 1.0 {
			t.Fatalf("max level = %f, want about 1.0", got)
		}
	})
}

func TestPCMToWAV_FieldValues(t *testing.T) {
	pcm := make([]byte, 640) // 20ms at 16kHz S16 Mono
	wav := PCMToWAV(pcm)

	tests := []struct {
		name   string
		offset int
		size   int // 2 or 4 bytes
		want   uint32
	}{
		{"format (PCM)", 20, 2, 1},
		{"channels", 22, 2, 1},
		{"sample rate", 24, 4, 16000},
		{"byte rate", 28, 4, 32000},
		{"block align", 32, 2, 2},
		{"bits per sample", 34, 2, 16},
		{"data size", 40, 4, uint32(len(pcm))},
		{"RIFF size", 4, 4, 36 + uint32(len(pcm))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got uint32
			switch tt.size {
			case 2:
				got = uint32(binary.LittleEndian.Uint16(wav[tt.offset : tt.offset+2]))
			case 4:
				got = binary.LittleEndian.Uint32(wav[tt.offset : tt.offset+4])
			}
			if got != tt.want {
				t.Fatalf("offset %d: got %d, want %d", tt.offset, got, tt.want)
			}
		})
	}
}

func TestPCMToWAV_DataPreserved(t *testing.T) {
	pcm := make([]byte, 8)
	putSample(pcm, 0, 1234)
	putSample(pcm, 2, -5678)
	putSample(pcm, 4, 32767)
	putSample(pcm, 6, -32768)

	wav := PCMToWAV(pcm)
	data := wav[44:]

	if len(data) != len(pcm) {
		t.Fatalf("data section length: got %d, want %d", len(data), len(pcm))
	}
	for i := range pcm {
		if data[i] != pcm[i] {
			t.Fatalf("byte %d: got %d, want %d", i, data[i], pcm[i])
		}
	}
}

func TestPCMLevel_EmptyAndShort(t *testing.T) {
	tests := []struct {
		name string
		pcm  []byte
	}{
		{"nil input", nil},
		{"empty slice", []byte{}},
		{"single byte", []byte{0x42}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PCMLevel(tt.pcm); got != 0 {
				t.Fatalf("got %f, want 0", got)
			}
		})
	}
}

func TestPCMLevel_SingleSample(t *testing.T) {
	pcm := make([]byte, 2)
	putSample(pcm, 0, 16384)

	// RMS of one sample: |16384/32768| = 0.5
	got := PCMLevel(pcm)
	if got != 0.5 {
		t.Fatalf("got %f, want 0.5", got)
	}
}

func TestPCMDurationSecs_OddLength(t *testing.T) {
	pcm := make([]byte, 3) // 3 bytes = 1 full sample (2 bytes), 1 byte remainder truncated
	got := PCMDurationSecs(pcm)
	want := 1.0 / float64(SampleRate)
	if got != want {
		t.Fatalf("got %f, want %f", got, want)
	}
}
