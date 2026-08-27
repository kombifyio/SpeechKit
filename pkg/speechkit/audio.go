package speechkit

import "github.com/kombifyio/SpeechKit/pkg/speechkit/audio"

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
