// Package live exposes the low-level Voice Agent realtime-protocol types.
//
// This is the public-API surface for building custom realtime providers
// (Gemini Live, OpenAI Realtime, or a third-party WebSocket model) and
// for libraries that drive a SpeechKit session directly without going
// through the higher-level [Service] in the parent package.
//
// Most embedders use the parent [voiceagent] package or
// [github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit] instead;
// reach for this package only when you need to plug in your own provider
// or read the raw [LiveMessage] stream.
package live

import (
	"context"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// State represents the current state of a Voice Agent session.
type State string

const (
	StateInactive     State = "inactive"
	StateConnecting   State = "connecting"
	StateListening    State = "listening"
	StateProcessing   State = "processing"
	StateSpeaking     State = "speaking"
	StateRecovering   State = "recovering"
	StateDeactivating State = "deactivating"
)

// ThinkingLevel controls how much deliberate reasoning Gemini Live should spend.
type ThinkingLevel string

const (
	ThinkingLevelOff    ThinkingLevel = "off"
	ThinkingLevelLow    ThinkingLevel = "low"
	ThinkingLevelMedium ThinkingLevel = "medium"
	ThinkingLevelHigh   ThinkingLevel = "high"
)

// StartSensitivity controls how aggressively automatic activity detection commits speech start.
type StartSensitivity string

const (
	StartSensitivityLow    StartSensitivity = "low"
	StartSensitivityMedium StartSensitivity = "medium"
	StartSensitivityHigh   StartSensitivity = "high"
)

// EndSensitivity controls how aggressively automatic activity detection commits speech end.
type EndSensitivity string

const (
	EndSensitivityLow    EndSensitivity = "low"
	EndSensitivityMedium EndSensitivity = "medium"
	EndSensitivityHigh   EndSensitivity = "high"
)

// ActivityHandling controls what Gemini Live should do when new activity starts.
type ActivityHandling string

const (
	ActivityHandlingUnspecified               ActivityHandling = ""
	ActivityHandlingNoInterrupt               ActivityHandling = "no_interrupt"
	ActivityHandlingStartOfActivityInterrupts ActivityHandling = "start_of_activity_interrupts"
)

// TurnCoverage controls how the live API builds a user turn from incoming activity.
type TurnCoverage string

const (
	TurnCoverageUnspecified               TurnCoverage = ""
	TurnCoverageTurnIncludesOnlyActivity  TurnCoverage = "turn_includes_only_activity"
	TurnCoverageTurnIncludesAllInput      TurnCoverage = "turn_includes_all_input"
	TurnCoverageTurnIncludesAudioActivity TurnCoverage = "turn_includes_audio_activity"
)

// ThinkingPolicy defines optional Gemini Live thinking behavior.
type ThinkingPolicy struct {
	Enabled         bool
	IncludeThoughts bool
	ThinkingBudget  int32
	ThinkingLevel   ThinkingLevel
}

// ContextCompressionPolicy defines how the live API should compress long sessions.
type ContextCompressionPolicy struct {
	Enabled       bool
	TriggerTokens int64
	TargetTokens  int64
}

// ActivityDetectionPolicy defines server-side VAD/session turn behavior.
type ActivityDetectionPolicy struct {
	Automatic         bool
	StartSensitivity  StartSensitivity
	EndSensitivity    EndSensitivity
	PrefixPaddingMs   int32
	SilenceDurationMs int32
	ActivityHandling  ActivityHandling
	TurnCoverage      TurnCoverage
}

// LivePolicies configures Google Live API features that shape Voice Agent behavior.
type LivePolicies struct {
	EnableInputAudioTranscription  bool
	EnableOutputAudioTranscription bool
	EnableAffectiveDialog          bool
	Thinking                       ThinkingPolicy
	ContextCompression             ContextCompressionPolicy
	ActivityDetection              ActivityDetectionPolicy
}

// ToolBehavior controls whether the model waits for a tool result.
type ToolBehavior string

const (
	ToolBehaviorUnspecified ToolBehavior = ""
	ToolBehaviorBlocking    ToolBehavior = "blocking"
	ToolBehaviorNonBlocking ToolBehavior = "non_blocking"
)

// ToolResponseScheduling controls how a non-blocking tool result is reintroduced into the conversation.
type ToolResponseScheduling string

const (
	ToolResponseSchedulingUnspecified ToolResponseScheduling = ""
	ToolResponseSchedulingSilent      ToolResponseScheduling = "silent"
	ToolResponseSchedulingWhenIdle    ToolResponseScheduling = "when_idle"
	ToolResponseSchedulingInterrupt   ToolResponseScheduling = "interrupt"
)

// ToolDefinition exposes a host-side action the Voice Agent may call.
type ToolDefinition struct {
	Name                 string
	Description          string
	ParametersJSONSchema map[string]any
	ResponseJSONSchema   map[string]any
	Behavior             ToolBehavior
}

// ToolCall is a host-side action request emitted by the Voice Agent runtime.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResponse resolves a previously emitted tool call.
type ToolResponse struct {
	ID       string
	Name     string
	Response map[string]any

	Scheduling   ToolResponseScheduling
	WillContinue *bool
}

// LiveProvider abstracts a real-time audio-to-audio model connection.
type LiveProvider interface {
	// Connect establishes a WebSocket session to the real-time model.
	Connect(ctx context.Context, cfg LiveConfig) error

	// SendAudio streams PCM audio chunks to the model.
	// Format: 16-bit signed int, little-endian, mono, 16kHz.
	SendAudio(chunk []byte) error

	// SendAudioStreamEnd signals that microphone input for the current turn ended.
	SendAudioStreamEnd() error

	// Receive blocks until the next server message arrives.
	// Returns audio chunks and/or text from the model.
	Receive(ctx context.Context) (*LiveMessage, error)

	// SendText injects a text prompt into the session (for idle reminders).
	SendText(text string) error

	// SendToolResponse sends the result of a host-side tool invocation back to the model.
	SendToolResponse(response ToolResponse) error

	// Close terminates the WebSocket session.
	Close() error

	// Name returns the provider identifier.
	Name() string
}

// LiveInstructionUpdater is optionally implemented by providers that can
// refresh active host instructions without treating the update as a user turn.
type LiveInstructionUpdater interface {
	UpdateInstructions(ctx context.Context, cfg LiveConfig) error
}

// LiveReconnector is an optional interface for providers that support session reconnection.
type LiveReconnector interface {
	Reconnect(ctx context.Context) error
}

// LiveConfig configures a real-time session.
type LiveConfig struct {
	Model string // e.g. "gemini-3.1-flash-live-preview"
	// FallbackModel is tried when the primary Model's Connect fails. Empty
	// disables the fallback. Typical pairing in 2026: a preview model as
	// Model + the last GA model as FallbackModel, so transient preview
	// outages don't take down a Voice Agent deployment.
	FallbackModel    string
	APIKey           string
	Voice            string // Voice name
	FrameworkPrompt  string
	RefinementPrompt string
	VocabularyHint   string
	Locale           string
	// Region is the Google Cloud region the caller's API key / project is
	// pinned to (e.g. "europe-west3", "us-central1"). Used by providers that
	// support regional endpoints. For Gemini Live (as of May 2026) the API
	// exposes a single global WebSocket endpoint, so this field does NOT
	// redirect traffic — it is logged at connect time for compliance evidence
	// (byok.key_updated audit event) and reserved for future regional routing
	// once Google publishes per-region hostnames. Data residency is controlled
	// at the Google Cloud project level; both the project region AND this field
	// must agree for audit records to be accurate.
	// See docs/compliance/byok-gemini-region-pinning.md.
	Region          string
	Policies        LivePolicies
	Tools           []ToolDefinition
	Workflow        *WorkflowConfig
	Speaker         speaker.Options
	Options         provideropts.Values
	ProviderOptions provideropts.Values
}

// LiveMessage is a message received from the real-time model.
type LiveMessage struct {
	Audio []byte // PCM audio chunk (24kHz 16-bit mono)
	Text  string // Text transcript (may be partial or empty)
	Done  bool   // True when the model's turn is complete

	// Transcription fields (populated when transcription is enabled).
	InputTranscript        string  // User speech transcribed by server
	InputTranscriptDone    bool    // True when input transcription segment is final
	InputSpeakerLabel      string  // Optional diarized speaker label for input transcript segments
	InputPersonID          string  // Optional known person id for input transcript segments
	InputDisplayName       string  // Optional known display name for input transcript segments
	InputSpeakerConfidence float64 // Optional speaker-label confidence
	OutputTranscript       string  // Model speech transcribed by server
	OutputTranscriptDone   bool    // True when output transcription segment is final

	ToolCalls               []ToolCall
	ToolCallCancellationIDs []string
	Interrupted             bool // True when user interrupted model (barge-in)
	GoAway                  bool // True when server signals imminent session end
}

// Callbacks are event handlers for UI integration with a low-level Session.
//
// This is the rich Callbacks struct used by the low-level Session runtime.
// The higher-level [voiceagent.Callbacks] in the parent package carries
// only the three most-common handlers (OnAudio, OnText, OnError) and is
// the right shape for the embedded [voiceagent.Service]; reach for this
// struct only when wiring a custom realtime host.
type Callbacks struct {
	OnStateChange          func(state State)
	OnAudio                func(audio []byte) // Audio chunk to play
	OnText                 func(text string)  // Text for display (speech bubble)
	OnError                func(err error)
	OnInputTranscript      func(text string, done bool) // User speech transcribed
	OnOutputTranscript     func(text string, done bool) // Model speech transcribed
	OnToolCall             func(call ToolCall)
	OnToolCallCancellation func(ids []string)
	OnInterrupted          func() // User interrupted model (barge-in)
	OnSessionEnd           func() // Session ended (error, GoAway failure, or deactivation)
}

// IdleConfig configures the idle timer behavior.
type IdleConfig struct {
	ReminderAfter   time.Duration // Default: 5 minutes
	DeactivateAfter time.Duration // Default: 15 minutes
}

// WorkflowConfig describes a deterministic, step-based Voice Agent behavior
// sequence. Durations are intentionally expressed as turns instead of wall
// clock time so tests and local installs can exercise long moderation flows
// quickly.
type WorkflowConfig struct {
	SequenceID string
	Completion string
	MaxTurns   int
	BasePrompt string
	Steps      []WorkflowStep
	// InitialStep selects the first active step. Out-of-range values fall
	// back to zero.
	InitialStep int
}

// WorkflowStep describes one stage in a Voice Agent workflow.
type WorkflowStep struct {
	ID           string
	Instruction  string
	ExitCriteria string
	RequireTools []string
	MaxTurns     int
}
