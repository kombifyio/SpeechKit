//go:build linux

package audio

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	pcmaudio "github.com/kombifyio/SpeechKit/internal/audio"
)

// TestFixtureRoundtripPCMToWAV exercises the exact byte path the dictation
// handler takes when the local-only E2E gate runs against the OSS-mirrored
// docker-compose stack: read the fixture WAV → audio.Decode (this package) →
// internal/audio.PCMToWAV → assert the re-encoded WAV header is correct and
// re-decodable.
//
// Whisper-server in the failing v0.35.9 run rejected the upload with
// `error: failed to read audio data from (RIFFd...)`. This test isolates the
// in-process encode pipeline so we can tell whether the bug is here (PCM
// extraction or WAV header write) or downstream (multipart upload, whisper
// container expectations).
func TestFixtureRoundtripPCMToWAV(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/e2e/dictation/hello-world.wav")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	t.Logf("fixture: %d bytes, header=%x", len(raw), raw[:16])

	decoded, err := Decode(raw, "audio/wav")
	if err != nil {
		t.Fatalf("Decode fixture: %v", err)
	}
	t.Logf("decoded: PCM=%d bytes, srcRate=%d, srcCh=%d, durationMs=%d",
		len(decoded.PCM), decoded.SourceRate, decoded.SourceCh, decoded.DurationMs)

	if decoded.SourceRate != 16000 {
		t.Errorf("decoded SourceRate = %d, want 16000", decoded.SourceRate)
	}
	if decoded.SourceCh != 1 {
		t.Errorf("decoded SourceCh = %d, want 1", decoded.SourceCh)
	}
	if len(decoded.PCM)%2 != 0 {
		t.Errorf("decoded PCM length %d is not even (should be S16 samples)", len(decoded.PCM))
	}

	encoded := pcmaudio.PCMToWAV(decoded.PCM)
	t.Logf("re-encoded: %d bytes, header=%x", len(encoded), encoded[:44])

	// Verify the header bytes match what whisper-server expects.
	if !bytes.Equal(encoded[0:4], []byte("RIFF")) {
		t.Errorf("re-encoded magic = %q, want RIFF", encoded[0:4])
	}
	if !bytes.Equal(encoded[8:12], []byte("WAVE")) {
		t.Errorf("re-encoded format = %q, want WAVE", encoded[8:12])
	}
	if !bytes.Equal(encoded[12:16], []byte("fmt ")) {
		t.Errorf("re-encoded fmt-chunk-id = %q, want 'fmt '", encoded[12:16])
	}
	if !bytes.Equal(encoded[36:40], []byte("data")) {
		t.Errorf("re-encoded data-chunk-id = %q, want 'data'", encoded[36:40])
	}

	gotChannels := binary.LittleEndian.Uint16(encoded[22:24])
	gotSampleRate := binary.LittleEndian.Uint32(encoded[24:28])
	gotBPS := binary.LittleEndian.Uint16(encoded[34:36])
	gotDataSize := binary.LittleEndian.Uint32(encoded[40:44])
	gotRIFFSize := binary.LittleEndian.Uint32(encoded[4:8])

	t.Logf("re-encoded fmt: channels=%d sampleRate=%d bps=%d dataSize=%d RIFFSize=%d",
		gotChannels, gotSampleRate, gotBPS, gotDataSize, gotRIFFSize)

	if gotChannels != 1 {
		t.Errorf("re-encoded channels = %d, want 1", gotChannels)
	}
	if gotSampleRate != 16000 {
		t.Errorf("re-encoded sampleRate = %d, want 16000", gotSampleRate)
	}
	if gotBPS != 16 {
		t.Errorf("re-encoded bps = %d, want 16", gotBPS)
	}
	if int(gotDataSize) != len(decoded.PCM) {
		t.Errorf("re-encoded dataSize = %d, want %d (PCM len)", gotDataSize, len(decoded.PCM))
	}
	if int(gotRIFFSize) != 36+len(decoded.PCM) {
		t.Errorf("re-encoded RIFFSize = %d, want %d (36 + PCM)", gotRIFFSize, 36+len(decoded.PCM))
	}

	// Round-trip through our own decoder — if THIS fails, the encode
	// side is broken. If it passes, the byte stream is valid WAV per
	// our schema and the mismatch must be in whisper-server's
	// expectations or in the upload path (multipart wrapping, etc).
	redecoded, err := Decode(encoded, "audio/wav")
	if err != nil {
		t.Fatalf("re-Decode encoded WAV: %v", err)
	}
	t.Logf("re-decoded: PCM=%d bytes, srcRate=%d, srcCh=%d, durationMs=%d",
		len(redecoded.PCM), redecoded.SourceRate, redecoded.SourceCh, redecoded.DurationMs)

	if !bytes.Equal(redecoded.PCM, decoded.PCM) {
		t.Errorf("PCM bytes diverge after roundtrip: original=%d bytes, re-decoded=%d bytes",
			len(decoded.PCM), len(redecoded.PCM))
	}

	// Persist the encoded WAV so we can hand-feed it to whisper-server
	// out of band when needed for cross-validation.
	if err := os.WriteFile("/tmp/fixture-roundtrip.wav", encoded, 0o644); err != nil {
		t.Logf("could not persist roundtrip output: %v", err)
	} else {
		t.Logf("wrote roundtrip output to /tmp/fixture-roundtrip.wav")
	}
}
