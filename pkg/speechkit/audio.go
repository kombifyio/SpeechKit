package speechkit

import "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"

// Audio format of the SpeechKit capture path, re-exported from [audio] so
// hosts that only import the root package get the same numbers: 16 kHz, one
// channel, signed 16-bit samples, two bytes per sample.
const (
	AudioSampleRate     = audio.SampleRate
	AudioChannels       = audio.Channels
	AudioBitsPerSample  = audio.BitsPerSample
	AudioBytesPerSample = audio.BytesPerSample
)

// PCMToWAV wraps raw 16kHz S16 mono PCM data in a WAV header.
func PCMToWAV(pcm []byte) []byte { return audio.PCMToWAV(pcm) }

// PCMDurationSecs returns the duration of 16kHz S16 mono PCM audio in seconds.
func PCMDurationSecs(pcm []byte) float64 { return audio.PCMDurationSecs(pcm) }
