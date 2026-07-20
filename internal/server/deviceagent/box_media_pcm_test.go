package deviceagent

import (
	"encoding/binary"
	"testing"
)

func TestParseBoxMediaWAVRejectsInconsistentOrTrailingData(t *testing.T) {
	valid := boxTestWAV(boxPCM16LE(160), 16000)
	for name, mutate := range map[string]func([]byte) []byte{
		"inconsistent byte rate": func(raw []byte) []byte {
			binary.LittleEndian.PutUint32(raw[28:32], 1)
			return raw
		},
		"inconsistent block alignment": func(raw []byte) []byte {
			binary.LittleEndian.PutUint16(raw[32:34], 4)
			return raw
		},
		"unaligned PCM": func(raw []byte) []byte {
			raw = raw[:len(raw)-1]
			binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))    // #nosec G115 -- bounded test fixture.
			binary.LittleEndian.PutUint32(raw[40:44], uint32(len(raw)-44)) // #nosec G115 -- bounded test fixture.
			return raw
		},
		"trailing byte": func(raw []byte) []byte {
			raw = append(raw, 0)
			binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8)) // #nosec G115 -- bounded test fixture.
			return raw
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw := mutate(append([]byte(nil), valid...))
			if _, _, _, _, err := parseBoxMediaWAV(raw); err == nil {
				t.Fatal("malformed WAV was accepted")
			}
		})
	}
}

func TestBoxMediaPCMSampleByteOrderAndResampling(t *testing.T) {
	l16 := []byte{0x12, 0x34, 0xfe, 0xdc}
	pcm := l16ToPCM16LE(l16)
	if want := []byte{0x34, 0x12, 0xdc, 0xfe}; string(pcm) != string(want) {
		t.Fatalf("PCM bytes=%x, want %x", pcm, want)
	}
	if roundTrip := pcm16LEToL16(pcm); string(roundTrip) != string(l16) {
		t.Fatalf("L16 roundtrip=%x, want %x", roundTrip, l16)
	}
	resampled, err := resamplePCM16Mono(boxPCM16LE(1600), 16000, 48000)
	if err != nil {
		t.Fatalf("resample: %v", err)
	}
	if len(resampled) != 4800*2 {
		t.Fatalf("resampled bytes=%d, want %d", len(resampled), 4800*2)
	}
}
