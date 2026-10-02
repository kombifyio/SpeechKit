//go:build linux

package voiceagent

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/app/internal/server/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// DownlinkSampleRate is the rate of server-to-client Voice Agent audio frames
// (raw S16LE mono, see protocol.go).
const DownlinkSampleRate = 24000

// NewDownlinkTTS adapts a TTS engine to the Voice Agent wire. Clients play
// every binary frame as 24 kHz S16LE mono PCM, so the synthesized payload is
// requested as WAV (its header carries the true rate) and converted to that
// shape whatever the selected provider returns. A payload that cannot be
// converted fails the turn instead of reaching the speaker as noise.
func NewDownlinkTTS(inner CascadedTTS) CascadedTTS {
	if inner == nil {
		return nil
	}
	return downlinkTTS{inner: inner}
}

type downlinkTTS struct {
	inner CascadedTTS
}

func (t downlinkTTS) Synthesize(ctx context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error) {
	opts.Format = "wav"
	result, err := t.inner.Synthesize(ctx, text, opts)
	if err != nil || result == nil || len(result.Audio) == 0 {
		return result, err
	}
	pcm, err := audio.SynthesizedToPCM16Mono(result.Audio, result.Format, result.SampleRate, DownlinkSampleRate)
	if err != nil {
		return nil, fmt.Errorf("voice agent downlink: %w", err)
	}
	converted := *result
	converted.Audio = pcm
	converted.Format = "pcm"
	converted.SampleRate = DownlinkSampleRate
	return &converted, nil
}
