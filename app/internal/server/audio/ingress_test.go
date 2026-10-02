//go:build linux

package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestDecode_WAV_16kHzMonoPassthrough(t *testing.T) {
	pcm := synthSine(TargetSampleRate, 1, 440.0, 500) // 500 ms tone
	wav := wrapWAV(pcm, TargetSampleRate, 1)
	got, err := Decode(wav, "audio/wav")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if got.SourceFormat != "wav" {
		t.Fatalf("SourceFormat = %q, want wav", got.SourceFormat)
	}
	if got.SourceRate != TargetSampleRate || got.SourceCh != 1 {
		t.Fatalf("SourceRate=%d SourceCh=%d, want %d/1", got.SourceRate, got.SourceCh, TargetSampleRate)
	}
	if len(got.PCM) != len(pcm) {
		t.Fatalf("PCM length changed: got %d want %d (16k mono should pass through)", len(got.PCM), len(pcm))
	}
	if got.DurationMs < 490 || got.DurationMs > 510 {
		t.Fatalf("DurationMs = %d, want ~500", got.DurationMs)
	}
}

func TestDecode_WAV_StereoDownmix(t *testing.T) {
	// Stereo: left channel = +1000, right channel = -1000 → average 0 (silence).
	const frames = TargetSampleRate / 2 // 500 ms
	pcm := make([]byte, frames*2*TargetBytesPerSample)
	left := int16(1000)
	right := int16(-1000)
	for f := 0; f < frames; f++ {
		binary.LittleEndian.PutUint16(pcm[f*4:], uint16(left))
		binary.LittleEndian.PutUint16(pcm[f*4+2:], uint16(right))
	}
	wav := wrapWAV(pcm, TargetSampleRate, 2)
	got, err := Decode(wav, "audio/wav")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(got.PCM) != frames*TargetBytesPerSample {
		t.Fatalf("mono output frames=%d, want %d", len(got.PCM)/TargetBytesPerSample, frames)
	}
	// Every mono sample should be approximately 0.
	maxAbs := int16(0)
	for i := 0; i+1 < len(got.PCM); i += 2 {
		s := int16(binary.LittleEndian.Uint16(got.PCM[i : i+2]))
		if s < 0 {
			s = -s
		}
		if s > maxAbs {
			maxAbs = s
		}
	}
	if maxAbs > 1 {
		t.Fatalf("stereo downmix should zero out opposite channels; max |sample| = %d", maxAbs)
	}
}

func TestDecode_WAV_ResampleTo16k(t *testing.T) {
	// 48 kHz source, 100 ms sine at 440 Hz, mono.
	pcm := synthSine(48000, 1, 440.0, 100)
	wav := wrapWAV(pcm, 48000, 1)
	got, err := Decode(wav, "audio/wav")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if got.SourceRate != 48000 {
		t.Fatalf("SourceRate should be reported as 48000, got %d", got.SourceRate)
	}
	// Expect 100 ms at 16 kHz = 1600 samples ±5 (linear resampler rounds).
	wantSamples := 1600
	gotSamples := len(got.PCM) / TargetBytesPerSample
	if gotSamples < wantSamples-8 || gotSamples > wantSamples+8 {
		t.Fatalf("resample sample count = %d, want ~%d", gotSamples, wantSamples)
	}
	// Duration ~100 ms.
	if got.DurationMs < 95 || got.DurationMs > 105 {
		t.Fatalf("DurationMs = %d, want ~100", got.DurationMs)
	}
}

func TestDecode_RawPCM_Passthrough(t *testing.T) {
	pcm := synthSine(TargetSampleRate, 1, 440.0, 250)
	got, err := Decode(pcm, "audio/L16;rate=16000;channels=1")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if got.SourceFormat != "pcm16" {
		t.Fatalf("SourceFormat = %q, want pcm16", got.SourceFormat)
	}
	if !bytes.Equal(got.PCM, pcm) {
		t.Fatalf("raw PCM should pass through unchanged")
	}
}

func TestDecodeWithLimits_RejectsDecodedAudioOverDurationLimit(t *testing.T) {
	pcm := synthSine(TargetSampleRate, 1, 440.0, 1100)
	_, err := DecodeWithLimits(context.Background(), pcm, "audio/L16;rate=16000;channels=1", DecodeLimits{MaxDecodedAudioSeconds: 1})
	if !errors.Is(err, ErrDecodedAudioTooLarge) {
		t.Fatalf("error = %v, want ErrDecodedAudioTooLarge", err)
	}

	var limitErr *DecodeLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %T, want *DecodeLimitError", err)
	}
	if limitErr.MaxSeconds != 1 || limitErr.ActualBytes <= limitErr.MaxBytes {
		t.Fatalf("limit error = %+v, want actual bytes over configured max", limitErr)
	}
}

func TestDecode_RawPCM_RejectsMisaligned(t *testing.T) {
	odd := []byte{1, 2, 3} // 3 bytes = not a multiple of 2
	_, err := Decode(odd, "audio/pcm")
	if err == nil {
		t.Fatalf("expected error for misaligned raw PCM")
	}
}

func TestDecode_EmptyFails(t *testing.T) {
	_, err := Decode(nil, "audio/wav")
	if err == nil {
		t.Fatalf("expected error for empty payload")
	}
}

func TestDecode_UnknownContentTypeFails(t *testing.T) {
	_, err := Decode([]byte{0x00, 0x01, 0x02, 0x03}, "application/json")
	if err == nil {
		t.Fatalf("expected ErrUnsupportedFormat for random bytes, got nil")
	}
}

func TestDecode_WAV_SniffedFromMagicBytes(t *testing.T) {
	// No Content-Type; rely on RIFF magic.
	pcm := synthSine(TargetSampleRate, 1, 440.0, 100)
	wav := wrapWAV(pcm, TargetSampleRate, 1)
	got, err := Decode(wav, "")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if got.SourceFormat != "wav" {
		t.Fatalf("SourceFormat sniffed from RIFF magic should be wav, got %q", got.SourceFormat)
	}
}

func TestDecode_WAV_RejectsNonPCMFormat(t *testing.T) {
	// Valid WAV header but declared format = 2 (ADPCM) → not supported.
	pcm := make([]byte, 320)
	wav := wrapWAV(pcm, TargetSampleRate, 1)
	// Overwrite audio format from 1 (PCM) → 2 (ADPCM).
	binary.LittleEndian.PutUint16(wav[20:22], 2)
	_, err := Decode(wav, "audio/wav")
	if err == nil {
		t.Fatalf("expected error for non-PCM WAV")
	}
}

func TestDecode_WAV_Rejects24bitDepth(t *testing.T) {
	pcm := make([]byte, 320)
	wav := wrapWAV(pcm, TargetSampleRate, 1)
	binary.LittleEndian.PutUint16(wav[34:36], 24)
	_, err := Decode(wav, "audio/wav")
	if err == nil {
		t.Fatalf("expected error for 24-bit WAV")
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// synthSine generates `ms` milliseconds of S16LE audio at `rate` with `ch`
// channels (same signal in every channel), frequency `hz`.
func synthSine(rate, ch int, hz float64, ms int) []byte {
	frames := rate * ms / 1000
	out := make([]byte, frames*ch*TargetBytesPerSample)
	for f := 0; f < frames; f++ {
		t := float64(f) / float64(rate)
		val := int16(math.Sin(2*math.Pi*hz*t) * 10000)
		for c := 0; c < ch; c++ {
			o := (f*ch + c) * TargetBytesPerSample
			binary.LittleEndian.PutUint16(out[o:], uint16(val))
		}
	}
	return out
}

// wrapWAV produces a canonical PCM WAV container around the given samples.
func wrapWAV(pcm []byte, rate, channels int) []byte {
	dataSize := uint32(len(pcm))
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], 36+dataSize)
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(rate*channels*TargetBytesPerSample))
	binary.LittleEndian.PutUint16(buf[32:], uint16(channels*TargetBytesPerSample))
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], dataSize)
	copy(buf[44:], pcm)
	return buf
}
