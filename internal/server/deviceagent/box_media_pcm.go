package deviceagent

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
)

// audio/L16 uses network byte order. SpeechKit's internal PCM/WAV kernel uses
// signed little-endian samples, so the media boundary swaps each exact sample
// without changing sample identity or count.
func l16ToPCM16LE(l16 []byte) []byte {
	out := make([]byte, len(l16))
	for index := 0; index+1 < len(l16); index += 2 {
		out[index], out[index+1] = l16[index+1], l16[index]
	}
	return out
}

func pcm16LEToL16(pcm []byte) []byte {
	return l16ToPCM16LE(pcm)
}

func boxMediaPCM48k(result *TurnResult) ([]byte, error) {
	if result == nil || !strings.EqualFold(strings.TrimSpace(result.Format), "wav") {
		return nil, errors.New("box media: TTS must return WAV")
	}
	pcm, rate, channels, bits, err := parseBoxMediaWAV(result.Audio)
	if err != nil {
		return nil, err
	}
	if channels != boxMediaChannels || bits != boxMediaBytesPerSample*8 {
		return nil, errors.New("box media: TTS WAV must be mono PCM16")
	}
	if result.SampleRate > 0 && result.SampleRate != rate {
		return nil, errors.New("box media: TTS sample-rate metadata mismatch")
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, errors.New("box media: TTS PCM is empty or misaligned")
	}
	duration := timeDurationForPCM(len(pcm), rate)
	if duration <= 0 || duration > 30_000 {
		return nil, errors.New("box media: TTS duration is outside 1ms..30s")
	}
	resampled, err := resamplePCM16Mono(pcm, rate, boxMediaOutputRate)
	if err != nil {
		return nil, err
	}
	if len(resampled) == 0 || len(resampled) > boxMediaMaximumResponseBytes {
		return nil, errors.New("box media: resampled response exceeds contract limit")
	}
	return resampled, nil
}

func timeDurationForPCM(byteCount, rate int) int64 {
	if byteCount <= 0 || rate <= 0 {
		return 0
	}
	return int64(byteCount/boxMediaBytesPerSample) * 1000 / int64(rate)
}

func parseBoxMediaWAV(raw []byte) (pcm []byte, rate, channels, bits int, err error) {
	if len(raw) < 12 || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, 0, 0, errors.New("box media: invalid TTS WAV header")
	}
	declared := int(binary.LittleEndian.Uint32(raw[4:8])) + 8
	if declared != len(raw) {
		return nil, 0, 0, 0, errors.New("box media: TTS WAV length mismatch")
	}
	foundFmt := false
	foundData := false
	offset := 12
	for offset+8 <= len(raw) {
		chunkID := string(raw[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		offset += 8
		if chunkSize < 0 || chunkSize > len(raw)-offset {
			return nil, 0, 0, 0, errors.New("box media: malformed TTS WAV chunk")
		}
		chunk := raw[offset : offset+chunkSize]
		switch chunkID {
		case "fmt ":
			if foundFmt || len(chunk) < 16 || binary.LittleEndian.Uint16(chunk[0:2]) != 1 {
				return nil, 0, 0, 0, errors.New("box media: unsupported TTS WAV format")
			}
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			rate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bits = int(binary.LittleEndian.Uint16(chunk[14:16]))
			blockAlign := int(binary.LittleEndian.Uint16(chunk[12:14]))
			byteRate := int(binary.LittleEndian.Uint32(chunk[8:12]))
			if channels <= 0 || channels > 8 || rate < 8000 || rate > 192000 || bits != 16 ||
				blockAlign != channels*bits/8 || byteRate != rate*blockAlign {
				return nil, 0, 0, 0, errors.New("box media: inconsistent TTS WAV format")
			}
			foundFmt = true
		case "data":
			if foundData {
				return nil, 0, 0, 0, errors.New("box media: multiple TTS WAV data chunks")
			}
			pcm = append([]byte(nil), chunk...)
			foundData = true
		}
		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
		if offset > len(raw) {
			return nil, 0, 0, 0, errors.New("box media: malformed TTS WAV padding")
		}
	}
	if offset != len(raw) {
		return nil, 0, 0, 0, errors.New("box media: malformed TTS WAV trailing bytes")
	}
	if !foundFmt || !foundData || rate < 8000 || rate > 192000 || len(pcm)%2 != 0 {
		return nil, 0, 0, 0, errors.New("box media: TTS WAV is incomplete")
	}
	return pcm, rate, channels, bits, nil
}

func resamplePCM16Mono(pcm []byte, sourceRate, targetRate int) ([]byte, error) {
	if sourceRate <= 0 || targetRate <= 0 || len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, errors.New("box media: invalid PCM resample input")
	}
	if sourceRate == targetRate {
		return append([]byte(nil), pcm...), nil
	}
	sourceSamples := len(pcm) / 2
	targetSamples64 := (int64(sourceSamples)*int64(targetRate) + int64(sourceRate)/2) / int64(sourceRate)
	if targetSamples64 <= 0 || targetSamples64 > boxMediaMaximumResponseBytes/2 {
		return nil, errors.New("box media: PCM resample output is outside limits")
	}
	targetSamples := int(targetSamples64)
	out := make([]byte, targetSamples*2)
	if sourceSamples == 1 {
		sample := binary.LittleEndian.Uint16(pcm)
		for index := 0; index < targetSamples; index++ {
			binary.LittleEndian.PutUint16(out[index*2:], sample)
		}
		return out, nil
	}
	for index := 0; index < targetSamples; index++ {
		position := float64(index) * float64(sourceRate) / float64(targetRate)
		left := int(position)
		if left >= sourceSamples-1 {
			left = sourceSamples - 1
			position = float64(left)
		}
		right := left + 1
		if right >= sourceSamples {
			right = left
		}
		fraction := position - float64(left)
		leftSample := float64(int16(binary.LittleEndian.Uint16(pcm[left*2:])))   // #nosec G115 -- identical-width PCM reinterpretation.
		rightSample := float64(int16(binary.LittleEndian.Uint16(pcm[right*2:]))) // #nosec G115 -- identical-width PCM reinterpretation.
		value := math.Round(leftSample + (rightSample-leftSample)*fraction)
		if value < math.MinInt16 {
			value = math.MinInt16
		}
		if value > math.MaxInt16 {
			value = math.MaxInt16
		}
		binary.LittleEndian.PutUint16(out[index*2:], uint16(int16(value))) // #nosec G115 -- clamped to int16 bounds.
	}
	return out, nil
}
