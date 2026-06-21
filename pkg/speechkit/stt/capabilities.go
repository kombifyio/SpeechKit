package stt

import "github.com/kombifyio/SpeechKit/pkg/speechkit"

// CapabilityReporter is an optional interface a provider can implement to
// self-describe its capabilities. Routing and catalogs can type-assert it to
// query capabilities directly instead of string-matching profile IDs.
//
// It is intentionally NOT part of STTProvider, so existing providers, server
// clients, and test doubles continue to satisfy the core interface without
// change; only the production providers below opt in. Capabilities are reported
// using the public speechkit.Capability vocabulary so the adapters carry no
// internal/ dependency.
type CapabilityReporter interface {
	Capabilities() []speechkit.Capability
}

// baseSTTCapabilities are reported by every speech-to-text provider. A fresh
// slice is returned each call so callers may append without aliasing.
func baseSTTCapabilities() []speechkit.Capability {
	return []speechkit.Capability{
		speechkit.CapabilityTranscription,
		speechkit.CapabilitySTT,
		speechkit.CapabilityAudioInput,
	}
}

func (*OpenAICompatibleProvider) Capabilities() []speechkit.Capability { return baseSTTCapabilities() }
func (*HuggingFaceProvider) Capabilities() []speechkit.Capability      { return baseSTTCapabilities() }
func (*LocalProvider) Capabilities() []speechkit.Capability            { return baseSTTCapabilities() }
func (*OpenRouterSTTProvider) Capabilities() []speechkit.Capability    { return baseSTTCapabilities() }

// Deepgram and Google add batch speaker diarization (live-verified in the
// speaker layer).
func (*DeepgramProvider) Capabilities() []speechkit.Capability {
	return append(baseSTTCapabilities(), speechkit.CapabilitySpeakerDiarization)
}

func (*GoogleSTTProvider) Capabilities() []speechkit.Capability {
	return append(baseSTTCapabilities(), speechkit.CapabilitySpeakerDiarization)
}

// AssemblyAI additionally attributes/identifies speakers against
// caller-supplied names or roles.
func (*AssemblyAIProvider) Capabilities() []speechkit.Capability {
	return append(baseSTTCapabilities(),
		speechkit.CapabilitySpeakerDiarization,
		speechkit.CapabilitySpeakerAttribution,
		speechkit.CapabilitySpeakerIdentification,
	)
}
