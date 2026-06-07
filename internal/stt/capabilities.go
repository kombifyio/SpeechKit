package stt

import "github.com/kombifyio/SpeechKit/internal/models"

// CapabilityReporter is an optional interface a provider can implement to
// self-describe its capabilities. Routing and catalogs can type-assert it to
// query capabilities directly instead of string-matching profile IDs.
//
// It is intentionally NOT part of STTProvider, so existing providers, server
// clients, and test doubles continue to satisfy the core interface without
// change; only the production providers below opt in.
type CapabilityReporter interface {
	Capabilities() []models.Capability
}

// baseSTTCapabilities are reported by every speech-to-text provider. A fresh
// slice is returned each call so callers may append without aliasing.
func baseSTTCapabilities() []models.Capability {
	return []models.Capability{
		models.CapabilityTranscription,
		models.CapabilitySTT,
		models.CapabilityAudioInput,
	}
}

func (*OpenAICompatibleProvider) Capabilities() []models.Capability { return baseSTTCapabilities() }
func (*HuggingFaceProvider) Capabilities() []models.Capability      { return baseSTTCapabilities() }
func (*LocalProvider) Capabilities() []models.Capability            { return baseSTTCapabilities() }
func (*OpenRouterSTTProvider) Capabilities() []models.Capability    { return baseSTTCapabilities() }

// Deepgram and Google add batch speaker diarization (live-verified in the
// speaker layer).
func (*DeepgramProvider) Capabilities() []models.Capability {
	return append(baseSTTCapabilities(), models.CapabilitySpeakerDiarization)
}

func (*GoogleSTTProvider) Capabilities() []models.Capability {
	return append(baseSTTCapabilities(), models.CapabilitySpeakerDiarization)
}

// AssemblyAI additionally attributes/identifies speakers against
// caller-supplied names or roles.
func (*AssemblyAIProvider) Capabilities() []models.Capability {
	return append(baseSTTCapabilities(),
		models.CapabilitySpeakerDiarization,
		models.CapabilitySpeakerAttribution,
		models.CapabilitySpeakerIdentification,
	)
}
