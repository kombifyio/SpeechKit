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

// Capabilities implements [CapabilityReporter]; OpenAI reports
// [speechkit.CapabilityTTS] only.
func (*OpenAI) Capabilities() []speechkit.Capability { return ttsCapabilities() }

// Capabilities implements [CapabilityReporter]; the opt-in Google Cloud
// Text-to-Speech provider reports [speechkit.CapabilityTTS] only.
func (*Google) Capabilities() []speechkit.Capability { return ttsCapabilities() }

// Capabilities implements [CapabilityReporter]; HuggingFace reports
// [speechkit.CapabilityTTS] only.
func (*HuggingFace) Capabilities() []speechkit.Capability { return ttsCapabilities() }

// Capabilities implements [CapabilityReporter]; Piper reports
// [speechkit.CapabilityTTS] only.
func (*Piper) Capabilities() []speechkit.Capability { return ttsCapabilities() }
