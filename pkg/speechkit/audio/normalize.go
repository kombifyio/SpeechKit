package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// NormalizePCM16Mono downmixes multi-channel S16LE PCM to mono and resamples it to
// dstRate.
func NormalizePCM16Mono(pcm []byte, srcRate, srcCh, dstRate int) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, errors.New("audio: normalize: empty PCM buffer")
	}
	if srcRate <= 0 || dstRate <= 0 {
		return nil, fmt.Errorf("audio: normalize: invalid sample rates %d -> %d", srcRate, dstRate)
	}
	if srcCh < 1 || srcCh > 8 {
		return nil, fmt.Errorf("audio: normalize: invalid channel count %d", srcCh)
	}

	if len(pcm)%(srcCh*BytesPerSample) != 0 {
		return nil, errors.New("audio: normalize: incomplete PCM frame")
	}

	mono := pcm
	if srcCh != 1 {
		mono = downmixToMono(pcm, srcCh)
	}
	if srcRate == dstRate {
		return mono, nil
	}
	return resampleLinear(mono, srcRate, dstRate), nil
}

// downmixToMono averages every N channels into a single S16LE mono stream.
func downmixToMono(pcm []byte, channels int) []byte {
	if channels <= 1 {
		return pcm
	}
	frameBytes := channels * BytesPerSample
	frames := len(pcm) / frameBytes
	out := make([]byte, frames*BytesPerSample)
	for f := 0; f < frames; f++ {
		var sum int32
		base := f * frameBytes
		for c := 0; c < channels; c++ {
			o := base + c*BytesPerSample
			s := int16(binary.LittleEndian.Uint16(pcm[o : o+2])) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
			sum += int32(s)
		}
		avg := sum / int32(channels) // #nosec G115 -- channel count is validated positive and tiny for decoded audio.
		if avg > math.MaxInt16 {
			avg = math.MaxInt16
		} else if avg < math.MinInt16 {
			avg = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[f*BytesPerSample:], uint16(int16(avg))) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
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
	srcSamples := len(mono) / BytesPerSample
	if srcSamples < 2 {
		return mono
	}
	dstSamples := int(int64(srcSamples) * int64(dstRate) / int64(srcRate))
	if dstSamples < 1 {
		return nil
	}
	out := make([]byte, dstSamples*BytesPerSample)
	ratio := float64(srcRate) / float64(dstRate)
	for i := 0; i < dstSamples; i++ {
		srcIdxF := float64(i) * ratio
		srcIdx := int(srcIdxF)
		frac := srcIdxF - float64(srcIdx)
		if srcIdx >= srcSamples-1 {
			srcIdx = srcSamples - 1
			frac = 0
		}
		s0 := int16(binary.LittleEndian.Uint16(mono[srcIdx*BytesPerSample:])) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
		var s1 int16
		if srcIdx+1 < srcSamples {
			s1 = int16(binary.LittleEndian.Uint16(mono[(srcIdx+1)*BytesPerSample:])) // #nosec G115 -- PCM sample reinterpretation intentionally preserves two's-complement bits.
		} else {
			s1 = s0
		}
		v := float64(s0)*(1-frac) + float64(s1)*frac
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i*BytesPerSample:], uint16(int16(v))) // #nosec G115 -- v is clamped to the int16 PCM range above.
	}
	return out
}
