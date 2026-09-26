// Package speaker defines SpeechKit's public speaker diarization and
// attribution contracts. It is intentionally provider-neutral so Dictation,
// Assist, Voice Agent fallbacks, and HTTP clients can share the same result
// shape without importing desktop internals.
package speaker

import (
	"context"
	"fmt"
	"strings"
)

// Capability names speaker-specific add-on capabilities. These are not modes:
// hosts opt into them per request or per provider profile.
type Capability string

// Speaker capabilities a provider profile or request can opt into.
const (
	// CapabilityDiarization separates speech into anonymous speaker turns.
	CapabilityDiarization Capability = "speaker_diarization"
	// CapabilityIdentification lets the provider itself name speakers from
	// host-supplied names or roles.
	CapabilityIdentification Capability = "speaker_identification"
	// CapabilityAttribution maps speaker turns to host-provided
	// [KnownSpeaker] context.
	CapabilityAttribution Capability = "speaker_attribution"
	// CapabilityEnrollment is reserved for biometric speaker enrollment; no
	// shipped provider profile declares it.
	CapabilityEnrollment Capability = "speaker_enrollment"
)

// IdentificationLevel describes how far a provider went beyond plain
// transcription.
type IdentificationLevel string

// Identification levels, weakest first.
const (
	// IdentificationNone means plain transcription without speaker work.
	IdentificationNone IdentificationLevel = "none"
	// IdentificationDiarization means anonymous speaker labels only.
	IdentificationDiarization IdentificationLevel = "diarization"
	// IdentificationAttribution means labels were mapped to host-provided
	// known speakers.
	IdentificationAttribution IdentificationLevel = "attribution"
	// IdentificationProviderID means the provider itself identified speakers
	// by name or role.
	IdentificationProviderID IdentificationLevel = "provider_identification"
	// IdentificationBiometric means identification against enrolled voice
	// prints; reserved, no shipped provider reaches it.
	IdentificationBiometric IdentificationLevel = "biometric"
)

// Values for [Options].SpeakerType: whether KnownValues and KnownSpeakers
// describe people by name or by role.
const (
	SpeakerTypeName = "name"
	SpeakerTypeRole = "role"
)

// Options configures a single diarization request. Empty options mean
// "transcribe only"; providers must not enable speaker work unless
// WantsDiarization returns true.
type Options struct {
	Enabled              bool           `json:"enabled,omitempty"`
	Diarization          bool           `json:"diarization,omitempty"`
	Identification       bool           `json:"identification,omitempty"`
	Attribution          bool           `json:"attribution,omitempty"`
	ProviderProfileID    string         `json:"providerProfileId,omitempty"`
	Model                string         `json:"model,omitempty"`
	DiarizationModel     string         `json:"diarizationModel,omitempty"`
	Language             string         `json:"language,omitempty"`
	SpeakersExpected     int            `json:"speakersExpected,omitempty"`
	MinSpeakersExpected  int            `json:"minSpeakersExpected,omitempty"`
	MaxSpeakersExpected  int            `json:"maxSpeakersExpected,omitempty"`
	SpeakerType          string         `json:"speakerType,omitempty"`
	KnownValues          []string       `json:"knownValues,omitempty"`
	KnownSpeakers        []KnownSpeaker `json:"knownSpeakers,omitempty"`
	PreferStreaming      bool           `json:"preferStreaming,omitempty"`
	AllowProviderMapping bool           `json:"allowProviderMapping,omitempty"`
}

// WantsDiarization reports whether any field of o asks for speaker work:
// an explicit flag, speaker-count bounds, or known speaker context.
// Providers must leave diarization off when it returns false.
func (o Options) WantsDiarization() bool {
	return o.Enabled || o.Diarization || o.Identification || o.Attribution ||
		o.SpeakersExpected > 0 || o.MinSpeakersExpected > 0 || o.MaxSpeakersExpected > 0 ||
		len(o.KnownValues) > 0 || len(o.KnownSpeakers) > 0
}

// WantsIdentification reports whether o asks for more than anonymous
// labels: Identification or Attribution is set, or known values or
// speakers were supplied.
func (o Options) WantsIdentification() bool {
	return o.Identification || o.Attribution || len(o.KnownValues) > 0 || len(o.KnownSpeakers) > 0
}

// Normalized returns o with string fields trimmed, SpeakerType lowercased
// and defaulted to [SpeakerTypeName] when identification is wanted (cleared
// otherwise), KnownValues trimmed and de-duplicated, and blank
// KnownSpeakers entries dropped.
func (o Options) Normalized() Options {
	o.ProviderProfileID = strings.TrimSpace(o.ProviderProfileID)
	o.Model = strings.TrimSpace(o.Model)
	o.DiarizationModel = strings.TrimSpace(o.DiarizationModel)
	o.Language = strings.TrimSpace(o.Language)
	o.SpeakerType = strings.ToLower(strings.TrimSpace(o.SpeakerType))
	switch o.SpeakerType {
	case SpeakerTypeName, SpeakerTypeRole:
	default:
		if o.WantsIdentification() {
			o.SpeakerType = SpeakerTypeName
		} else {
			o.SpeakerType = ""
		}
	}
	o.KnownValues = cleanStrings(o.KnownValues)
	o.KnownSpeakers = cleanKnownSpeakers(o.KnownSpeakers)
	return o
}

// KnownSpeaker is app-provided context for attribution or provider-supported
// speaker identification. It is not a biometric enrollment record.
type KnownSpeaker struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Role        string `json:"role,omitempty"`
	Description string `json:"description,omitempty"`
}

// Speaker is the canonical person/speaker entry for a diarized transcript.
type Speaker struct {
	Label                 string  `json:"label"`
	PersonID              string  `json:"personId,omitempty"`
	DisplayName           string  `json:"displayName,omitempty"`
	Role                  string  `json:"role,omitempty"`
	Confidence            float64 `json:"confidence,omitempty"`
	AttributionConfidence float64 `json:"attributionConfidence,omitempty"`
}

// SpeakerWord is the word-level speaker attribution surface shared by
// Deepgram, AssemblyAI, and Google. Times are milliseconds from audio start.
type SpeakerWord struct {
	Text                  string  `json:"text"`
	StartMs               int64   `json:"startMs,omitempty"`
	EndMs                 int64   `json:"endMs,omitempty"`
	Confidence            float64 `json:"confidence,omitempty"`
	SpeakerLabel          string  `json:"speakerLabel,omitempty"`
	SpeakerConfidence     float64 `json:"speakerConfidence,omitempty"`
	PersonID              string  `json:"personId,omitempty"`
	DisplayName           string  `json:"displayName,omitempty"`
	Role                  string  `json:"role,omitempty"`
	AttributionConfidence float64 `json:"attributionConfidence,omitempty"`
}

// SpeakerSegment groups contiguous speech attributed to one speaker.
type SpeakerSegment struct {
	Text                  string        `json:"text"`
	StartMs               int64         `json:"startMs,omitempty"`
	EndMs                 int64         `json:"endMs,omitempty"`
	SpeakerLabel          string        `json:"speakerLabel,omitempty"`
	SpeakerConfidence     float64       `json:"speakerConfidence,omitempty"`
	PersonID              string        `json:"personId,omitempty"`
	DisplayName           string        `json:"displayName,omitempty"`
	Role                  string        `json:"role,omitempty"`
	AttributionConfidence float64       `json:"attributionConfidence,omitempty"`
	Words                 []SpeakerWord `json:"words,omitempty"`
}

// DiarizationResult is the canonical output contract for speaker add-ons.
type DiarizationResult struct {
	Provider string              `json:"provider,omitempty"`
	Model    string              `json:"model,omitempty"`
	Level    IdentificationLevel `json:"level,omitempty"`
	Text     string              `json:"text,omitempty"`
	Language string              `json:"language,omitempty"`
	Speakers []Speaker           `json:"speakers,omitempty"`
	Segments []SpeakerSegment    `json:"segments,omitempty"`
	Words    []SpeakerWord       `json:"words,omitempty"`
}

// Provider is the public add-on provider contract for applications that want
// diarization independent from the STT router.
type Provider interface {
	Diarize(ctx context.Context, audio []byte, opts Options) (*DiarizationResult, error)
	Name() string
	Health(ctx context.Context) error
}

// ProviderProfile is a provider-neutral capability matrix row for Notion or
// setup UIs. Values are strings by design to avoid coupling this subpackage to
// the parent speechkit catalog package.
type ProviderProfile struct {
	ID                  string       `json:"id"`
	Name                string       `json:"name"`
	Provider            string       `json:"provider"`
	Framework           string       `json:"framework"`
	Mode                string       `json:"mode"`
	ProviderKind        string       `json:"providerKind"`
	ExecutionMode       string       `json:"executionMode"`
	Streaming           bool         `json:"streaming"`
	Batch               bool         `json:"batch"`
	IdentificationLevel string       `json:"identificationLevel"`
	Capabilities        []Capability `json:"capabilities,omitempty"`
	RequiredEnvVars     []string     `json:"requiredEnvVars,omitempty"`
	Status              string       `json:"status,omitempty"`
}

// DefaultProviderProfiles returns the built-in speaker capability matrix:
// the Deepgram and AssemblyAI batch and streaming diarization profiles,
// AssemblyAI speaker identification, and the cascaded Voice Agent
// attribution fallback. Each call builds a fresh slice.
func DefaultProviderProfiles() []ProviderProfile {
	return []ProviderProfile{
		{
			ID:                  "stt.deepgram.nova-3-diarization",
			Name:                "Deepgram Diarization",
			Provider:            "Deepgram",
			Framework:           "Deepgram Speech-to-Text /v1/listen",
			Mode:                "dictation",
			ProviderKind:        "direct_provider",
			ExecutionMode:       "deepgram_api",
			Streaming:           false,
			Batch:               true,
			IdentificationLevel: string(IdentificationDiarization),
			Capabilities:        []Capability{CapabilityDiarization},
			RequiredEnvVars:     []string{"DEEPGRAM_API_KEY"},
			Status:              "implemented",
		},
		{
			ID:                  "speaker.deepgram.streaming-diarization",
			Name:                "Deepgram Streaming Diarization",
			Provider:            "Deepgram",
			Framework:           "Deepgram Streaming Speech-to-Text /v1/listen",
			Mode:                "voice_agent",
			ProviderKind:        "direct_provider",
			ExecutionMode:       "deepgram_api",
			Streaming:           true,
			Batch:               false,
			IdentificationLevel: string(IdentificationDiarization),
			Capabilities:        []Capability{CapabilityDiarization},
			RequiredEnvVars:     []string{"DEEPGRAM_API_KEY"},
			Status:              "implemented",
		},
		{
			ID:                  "stt.assemblyai.universal-diarization",
			Name:                "AssemblyAI Diarization",
			Provider:            "AssemblyAI",
			Framework:           "AssemblyAI /v2/upload + /v2/transcript",
			Mode:                "dictation",
			ProviderKind:        "direct_provider",
			ExecutionMode:       "assemblyai_api",
			Streaming:           false,
			Batch:               true,
			IdentificationLevel: string(IdentificationDiarization),
			Capabilities:        []Capability{CapabilityDiarization},
			RequiredEnvVars:     []string{"ASSEMBLYAI_API_KEY"},
			Status:              "implemented",
		},
		{
			ID:                  "speaker.assemblyai.identification",
			Name:                "AssemblyAI Speaker Identification",
			Provider:            "AssemblyAI",
			Framework:           "AssemblyAI Speech Understanding speaker_identification",
			Mode:                "dictation",
			ProviderKind:        "direct_provider",
			ExecutionMode:       "assemblyai_api",
			Streaming:           false,
			Batch:               true,
			IdentificationLevel: string(IdentificationProviderID),
			Capabilities:        []Capability{CapabilityIdentification, CapabilityAttribution},
			RequiredEnvVars:     []string{"ASSEMBLYAI_API_KEY"},
			Status:              "implemented",
		},
		{
			ID:                  "speaker.assemblyai.streaming-diarization",
			Name:                "AssemblyAI Streaming Diarization",
			Provider:            "AssemblyAI",
			Framework:           "AssemblyAI Streaming v3 WebSocket",
			Mode:                "voice_agent",
			ProviderKind:        "direct_provider",
			ExecutionMode:       "assemblyai_api",
			Streaming:           true,
			Batch:               false,
			IdentificationLevel: string(IdentificationDiarization),
			Capabilities:        []Capability{CapabilityDiarization},
			RequiredEnvVars:     []string{"ASSEMBLYAI_API_KEY"},
			Status:              "implemented",
		},
		{
			ID:                  "stt.google.latest-long-diarization",
			Name:                "Google STT Diarization",
			Provider:            "Google",
			Framework:           "Google Cloud Speech-to-Text v1 diarizationConfig",
			Mode:                "dictation",
			ProviderKind:        "direct_provider",
			ExecutionMode:       "google_api",
			Streaming:           false,
			Batch:               true,
			IdentificationLevel: string(IdentificationDiarization),
			Capabilities:        []Capability{CapabilityDiarization},
			RequiredEnvVars:     []string{"SPEECHKIT_GOOGLE_STT_API_KEY"},
			Status:              "implemented",
		},
		{
			// Google STT v2 StreamingRecognize cannot diarize (diarization is
			// BatchRecognize/Recognize-only per the Chirp-3 docs); this profile is a
			// realtime TRANSCRIPTION source and advertises no speaker capability. The
			// diarization limitation is documented in the capability matrix
			// (speaker_diarization / Google / not_supported).
			ID:                  "speaker.google.v2-streaming-transcription",
			Name:                "Google STT v2 Streaming Transcription",
			Provider:            "Google",
			Framework:           "Google Cloud Speech-to-Text v2 StreamingRecognize",
			Mode:                "voice_agent",
			ProviderKind:        "direct_provider",
			ExecutionMode:       "google_api",
			Streaming:           true,
			Batch:               false,
			IdentificationLevel: string(IdentificationNone),
			Capabilities:        []Capability{},
			RequiredEnvVars:     []string{"GOOGLE_APPLICATION_CREDENTIALS", "SPEECHKIT_GOOGLE_STT_CREDENTIALS_JSON"},
			Status:              "implemented",
		},
		{
			ID:                  "voice_agent.cascaded.speaker-attribution",
			Name:                "Voice Agent Cascaded Speaker Attribution",
			Provider:            "Voice Agent cascaded fallback",
			Framework:           "Voice Agent transcript frames + SpeakerFrame",
			Mode:                "voice_agent",
			ProviderKind:        "mixed",
			ExecutionMode:       "cascaded",
			Streaming:           true,
			Batch:               true,
			IdentificationLevel: "inherits STT provider",
			Capabilities:        []Capability{CapabilityAttribution},
			Status:              "partial",
		},
	}
}

// NormalizeSpeakerLabel converts a provider's raw speaker value (a number,
// string, or nil) to the canonical "speaker_<id>" form. Unknown and
// unattributed markers, nil, and blank input return "" so the speech
// stays unattributed.
func NormalizeSpeakerLabel(raw any) string {
	label := strings.TrimSpace(fmt.Sprint(raw))
	if label == "" || label == "<nil>" {
		return ""
	}
	lower := strings.ToLower(label)
	switch lower {
	case "unknown", "speaker_unknown", "speaker unknown", "unattributed", "speaker_unattributed":
		return ""
	}
	if strings.HasPrefix(lower, "speaker_") || strings.HasPrefix(lower, "speaker ") {
		return strings.ReplaceAll(label, " ", "_")
	}
	return "speaker_" + label
}

// BuildSegmentsFromWords groups words into [SpeakerSegment] runs, starting
// a new segment whenever the speaker label, person, display name, or role
// changes. Blank words are skipped, segment text joins the words with
// spaces, and confidences are averaged over the words that reported one.
// It returns nil for no words.
func BuildSegmentsFromWords(words []SpeakerWord) []SpeakerSegment {
	if len(words) == 0 {
		return nil
	}
	var segments []SpeakerSegment
	var current *SpeakerSegment
	for _, word := range words {
		if strings.TrimSpace(word.Text) == "" {
			continue
		}
		label := strings.TrimSpace(word.SpeakerLabel)
		if current == nil || current.SpeakerLabel != label || current.PersonID != word.PersonID || current.DisplayName != word.DisplayName || current.Role != word.Role {
			if current != nil {
				current.Text = joinWordText(current.Words)
				segments = append(segments, *current)
			}
			current = &SpeakerSegment{
				StartMs:               word.StartMs,
				EndMs:                 word.EndMs,
				SpeakerLabel:          label,
				SpeakerConfidence:     word.SpeakerConfidence,
				PersonID:              word.PersonID,
				DisplayName:           word.DisplayName,
				Role:                  word.Role,
				AttributionConfidence: word.AttributionConfidence,
				Words:                 []SpeakerWord{word},
			}
			continue
		}
		current.EndMs = word.EndMs
		current.Words = append(current.Words, word)
		current.SpeakerConfidence = averageNonZero(current.SpeakerConfidence, word.SpeakerConfidence, len(current.Words))
		current.AttributionConfidence = averageNonZero(current.AttributionConfidence, word.AttributionConfidence, len(current.Words))
	}
	if current != nil {
		current.Text = joinWordText(current.Words)
		segments = append(segments, *current)
	}
	return segments
}

// SpeakersFromSegments lists the distinct speakers of segments in order of
// first appearance, keyed by the first non-empty of person ID, display
// name, role, and label. Confidence and AttributionConfidence keep the
// first non-zero values seen; segments without any identity are skipped.
func SpeakersFromSegments(segments []SpeakerSegment) []Speaker {
	seen := map[string]int{}
	var out []Speaker
	for _, segment := range segments {
		key := firstNonEmpty(segment.PersonID, segment.DisplayName, segment.Role, segment.SpeakerLabel)
		if key == "" {
			continue
		}
		if idx, ok := seen[key]; ok {
			if out[idx].Confidence == 0 {
				out[idx].Confidence = segment.SpeakerConfidence
			}
			if out[idx].AttributionConfidence == 0 {
				out[idx].AttributionConfidence = segment.AttributionConfidence
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, Speaker{
			Label:                 segment.SpeakerLabel,
			PersonID:              segment.PersonID,
			DisplayName:           segment.DisplayName,
			Role:                  segment.Role,
			Confidence:            segment.SpeakerConfidence,
			AttributionConfidence: segment.AttributionConfidence,
		})
	}
	return out
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cleanKnownSpeakers(values []KnownSpeaker) []KnownSpeaker {
	out := make([]KnownSpeaker, 0, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.DisplayName = strings.TrimSpace(value.DisplayName)
		value.Role = strings.TrimSpace(value.Role)
		value.Description = strings.TrimSpace(value.Description)
		if value.ID == "" && value.DisplayName == "" && value.Role == "" && value.Description == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func joinWordText(words []SpeakerWord) string {
	parts := make([]string, 0, len(words))
	for _, word := range words {
		if text := strings.TrimSpace(word.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func averageNonZero(current, next float64, count int) float64 {
	if next == 0 {
		return current
	}
	if current == 0 || count <= 1 {
		return next
	}
	return ((current * float64(count-1)) + next) / float64(count)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
