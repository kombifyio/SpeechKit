//go:build linux

// Package audio normalizes inbound audio payloads to the Framework kernel's
// canonical PCM format (16 kHz, signed 16-bit little-endian, mono) before
// they enter the STT router. Every client format that the server accepts is
// converted here; the kernel itself never sees raw MP3, Opus, or stereo input.
package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

// TargetSampleRate is the canonical rate every STT provider expects.
const TargetSampleRate = 16000

// TargetChannels is the canonical channel count (mono) used by every STT provider.
const TargetChannels = 1

// TargetBytesPerSample is 16-bit signed little-endian.
const TargetBytesPerSample = 2

// Supported MIME types keyed by normalized lowercase media type. This list
// drives both Content-Type sniffing and the /v1/admin/models capability
// response later on.
var supportedMimeTypes = map[string]string{
	"audio/wav":                "wav",
	"audio/x-wav":              "wav",
	"audio/wave":               "wav",
	"audio/mpeg":               "mp3",
	"audio/mp3":                "mp3",
	"audio/x-mp3":              "mp3",
	"audio/l16":                "pcm16",
	"audio/pcm":                "pcm16",
	"audio/raw":                "pcm16",
	"application/octet-stream": "pcm16", // common default when client is uncertain
	// Browser-default formats. The MediaRecorder API in Chromium emits
	// audio/webm;codecs=opus; Firefox emits audio/ogg;codecs=opus. Both
	// route through ffmpeg to produce canonical PCM.
	"audio/webm": "webm",
	"audio/ogg":  "ogg",
	"video/webm": "webm", // some browsers tag muxed containers as video/*
	"audio/opus": "ogg",  // bare opus payloads are conventionally OGG-Opus
}

// ErrUnsupportedFormat is returned when neither the Content-Type nor the
// payload's magic bytes identify a format we can decode.
var ErrUnsupportedFormat = errors.New("audio: unsupported format (supported: wav, mp3, raw pcm16 @ 16kHz mono, webm/opus, ogg/opus)")

// ErrDecodedAudioTooLarge is wrapped by DecodeLimitError when a compressed
// payload expands beyond the configured decoded-duration ceiling.
var ErrDecodedAudioTooLarge = errors.New("audio: decoded audio exceeds configured duration limit")

// ErrFFmpegUnavailable is returned when a payload requires ffmpeg but the
// binary is not on PATH. Container deployments include ffmpeg; bare-metal
// dev environments may need to install it.
var ErrFFmpegUnavailable = errors.New("audio: ffmpeg not available — install ffmpeg or send WAV/MP3/raw-PCM instead")

// DecodeLimits bounds resource amplification after compressed/container audio
// is decoded to PCM. Zero values preserve the historical unbounded behavior
// for internal callers; server handlers pass the production limit from config.
type DecodeLimits struct {
	MaxDecodedAudioSeconds int
}

// DecodeLimitError carries the configured ceiling and observed decoded
// size/duration for callers that need to map the failure to HTTP 413.
type DecodeLimitError struct {
	MaxSeconds  int
	DurationMs  int64
	MaxBytes    int64
	ActualBytes int64
}

func (e *DecodeLimitError) Error() string {
	if e == nil {
		return ErrDecodedAudioTooLarge.Error()
	}
	if e.DurationMs > 0 {
		return fmt.Sprintf("audio: decoded duration %dms exceeds maximum %ds", e.DurationMs, e.MaxSeconds)
	}
	return fmt.Sprintf("audio: decoded audio %d bytes exceeds maximum %d bytes", e.ActualBytes, e.MaxBytes)
}

func (e *DecodeLimitError) Unwrap() error { return ErrDecodedAudioTooLarge }

// DecodedAudio is the canonical 16 kHz, mono, S16LE PCM payload plus the
// metadata recovered from the source.
type DecodedAudio struct {
	PCM          []byte
	SourceFormat string
	SourceRate   int
	SourceCh     int
	DurationMs   int64
}

// Decode consumes raw bytes and a client-provided Content-Type and returns a
// canonical DecodedAudio ready for the STT router. contentType may be empty;
// in that case magic-byte sniffing is attempted.
func Decode(raw []byte, contentType string) (*DecodedAudio, error) {
	return DecodeWithLimits(context.Background(), raw, contentType, DecodeLimits{})
}

// DecodeWithLimits consumes raw bytes and enforces optional decoded-audio
// limits while normalizing to canonical PCM.
func DecodeWithLimits(ctx context.Context, raw []byte, contentType string, limits DecodeLimits) (*DecodedAudio, error) {
	if len(raw) == 0 {
		return nil, errors.New("audio: empty payload")
	}

	kind := detectFormat(raw, contentType)
	switch kind {
	case "wav":
		return decodeWAV(raw, limits)
	case "mp3":
		return decodeMP3(raw, limits)
	case "pcm16":
		// Bare PCM is only acceptable when the client explicitly labels it
		// — we can't sniff it from bytes alone without false positives.
		return decodeRawPCM(raw, limits)
	case "webm":
		return decodeViaFFmpeg(ctx, raw, "webm", "matroska,webm", limits)
	case "ogg":
		return decodeViaFFmpeg(ctx, raw, "ogg", "ogg", limits)
	default:
		return nil, ErrUnsupportedFormat
	}
}

// DecodeReader is a convenience wrapper that buffers a reader (capped at
// maxBytes) and then invokes Decode.
func DecodeReader(r io.Reader, contentType string, maxBytes int64) (*DecodedAudio, error) {
	lr := io.LimitReader(r, maxBytes+1)
	buf, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("audio: read payload: %w", err)
	}
	if int64(len(buf)) > maxBytes {
		return nil, fmt.Errorf("audio: payload exceeds %d bytes", maxBytes)
	}
	return Decode(buf, contentType)
}

// detectFormat resolves a normalized format string from Content-Type + sniffing.
func detectFormat(raw []byte, contentType string) string {
	// Content-Type wins when recognizable.
	if ct := strings.TrimSpace(contentType); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err == nil {
			if kind, ok := supportedMimeTypes[strings.ToLower(mt)]; ok {
				return kind
			}
		}
	}
	// Magic-byte sniffing covers each format in priority order:
	// WAV (RIFF) > OGG (OggS) > WebM/Matroska (EBML 1A 45 DF A3) > MP3.
	if len(raw) >= 4 && bytes.Equal(raw[:4], []byte("RIFF")) {
		return "wav"
	}
	if len(raw) >= 4 && bytes.Equal(raw[:4], []byte("OggS")) {
		return "ogg"
	}
	if len(raw) >= 4 && bytes.Equal(raw[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		return "webm"
	}
	if len(raw) >= 3 && bytes.Equal(raw[:3], []byte("ID3")) {
		return "mp3"
	}
	if len(raw) >= 2 && raw[0] == 0xFF && (raw[1]&0xE0) == 0xE0 {
		return "mp3"
	}
	// Fall back to http.DetectContentType for MP3 variants.
	if strings.HasPrefix(http.DetectContentType(raw), "audio/mpeg") {
		return "mp3"
	}
	return ""
}

// decodeWAV parses a canonical PCM WAV file, extracts the data chunk, and
// normalizes to 16 kHz mono S16LE.
func decodeWAV(raw []byte, limits DecodeLimits) (*DecodedAudio, error) {
	if len(raw) < 44 || !bytes.Equal(raw[0:4], []byte("RIFF")) || !bytes.Equal(raw[8:12], []byte("WAVE")) {
		return nil, errors.New("audio: invalid WAV header")
	}

	// Walk chunks starting at offset 12. We need the fmt chunk and the data chunk.
	var (
		haveFmt     bool
		haveData    bool
		audioFormat uint16
		channels    uint16
		sampleRate  uint32
		bps         uint16
		dataStart   int
		dataEnd     int
	)
	offset := 12
	for offset+8 <= len(raw) {
		id := string(raw[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + size
		if chunkEnd > len(raw) {
			// Some WAV files over-report the data chunk size; clamp to payload.
			chunkEnd = len(raw)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("audio: malformed WAV fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(raw[chunkStart : chunkStart+2])
			channels = binary.LittleEndian.Uint16(raw[chunkStart+2 : chunkStart+4])
			sampleRate = binary.LittleEndian.Uint32(raw[chunkStart+4 : chunkStart+8])
			bps = binary.LittleEndian.Uint16(raw[chunkStart+14 : chunkStart+16])
			haveFmt = true
		case "data":
			dataStart = chunkStart
			dataEnd = chunkEnd
			haveData = true
		}
		// Chunks are word-aligned: odd sizes pad with one byte.
		if size%2 == 1 {
			chunkEnd++
		}
		offset = chunkEnd
	}

	if !haveFmt || !haveData {
		return nil, errors.New("audio: WAV missing fmt or data chunk")
	}
	if audioFormat != 1 {
		return nil, fmt.Errorf("audio: WAV audio format %d not supported (only PCM=1 is accepted)", audioFormat)
	}
	if bps != 16 {
		return nil, fmt.Errorf("audio: WAV bit depth %d not supported (only 16-bit PCM is accepted)", bps)
	}
	if channels == 0 || channels > 8 {
		return nil, fmt.Errorf("audio: WAV channel count %d out of range", channels)
	}
	if sampleRate == 0 {
		return nil, errors.New("audio: WAV sample rate is zero")
	}

	pcm := raw[dataStart:dataEnd]
	normalized, err := normalize(pcm, int(sampleRate), int(channels))
	if err != nil {
		return nil, err
	}
	decoded := &DecodedAudio{
		PCM:          normalized,
		SourceFormat: "wav",
		SourceRate:   int(sampleRate),
		SourceCh:     int(channels),
		DurationMs:   pcmDurationMs(normalized),
	}
	return decoded, enforceDecodedLimit(decoded, limits)
}

// decodeMP3 uses hajimehoshi/go-mp3 (pure-Go) to produce S16LE PCM at the
// decoder's native rate, then normalizes to 16 kHz mono.
func decodeMP3(raw []byte, limits DecodeLimits) (*DecodedAudio, error) {
	dec, err := mp3.NewDecoder(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("audio: decode MP3: %w", err)
	}
	// go-mp3 always outputs S16LE stereo at its native sample rate.
	reader := io.Reader(dec)
	maxNativeBytes := decodedBytesLimitFor(limits, dec.SampleRate(), 2)
	if maxNativeBytes > 0 {
		reader = io.LimitReader(dec, maxNativeBytes+1)
	}
	pcm, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("audio: read MP3 stream: %w", err)
	}
	if maxNativeBytes > 0 && int64(len(pcm)) > maxNativeBytes {
		return nil, decodeLimitError(limits, maxNativeBytes, int64(len(pcm)), 0)
	}
	normalized, err := normalize(pcm, dec.SampleRate(), 2)
	if err != nil {
		return nil, err
	}
	decoded := &DecodedAudio{
		PCM:          normalized,
		SourceFormat: "mp3",
		SourceRate:   dec.SampleRate(),
		SourceCh:     2,
		DurationMs:   pcmDurationMs(normalized),
	}
	return decoded, enforceDecodedLimit(decoded, limits)
}

// decodeRawPCM accepts bytes already claimed to be 16-bit PCM. If the payload
// is not in 16 kHz mono we still try to normalize, treating the bytes as
// S16LE at the declared rate. v1 assumes clients label raw PCM with
// `audio/L16;rate=16000;channels=1` — non-16k mono raw requires future work.
func decodeRawPCM(raw []byte, limits DecodeLimits) (*DecodedAudio, error) {
	if len(raw)%TargetBytesPerSample != 0 {
		return nil, fmt.Errorf("audio: raw PCM length %d is not aligned to %d-byte samples", len(raw), TargetBytesPerSample)
	}
	// We can't know rate/channels without parameters; assume canonical.
	decoded := &DecodedAudio{
		PCM:          raw,
		SourceFormat: "pcm16",
		SourceRate:   TargetSampleRate,
		SourceCh:     TargetChannels,
		DurationMs:   pcmDurationMs(raw),
	}
	return decoded, enforceDecodedLimit(decoded, limits)
}

// normalize downmixes multi-channel PCM to mono and resamples to 16 kHz.
// Input is S16LE; output is S16LE at TargetSampleRate mono.
func normalize(pcm []byte, srcRate, srcCh int) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, errors.New("audio: normalize: empty PCM buffer")
	}
	if srcCh < 1 {
		return nil, fmt.Errorf("audio: normalize: invalid channel count %d", srcCh)
	}

	mono := pcm
	if srcCh != 1 {
		mono = downmixToMono(pcm, srcCh)
	}
	if srcRate == TargetSampleRate {
		return mono, nil
	}
	return resampleLinear(mono, srcRate, TargetSampleRate), nil
}

// downmixToMono averages every N channels into a single S16LE mono stream.
func downmixToMono(pcm []byte, channels int) []byte {
	if channels <= 1 {
		return pcm
	}
	frameBytes := channels * TargetBytesPerSample
	frames := len(pcm) / frameBytes
	out := make([]byte, frames*TargetBytesPerSample)
	for f := 0; f < frames; f++ {
		var sum int32
		base := f * frameBytes
		for c := 0; c < channels; c++ {
			o := base + c*TargetBytesPerSample
			s := int16(binary.LittleEndian.Uint16(pcm[o : o+2])) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
			sum += int32(s)
		}
		avg := sum / int32(channels) // #nosec G115 -- channel count is validated positive and tiny for decoded audio.
		if avg > math.MaxInt16 {
			avg = math.MaxInt16
		} else if avg < math.MinInt16 {
			avg = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[f*TargetBytesPerSample:], uint16(int16(avg))) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
	}
	return out
}

// resampleLinear is a naïve linear-interpolation resampler. Good enough for
// speech at 16 kHz; production-grade sinc resampling is a v1.1 item if STT
// quality regresses. Keeping it pure-Go avoids pulling in SoX/libsamplerate.
func resampleLinear(mono []byte, srcRate, dstRate int) []byte {
	if srcRate == dstRate {
		return mono
	}
	srcSamples := len(mono) / TargetBytesPerSample
	if srcSamples < 2 {
		return mono
	}
	dstSamples := int(int64(srcSamples) * int64(dstRate) / int64(srcRate))
	if dstSamples < 1 {
		return nil
	}
	out := make([]byte, dstSamples*TargetBytesPerSample)
	ratio := float64(srcRate) / float64(dstRate)
	for i := 0; i < dstSamples; i++ {
		srcIdxF := float64(i) * ratio
		srcIdx := int(srcIdxF)
		frac := srcIdxF - float64(srcIdx)
		if srcIdx >= srcSamples-1 {
			srcIdx = srcSamples - 1
			frac = 0
		}
		s0 := int16(binary.LittleEndian.Uint16(mono[srcIdx*TargetBytesPerSample:])) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
		var s1 int16
		if srcIdx+1 < srcSamples {
			s1 = int16(binary.LittleEndian.Uint16(mono[(srcIdx+1)*TargetBytesPerSample:])) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
		} else {
			s1 = s0
		}
		v := float64(s0)*(1-frac) + float64(s1)*frac
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i*TargetBytesPerSample:], uint16(int16(v))) // #nosec G115 -- v is clamped to the int16 PCM range above.
	}
	return out
}

func pcmDurationMs(pcm []byte) int64 {
	samples := int64(len(pcm) / TargetBytesPerSample)
	return samples * 1000 / int64(TargetSampleRate)
}

func enforceDecodedLimit(decoded *DecodedAudio, limits DecodeLimits) error {
	if decoded == nil || limits.MaxDecodedAudioSeconds <= 0 {
		return nil
	}
	maxBytes := decodedBytesLimitFor(limits, TargetSampleRate, TargetChannels)
	if maxBytes > 0 && int64(len(decoded.PCM)) > maxBytes {
		return decodeLimitError(limits, maxBytes, int64(len(decoded.PCM)), decoded.DurationMs)
	}
	maxDurationMs := int64(limits.MaxDecodedAudioSeconds) * 1000
	if decoded.DurationMs > maxDurationMs {
		return decodeLimitError(limits, maxBytes, int64(len(decoded.PCM)), decoded.DurationMs)
	}
	return nil
}

func decodedBytesLimitFor(limits DecodeLimits, sampleRate, channels int) int64 {
	if limits.MaxDecodedAudioSeconds <= 0 || sampleRate <= 0 || channels <= 0 {
		return 0
	}
	return int64(limits.MaxDecodedAudioSeconds) * int64(sampleRate) * int64(channels) * int64(TargetBytesPerSample)
}

func decodeLimitError(limits DecodeLimits, maxBytes, actualBytes, durationMs int64) error {
	return &DecodeLimitError{
		MaxSeconds:  limits.MaxDecodedAudioSeconds,
		DurationMs:  durationMs,
		MaxBytes:    maxBytes,
		ActualBytes: actualBytes,
	}
}

type cappedBuffer struct {
	buf         bytes.Buffer
	limit       int64
	actualBytes int64
	exceeded    bool
}

func newCappedBuffer(limit int64) cappedBuffer {
	return cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.actualBytes += int64(len(p))
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.exceeded = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		b.exceeded = true
		_, _ = b.buf.Write(p[:int(remaining)])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte { return b.buf.Bytes() }

func (b *cappedBuffer) String() string { return b.buf.String() }

func (b *cappedBuffer) Exceeded() bool { return b.exceeded }

func (b *cappedBuffer) Limit() int64 { return b.limit }

func (b *cappedBuffer) ActualBytes() int64 { return b.actualBytes }

// ffmpegBinary is overridable in tests. Production uses whichever ffmpeg
// is on PATH (the Server-Target Dockerfile installs it).
var ffmpegBinary = "ffmpeg"

// ffmpegTimeout caps a single decode invocation. 30s comfortably covers
// the 25 MB max upload limit even on a small CPU.
const ffmpegTimeout = 30 * time.Second

// decodeViaFFmpeg pipes raw container bytes through ffmpeg to produce
// canonical 16 kHz S16LE mono PCM. inputFormat is the ffmpeg `-f` value
// passed as the demuxer hint so ffmpeg doesn't have to probe a piped
// stream that lacks seekable file boundaries. label is what ends up in
// DecodedAudio.SourceFormat so the response carries a stable identifier.
func decodeViaFFmpeg(ctx context.Context, raw []byte, label, inputFormat string, limits DecodeLimits) (*DecodedAudio, error) {
	if _, err := exec.LookPath(ffmpegBinary); err != nil {
		return nil, ErrFFmpegUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-f", inputFormat,
		"-i", "pipe:0",
		"-vn", // drop any video track that snuck into a webm container
		"-ar", fmt.Sprintf("%d", TargetSampleRate),
		"-ac", fmt.Sprintf("%d", TargetChannels),
		"-f", "s16le",
		"pipe:1",
	}
	cmd := exec.CommandContext(ctx, ffmpegBinary, args...) // #nosec G204 -- ffmpegBinary is deployment/operator controlled; arguments are fixed, non-shell argv values.
	cmd.Stdin = bytes.NewReader(raw)
	stdout := newCappedBuffer(decodedBytesLimitFor(limits, TargetSampleRate, TargetChannels))
	stderr := newCappedBuffer(64 << 10)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("audio: ffmpeg decode (%s): %w; stderr=%q",
			label, err, strings.TrimSpace(stderr.String()))
	}
	if stdout.Exceeded() {
		return nil, decodeLimitError(limits, stdout.Limit(), stdout.ActualBytes(), 0)
	}
	pcm := stdout.Bytes()
	if len(pcm) == 0 {
		return nil, fmt.Errorf("audio: ffmpeg decode (%s) produced 0 bytes; stderr=%q",
			label, strings.TrimSpace(stderr.String()))
	}
	decoded := &DecodedAudio{
		PCM:          pcm,
		SourceFormat: label,
		// We don't know the source rate/channels without parsing the
		// container ourselves; ffmpeg has already resampled to
		// canonical 16 kHz mono. Report the canonical values so the
		// API response is consistent with the data on the wire.
		SourceRate: TargetSampleRate,
		SourceCh:   TargetChannels,
		DurationMs: pcmDurationMs(pcm),
	}
	return decoded, enforceDecodedLimit(decoded, limits)
}
