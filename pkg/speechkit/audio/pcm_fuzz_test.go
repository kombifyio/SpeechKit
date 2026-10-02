package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func FuzzPCMFraming(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x01})
	f.Add([]byte{0x00, 0x80, 0xff, 0x7f})
	f.Add(bytes.Repeat([]byte{0x10, 0x20}, 1600))

	f.Fuzz(func(t *testing.T, pcm []byte) {
		wav := PCMToWAV(pcm)

		if len(wav) != 44+len(pcm) {
			t.Fatalf("wav length %d, want %d", len(wav), 44+len(pcm))
		}
		if !bytes.Equal(wav[44:], pcm) {
			t.Fatal("wav payload does not round-trip the PCM input")
		}
		if got := binary.LittleEndian.Uint32(wav[40:]); int(got) != len(pcm) {
			t.Fatalf("data chunk size %d, want %d", got, len(pcm))
		}
		if got := binary.LittleEndian.Uint32(wav[4:]); int(got) != 36+len(pcm) {
			t.Fatalf("riff size %d, want %d", got, 36+len(pcm))
		}

		if d := PCMDurationSecs(pcm); d < 0 || math.IsNaN(d) {
			t.Fatalf("invalid duration %v", d)
		}
		if l := PCMLevel(pcm); l < 0 || l > 1 || math.IsNaN(l) {
			t.Fatalf("level %v outside [0,1]", l)
		}
	})
}
