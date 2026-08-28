package stt

import (
	"encoding/binary"

	audiofmt "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
)

// PCM16FromWAV extracts raw PCM16 sample data plus the sample rate and channel
// count from a LINEAR16 RIFF/WAVE byte slice. It returns ok=false when the
// input is not a 16-bit PCM WAV, in which case callers should treat the bytes
// as already-raw PCM and fall back to a default rate.
//
// Providers that must declare the sample rate explicitly (e.g. Google STT v1
// speech:recognize) rely on this to avoid sending a wrong hard-coded rate,
// which the provider rejects with HTTP 400 on a mismatch.
func PCM16FromWAV(audio []byte) (pcm []byte, sampleRate, channels int, ok bool) {
	if len(audio) < 12 || string(audio[0:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return nil, 0, 0, false
	}
	offset := 12
	var data []byte
	var rate, ch, bits int
	var foundFmt bool
	for offset+8 <= len(audio) {
		chunkID := string(audio[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(audio[offset+4 : offset+8]))
		offset += 8
		if size < 0 || offset+size > len(audio) {
			return nil, 0, 0, false
		}
		chunk := audio[offset : offset+size]
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, 0, false
			}
			format := binary.LittleEndian.Uint16(chunk[0:2])
			isPCM := format == 1
			if format == 0xfffe && len(chunk) >= 26 {
				isPCM = binary.LittleEndian.Uint16(chunk[24:26]) == 1
			}
			bits = int(binary.LittleEndian.Uint16(chunk[14:16]))
			if !isPCM || bits != 16 {
				return nil, 0, 0, false
			}
			ch = int(binary.LittleEndian.Uint16(chunk[2:4]))
			rate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			foundFmt = true
		case "data":
			data = chunk
		}
		offset += size
		if size%2 == 1 {
			offset++
		}
	}
	if !foundFmt || data == nil || rate <= 0 {
		return nil, 0, 0, false
	}
	return data, rate, ch, true
}

// MaxResponseBytes bounds how much of a provider's HTTP response an adapter
// reads. Transcription responses are text; anything larger is a
// misconfiguration or a hostile endpoint, not a longer transcript.
const MaxResponseBytes = 1 << 20

// EnsureTranscriptionWAV wraps raw PCM in a WAV header when it does not
// already carry one, because every HTTP transcription endpoint expects a
// container rather than bare samples.
func EnsureTranscriptionWAV(raw []byte) []byte {
	if IsWAV(raw) {
		return raw
	}
	return audiofmt.PCMToWAV(raw)
}

// IsWAV reports whether raw starts with a RIFF/WAVE header.
func IsWAV(raw []byte) bool {
	return len(raw) >= 12 &&
		string(raw[0:4]) == "RIFF" &&
		string(raw[8:12]) == "WAVE"
}
