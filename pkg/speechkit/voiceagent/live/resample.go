package live

import "github.com/kombifyio/SpeechKit/pkg/speechkit/internal/pcmresample"

// UpsampleMicPCM16Mono linearly interpolates a 16-bit signed little-endian PCM
// mono buffer from SpeechKit's mic rate (16 kHz) to OpenAI's input rate
// (24 kHz). For the 16 kHz to 24 kHz path (ratio 2:3) the cost is negligible
// compared to the WS round-trip; CPU profiling at scale should still consider
// replacing this with a polyphase FIR resampler if it shows up as a hot path.
func UpsampleMicPCM16Mono(src []byte) []byte {
	return pcmresample.UpsampleMicPCM16Mono(src)
}
