package live

// TranscriptionMode is the AssemblyAI native speech recognition profile.
type TranscriptionMode string

// Supported native transcription modes. Empty uses the provider default.
const (
	TranscriptionBalanced    TranscriptionMode = "balanced"
	TranscriptionMinLatency  TranscriptionMode = "min_latency"
	TranscriptionMaxAccuracy TranscriptionMode = "max_accuracy"
)

// Valid reports whether the mode is supported, including the default.
func (m TranscriptionMode) Valid() bool {
	return m == "" || m == TranscriptionBalanced || m == TranscriptionMinLatency || m == TranscriptionMaxAccuracy
}
