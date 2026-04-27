//go:build linux

package audio

import (
	"bytes"
	"os/exec"
	"testing"
)

// requireFFmpeg skips the test when ffmpeg is not on PATH. The Server-Target
// Dockerfile installs ffmpeg, and the server-linux CI workflow mirrors the
// install. Bare-metal dev environments without ffmpeg get a clear skip
// rather than a confusing failure.
func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skipf("ffmpeg not on PATH: %v (install ffmpeg locally or run via the Dockerfile)", err)
	}
}

// encodeWithFFmpeg pipes raw 16 kHz S16LE mono PCM through ffmpeg into the
// given container/codec. Returns the encoded bytes.
func encodeWithFFmpeg(t *testing.T, pcm []byte, format, codec string) []byte {
	t.Helper()
	cmd := exec.Command("ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-f", "s16le",
		"-ar", "16000",
		"-ac", "1",
		"-i", "pipe:0",
		"-c:a", codec,
		"-f", format,
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(pcm)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("encodeWithFFmpeg(%s/%s): %v; stderr=%s", format, codec, err, stderr.String())
	}
	if out.Len() == 0 {
		t.Fatalf("encodeWithFFmpeg(%s/%s) produced 0 bytes", format, codec)
	}
	return out.Bytes()
}

func TestDecode_WebMOpus_RoundTrip(t *testing.T) {
	requireFFmpeg(t)

	// 500 ms of 440 Hz tone — long enough that opus produces a real
	// payload but small enough that the test runs fast.
	srcPCM := synthSine(TargetSampleRate, 1, 440.0, 500)
	encoded := encodeWithFFmpeg(t, srcPCM, "webm", "libopus")

	got, err := Decode(encoded, "audio/webm;codecs=opus")
	if err != nil {
		t.Fatalf("Decode webm/opus: %v", err)
	}
	if got.SourceFormat != "webm" {
		t.Fatalf("SourceFormat = %q, want webm", got.SourceFormat)
	}
	if got.SourceRate != TargetSampleRate || got.SourceCh != TargetChannels {
		t.Fatalf("Source rate/ch = %d/%d, want %d/%d", got.SourceRate, got.SourceCh, TargetSampleRate, TargetChannels)
	}
	// Duration tolerance of ±50 ms — opus encoder pads to its frame
	// boundaries (20 ms typical) and resampling rounds.
	if got.DurationMs < 450 || got.DurationMs > 550 {
		t.Fatalf("DurationMs = %d, want ~500", got.DurationMs)
	}
	// PCM should be ~16 000 samples = 32 000 bytes.
	if len(got.PCM) < 16000 || len(got.PCM) > 48000 {
		t.Fatalf("PCM length = %d bytes, expected ~32 000", len(got.PCM))
	}
}

func TestDecode_OGGOpus_RoundTrip(t *testing.T) {
	requireFFmpeg(t)
	srcPCM := synthSine(TargetSampleRate, 1, 440.0, 250)
	encoded := encodeWithFFmpeg(t, srcPCM, "ogg", "libopus")
	got, err := Decode(encoded, "audio/ogg;codecs=opus")
	if err != nil {
		t.Fatalf("Decode ogg/opus: %v", err)
	}
	if got.SourceFormat != "ogg" {
		t.Fatalf("SourceFormat = %q, want ogg", got.SourceFormat)
	}
	if got.DurationMs < 200 || got.DurationMs > 300 {
		t.Fatalf("DurationMs = %d, want ~250", got.DurationMs)
	}
}

func TestDecode_WebM_SniffedFromMagicBytes(t *testing.T) {
	requireFFmpeg(t)
	srcPCM := synthSine(TargetSampleRate, 1, 440.0, 100)
	encoded := encodeWithFFmpeg(t, srcPCM, "webm", "libopus")

	got, err := Decode(encoded, "")
	if err != nil {
		t.Fatalf("Decode webm sniffed: %v", err)
	}
	if got.SourceFormat != "webm" {
		t.Fatalf("Sniffed SourceFormat = %q, want webm", got.SourceFormat)
	}
}

func TestDecode_OGG_SniffedFromMagicBytes(t *testing.T) {
	requireFFmpeg(t)
	srcPCM := synthSine(TargetSampleRate, 1, 440.0, 100)
	encoded := encodeWithFFmpeg(t, srcPCM, "ogg", "libopus")

	got, err := Decode(encoded, "")
	if err != nil {
		t.Fatalf("Decode ogg sniffed: %v", err)
	}
	if got.SourceFormat != "ogg" {
		t.Fatalf("Sniffed SourceFormat = %q, want ogg", got.SourceFormat)
	}
}

func TestDecode_FFmpegUnavailableReturnsClearError(t *testing.T) {
	// Override the binary to one that doesn't exist; the Decode path
	// should detect this via exec.LookPath and return ErrFFmpegUnavailable
	// rather than crashing or returning a generic error.
	original := ffmpegBinary
	t.Cleanup(func() { ffmpegBinary = original })
	ffmpegBinary = "ffmpeg-this-will-never-exist-on-PATH"

	_, err := Decode([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00, 0x00, 0x00}, "audio/webm")
	if err == nil {
		t.Fatalf("expected error when ffmpeg is unavailable")
	}
	if err != ErrFFmpegUnavailable {
		t.Fatalf("expected ErrFFmpegUnavailable, got %v", err)
	}
}
