package tts

import "github.com/kombifyio/SpeechKit/pkg/speechkit"

// CapabilityReporter is an optional interface a provider can implement to
// self-describe its capabilities, so routing can query them instead of
// string-matching profile IDs. It is intentionally NOT part of Provider, so
// existing implementations and test doubles are unaffected.
type CapabilityReporter interface {
	Capabilities() []speechkit.Capability
}

func ttsCapabilities() []speechkit.Capability {
	return []speechkit.Capability{speechkit.CapabilityTTS}
}

func (*OpenAI) Capabilities() []speechkit.Capability      { return ttsCapabilities() }
func (*Google) Capabilities() []speechkit.Capability      { return ttsCapabilities() }
func (*HuggingFace) Capabilities() []speechkit.Capability { return ttsCapabilities() }
func (*Piper) Capabilities() []speechkit.Capability       { return ttsCapabilities() }
