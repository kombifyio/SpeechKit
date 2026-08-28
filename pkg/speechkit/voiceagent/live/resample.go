package live

import "encoding/binary"

// Microphone capture runs at 16 kHz; the realtime providers that need PCM
// input want 24 kHz. Both rates are protocol facts, not tuning knobs.
const (
	micSampleRate      = 16000
	upsampledInputRate = 24000
)

// UpsampleMicPCM16Mono linearly interpolates a 16-bit signed little-endian PCM
// mono buffer from SpeechKit's mic rate to OpenAI's input rate. For the 16 kHz
// to 24 kHz path
// (ratio 2:3) the cost is negligible compared to the WS round-trip; CPU
// profiling at scale should still consider replacing this with a polyphase
// FIR resampler if it shows up as a hot path.
func UpsampleMicPCM16Mono(src []byte) []byte {
	if micSampleRate == upsampledInputRate || len(src) < 2 {
		return src
	}
	// Decode int16 LE samples.
	srcSampleCount := len(src) / 2
	if srcSampleCount == 0 {
		return src
	}
	srcSamples := make([]int16, srcSampleCount)
	for i := range srcSampleCount {
		lo := int16(src[2*i])
		hi := int16(src[2*i+1])
		srcSamples[i] = (hi << 8) | (lo & 0xff)
	}
	dstSampleCount := srcSampleCount * upsampledInputRate / micSampleRate
	if dstSampleCount == 0 {
		return nil
	}
	dst := make([]byte, dstSampleCount*2)
	step := float64(micSampleRate) / float64(upsampledInputRate)
	for i := range dstSampleCount {
		pos := float64(i) * step
		lo := int(pos)
		hi := lo + 1
		if hi >= srcSampleCount {
			hi = srcSampleCount - 1
		}
		frac := pos - float64(lo)
		s := float64(srcSamples[lo])*(1-frac) + float64(srcSamples[hi])*frac
		v := int16(s)
		sample := uint16(v) // #nosec G115 -- PCM16 little-endian stores signed samples as two's-complement bytes.
		binary.LittleEndian.PutUint16(dst[2*i:], sample)
	}
	return dst
}
