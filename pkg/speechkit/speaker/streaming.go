package speaker

import (
	"context"
	"strings"
)

// AudioEncoding names the on-wire audio encoding a streaming speaker provider
// receives.
type AudioEncoding string

const (
	AudioEncodingLinear16 AudioEncoding = "linear16"
	AudioEncodingPCM16    AudioEncoding = "pcm16"
)

// AudioFormat describes the raw microphone stream passed to a speaker
// streaming provider. Empty fields normalize to SpeechKit's Voice Agent
// default: 16 kHz S16 mono PCM.
type AudioFormat struct {
	Encoding     AudioEncoding `json:"encoding,omitempty"`
	SampleRateHz int           `json:"sampleRateHz,omitempty"`
	Channels     int           `json:"channels,omitempty"`
	Container    string        `json:"container,omitempty"`
}

func (f AudioFormat) Normalized() AudioFormat {
	f.Container = strings.TrimSpace(f.Container)
	if f.Encoding == "" {
		f.Encoding = AudioEncodingLinear16
	}
	if f.SampleRateHz <= 0 {
		f.SampleRateHz = 16000
	}
	if f.Channels <= 0 {
		f.Channels = 1
	}
	return f
}

// SpeakerFrame is the canonical realtime speaker attribution frame. It is
// intentionally shaped like DiarizationResult fragments so Voice Agent,
// realtime clients, and SDKs can consume one public contract.
type SpeakerFrame struct {
	Provider  string          `json:"provider,omitempty"`
	Model     string          `json:"model,omitempty"`
	Sequence  int64           `json:"sequence,omitempty"`
	Text      string          `json:"text,omitempty"`
	IsFinal   bool            `json:"isFinal,omitempty"`
	Segment   *SpeakerSegment `json:"segment,omitempty"`
	Words     []SpeakerWord   `json:"words,omitempty"`
	Speakers  []Speaker       `json:"speakers,omitempty"`
	LatencyMs int64           `json:"latencyMs,omitempty"`
}

// StreamingProvider is implemented by speaker providers that support live
// attribution against a stream of raw audio chunks.
type StreamingProvider interface {
	StartSpeakerStream(ctx context.Context, opts Options, format AudioFormat) (SpeakerStream, error)
}

// SpeakerStream is a bidirectional streaming speaker session.
type SpeakerStream interface {
	SendAudio(ctx context.Context, chunk []byte) error
	EndAudio(ctx context.Context) error
	Receive(ctx context.Context) (*SpeakerFrame, error)
	Close() error
}

// FrameFromWords builds the common frame fields from provider word output.
func FrameFromWords(provider, model string, sequence int64, text string, final bool, words []SpeakerWord, latencyMs int64) SpeakerFrame {
	segments := BuildSegmentsFromWords(words)
	var segment *SpeakerSegment
	if len(segments) > 0 {
		first := segments[0]
		if strings.TrimSpace(text) != "" {
			first.Text = strings.TrimSpace(text)
		}
		segment = &first
	}
	return SpeakerFrame{
		Provider:  strings.TrimSpace(provider),
		Model:     strings.TrimSpace(model),
		Sequence:  sequence,
		Text:      strings.TrimSpace(text),
		IsFinal:   final,
		Segment:   segment,
		Words:     words,
		Speakers:  SpeakersFromSegments(segments),
		LatencyMs: latencyMs,
	}
}
