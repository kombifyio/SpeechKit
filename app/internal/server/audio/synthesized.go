//go:build linux

package audio

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/hajimehoshi/go-mp3"
)

// SynthesizedToPCM16Mono converts one synthesized speech payload (WAV, MP3 or
// raw S16LE PCM at sourceRate) to S16LE mono PCM at dstRate, the shape a raw
// PCM audio wire expects. Formats it cannot decode, such as Opus, fail rather
// than pass through as unplayable bytes.
func SynthesizedToPCM16Mono(raw []byte, format string, sourceRate, dstRate int) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("audio: empty synthesized payload")
	}
	if dstRate <= 0 {
		return nil, fmt.Errorf("audio: invalid target sample rate %d", dstRate)
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "pcm", "linear16", "pcm16", "s16le":
		if sourceRate <= 0 {
			return nil, fmt.Errorf("audio: raw PCM needs its sample rate")
		}
		if len(raw)%TargetBytesPerSample != 0 {
			return nil, fmt.Errorf("audio: raw PCM length %d is not aligned to %d-byte samples", len(raw), TargetBytesPerSample)
		}
		return normalizeTo(raw, sourceRate, 1, dstRate)
	case "wav", "wave":
		pcm, rate, channels, err := parseWAV(raw)
		if err != nil {
			return nil, err
		}
		return normalizeTo(pcm, rate, channels, dstRate)
	case "mp3", "mpeg":
		decoder, err := mp3.NewDecoder(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("audio: decode MP3: %w", err)
		}
		pcm, err := io.ReadAll(decoder)
		if err != nil {
			return nil, fmt.Errorf("audio: read MP3 stream: %w", err)
		}
		// go-mp3 always emits S16LE stereo at the stream's native rate.
		return normalizeTo(pcm, decoder.SampleRate(), 2, dstRate)
	default:
		return nil, fmt.Errorf("audio: synthesized format %q cannot be converted to PCM", format)
	}
}
