package speechkit

import (
	"context"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// DictationRun is the public record produced by a completed Dictation request.
// Hosts may persist it directly or map it into their own history model.
type DictationRun struct {
	ID               string                     `json:"id,omitempty"`
	Transcript       Transcript                 `json:"transcript"`
	Finalization     TranscriptionFinalization  `json:"finalization"`
	StartedAt        time.Time                  `json:"startedAt,omitempty"`
	CompletedAt      time.Time                  `json:"completedAt,omitempty"`
	ProviderProfile  string                     `json:"providerProfile,omitempty"`
	DictionaryTerms  []string                   `json:"dictionaryTerms,omitempty"`
	AudioDurationMs  int64                      `json:"audioDurationMs,omitempty"`
	ProcessingTimeMs int64                      `json:"processingTimeMs,omitempty"`
	Speakers         *speaker.DiarizationResult `json:"speakers,omitempty"`
}

// AssistSurfaceDecision describes where an Assist result should be presented.
type AssistSurfaceDecision string

// Surface decisions for an [AssistResult]: present it in the Assist panel,
// insert it into the editable target, replace the selection, only acknowledge
// that an action ran, or show nothing. Tool results default to
// AssistSurfaceActionAck; a silent tool result may fall through to the
// generator.
const (
	AssistSurfacePanel     AssistSurfaceDecision = "panel"
	AssistSurfaceInsert    AssistSurfaceDecision = "insert"
	AssistSurfaceReplace   AssistSurfaceDecision = "replace"
	AssistSurfaceActionAck AssistSurfaceDecision = "action_ack"
	AssistSurfaceSilent    AssistSurfaceDecision = "silent"
)

// AssistRequest is the mode-scoped input for Assist integrations.
type AssistRequest struct {
	Text              string                     `json:"text"`
	Locale            string                     `json:"locale,omitempty"`
	Selection         string                     `json:"selection,omitempty"`
	Context           string                     `json:"context,omitempty"`
	EditableTarget    bool                       `json:"editableTarget,omitempty"`
	ProviderProfileID string                     `json:"providerProfileId,omitempty"`
	SessionKey        string                     `json:"sessionKey,omitempty"`
	SpeakerOptions    speaker.Options            `json:"speakerOptions,omitempty"`
	Speakers          *speaker.DiarizationResult `json:"speakers,omitempty"`
	// Target is the host destination for insertion or execution (see
	// OutputTarget). Assist services carry it unchanged into the tool call of a
	// matched utility and of a multi-turn follow-up; it is host-local state and
	// is never serialised.
	Target any `json:"-"`
}

// AudioData carries optional synthesized audio without making AssistResult
// non-comparable for existing SDK consumers.
type AudioData []byte

// NewAudioData copies data into a new [AudioData]. It returns nil for empty
// input so results without audio stay comparable.
func NewAudioData(data []byte) *AudioData {
	if len(data) == 0 {
		return nil
	}
	clone := AudioData(append([]byte(nil), data...))
	return &clone
}

// Bytes returns a copy of the audio bytes, or nil for a nil receiver.
func (a *AudioData) Bytes() []byte {
	if a == nil {
		return nil
	}
	return append([]byte(nil), (*a)...)
}

// Len returns the number of audio bytes, or 0 for a nil receiver.
func (a *AudioData) Len() int {
	if a == nil {
		return 0
	}
	return len(*a)
}

// AssistResult is the public one-shot output contract for Assist Mode.
type AssistResult struct {
	Text       string                     `json:"text"`
	SpeakText  string                     `json:"speakText,omitempty"`
	Action     string                     `json:"action,omitempty"`
	Kind       string                     `json:"kind,omitempty"`
	Surface    AssistSurfaceDecision      `json:"surface"`
	ShortcutID string                     `json:"shortcutId,omitempty"`
	Locale     string                     `json:"locale,omitempty"`
	MessageID  localization.MessageID     `json:"messageId,omitempty"`
	ReasonCode string                     `json:"reasonCode,omitempty"`
	Audio      *AudioData                 `json:"audio,omitempty"`
	Format     string                     `json:"format,omitempty"`
	Speakers   *speaker.DiarizationResult `json:"speakers,omitempty"`
}

// VoiceAgentTurn is one finalized turn in a realtime or fallback dialogue.
type VoiceAgentTurn struct {
	Role              string    `json:"role"`
	Text              string    `json:"text"`
	CreatedAt         time.Time `json:"createdAt,omitempty"`
	SpeakerLabel      string    `json:"speakerLabel,omitempty"`
	PersonID          string    `json:"personId,omitempty"`
	DisplayName       string    `json:"displayName,omitempty"`
	SpeakerConfidence float64   `json:"speakerConfidence,omitempty"`
}

// VoiceAgentSessionSummary is the structured handoff produced when a Voice
// Agent session ends.
type VoiceAgentSessionSummary struct {
	Title         string   `json:"title,omitempty"`
	Summary       string   `json:"summary"`
	Ideas         []string `json:"ideas,omitempty"`
	Decisions     []string `json:"decisions,omitempty"`
	OpenQuestions []string `json:"openQuestions,omitempty"`
	NextSteps     []string `json:"nextSteps,omitempty"`
	RawText       string   `json:"rawText,omitempty"`
}

// VoiceAgentSession is the public record for a live dialogue.
type VoiceAgentSession struct {
	ID                string                     `json:"id,omitempty"`
	StartedAt         time.Time                  `json:"startedAt,omitempty"`
	EndedAt           time.Time                  `json:"endedAt,omitempty"`
	Locale            string                     `json:"locale,omitempty"`
	ProviderProfileID string                     `json:"providerProfileId,omitempty"`
	RuntimeKind       string                     `json:"runtimeKind,omitempty"`
	Turns             []VoiceAgentTurn           `json:"turns,omitempty"`
	Summary           VoiceAgentSessionSummary   `json:"summary"`
	Speakers          *speaker.DiarizationResult `json:"speakers,omitempty"`
}

// DictationService is the mode-scoped SDK contract for text-only dictation.
type DictationService interface {
	Start(context.Context) error
	Stop(context.Context) (DictationRun, error)
}

// AssistService is the mode-scoped SDK contract for one-shot utilities and
// work-product generation.
type AssistService interface {
	Process(context.Context, AssistRequest) (AssistResult, error)
}

// VoiceAgentService is the mode-scoped SDK contract for realtime dialogue.
type VoiceAgentService interface {
	Start(context.Context) error
	Stop(context.Context) (VoiceAgentSession, error)
	SendText(context.Context, string) error
	CurrentSession(context.Context) (VoiceAgentSession, error)
}
