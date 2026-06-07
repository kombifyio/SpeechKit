package tts

import "github.com/kombifyio/SpeechKit/internal/models"

// CapabilityReporter is an optional interface a provider can implement to
// self-describe its capabilities, so routing can query them instead of
// string-matching profile IDs. It is intentionally NOT part of Provider, so
// existing implementations and test doubles are unaffected.
type CapabilityReporter interface {
	Capabilities() []models.Capability
}

func ttsCapabilities() []models.Capability {
	return []models.Capability{models.CapabilityTTS}
}

func (*OpenAI) Capabilities() []models.Capability      { return ttsCapabilities() }
func (*Google) Capabilities() []models.Capability      { return ttsCapabilities() }
func (*HuggingFace) Capabilities() []models.Capability { return ttsCapabilities() }
func (*Piper) Capabilities() []models.Capability       { return ttsCapabilities() }
