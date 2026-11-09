package cascaded

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

// OutputSampleRate is the raw S16LE mono format of live Voice Agent audio.
const OutputSampleRate = 24000

// NewPCMOutputTTS adapts synthesized speech to the live Voice Agent output
// contract. It requests WAV to retain the source rate, then decodes and
// resamples it before any bytes reach playback. Nil keeps text-only sessions.
func NewPCMOutputTTS(inner TTS) TTS {
	if inner == nil {
		return nil
	}
	return pcmOutputTTS{inner: inner}
}

type pcmOutputTTS struct{ inner TTS }

func (t pcmOutputTTS) Synthesize(ctx context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error) {
	opts.Format = "wav"
	result, err := t.inner.Synthesize(ctx, text, opts)
	if err != nil || result == nil || len(result.Audio) == 0 {
		return result, err
	}
	pcm, err := audio.SynthesizedToPCM16Mono(result.Audio, result.Format, result.SampleRate, OutputSampleRate)
	if err != nil {
		return nil, fmt.Errorf("voice agent output: %w", err)
	}
	converted := *result
	converted.Audio = pcm
	converted.Format = "pcm"
	converted.SampleRate = OutputSampleRate
	return &converted, nil
}

// pcmStreamTTS is implemented by the live output adapter: it streams speech in
// the output format itself, so the provider can play a sentence while it is
// still being synthesized. streamed=false means the request is not streamable
// and nothing was emitted; the caller synthesizes it whole.
type pcmStreamTTS interface {
	streamPCM(ctx context.Context, text string, opts tts.SynthesizeOpts, emit func(pcm []byte) error) (streamed bool, err error)
}

func (t pcmOutputTTS) streamPCM(ctx context.Context, text string, opts tts.SynthesizeOpts, emit func(pcm []byte) error) (bool, error) {
	streamer, ok := t.inner.(tts.PCMStreamer)
	if !ok {
		return false, nil
	}
	return streamer.SynthesizePCMStream(ctx, text, opts, OutputSampleRate, emit)
}
