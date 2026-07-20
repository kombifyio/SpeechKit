package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildWAV assembles a minimal PCM WAV, optionally inserting a LIST chunk
// between fmt and data to mirror the checked-in fixtures.
func buildWAV(t *testing.T, rate int, channels, bits uint16, data []byte, withList bool) []byte {
	t.Helper()
	var body bytes.Buffer
	body.WriteString("WAVE")

	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1) // PCM
	binary.LittleEndian.PutUint16(fmtChunk[2:4], channels)
	binary.LittleEndian.PutUint32(fmtChunk[4:8], uint32(rate))
	byteRate := uint32(rate) * uint32(channels) * uint32(bits/8)
	binary.LittleEndian.PutUint32(fmtChunk[8:12], byteRate)
	binary.LittleEndian.PutUint16(fmtChunk[12:14], channels*bits/8)
	binary.LittleEndian.PutUint16(fmtChunk[14:16], bits)
	writeChunk(&body, "fmt ", fmtChunk)

	if withList {
		writeChunk(&body, "LIST", []byte("INFOxxx")) // odd length exercises the pad-byte path
	}
	writeChunk(&body, "data", data)

	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func writeChunk(buf *bytes.Buffer, id string, payload []byte) {
	buf.WriteString(id)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(payload)))
	buf.Write(payload)
	if len(payload)%2 == 1 {
		buf.WriteByte(0) // word alignment pad
	}
}

func TestParseWAV(t *testing.T) {
	data := []byte{0x01, 0x00, 0x02, 0x00, 0x03, 0x00}

	t.Run("canonical 16k mono with LIST chunk", func(t *testing.T) {
		rate, pcm, err := parseWAV(buildWAV(t, 16000, 1, 16, data, true))
		if err != nil {
			t.Fatalf("parseWAV: %v", err)
		}
		if rate != 16000 {
			t.Errorf("rate = %d, want 16000", rate)
		}
		if !bytes.Equal(pcm, data) {
			t.Errorf("pcm = %v, want %v", pcm, data)
		}
	})

	t.Run("rejects stereo", func(t *testing.T) {
		if _, _, err := parseWAV(buildWAV(t, 16000, 2, 16, data, false)); err == nil {
			t.Fatal("expected error for 2-channel audio")
		}
	})

	t.Run("rejects 8-bit", func(t *testing.T) {
		if _, _, err := parseWAV(buildWAV(t, 16000, 1, 8, data, false)); err == nil {
			t.Fatal("expected error for 8-bit audio")
		}
	})

	t.Run("rejects non-RIFF", func(t *testing.T) {
		if _, _, err := parseWAV([]byte("not a wav file at all")); err == nil {
			t.Fatal("expected error for non-RIFF input")
		}
	})

	t.Run("rejects truncated chunk", func(t *testing.T) {
		raw := buildWAV(t, 16000, 1, 16, data, false)
		if _, _, err := parseWAV(raw[:len(raw)-3]); err == nil {
			t.Fatal("expected error for truncated data chunk")
		}
	})
}

func TestInferDownlinkRate(t *testing.T) {
	// ~2 s of speech at 24 kHz S16LE mono = 24000 * 2 * 2 = 96000 bytes, and
	// ~5 words at 2.5 w/s ≈ 2 s ground truth ⇒ 24 kHz must win.
	t.Run("transcript picks 24k", func(t *testing.T) {
		best, durs := inferDownlinkRate(96000, 5)
		if best != wsDownlinkRate {
			t.Fatalf("best = %d, want %d (durations=%v)", best, wsDownlinkRate, durs)
		}
	})

	// Same byte count but ~15 words (≈6 s) should match the slower 16 kHz
	// reading, proving the heuristic actually discriminates and would flag drift.
	t.Run("transcript detects 16k drift", func(t *testing.T) {
		best, _ := inferDownlinkRate(96000, 15)
		if best != wsUplinkRate {
			t.Fatalf("best = %d, want %d", best, wsUplinkRate)
		}
	})

	t.Run("no words defaults to 24k without inference", func(t *testing.T) {
		best, durs := inferDownlinkRate(96000, 0)
		if best != wsDownlinkRate {
			t.Fatalf("best = %d, want default %d", best, wsDownlinkRate)
		}
		if got := durs[wsDownlinkRate]; got != 2.0 {
			t.Fatalf("duration@24k = %.3f, want 2.0", got)
		}
	})
}

func TestCountWords(t *testing.T) {
	cases := map[string]int{"": 0, "one": 1, "  two  words  ": 2, "a b c d": 4}
	for in, want := range cases {
		if got := countWords(in); got != want {
			t.Errorf("countWords(%q) = %d, want %d", in, got, want)
		}
	}
}
