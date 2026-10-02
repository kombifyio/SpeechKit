//go:build linux

package voiceagent

import (
	"context"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// ProviderFactory builds a Framework kernel voice-agent provider on demand.
// Each WebSocket session gets its own provider instance so concurrent
// sessions don't share realtime-provider state.
//
// The concrete production implementation returns a provider-specific adapter;
// tests supply a fake that records frames and replies with canned audio.
type ProviderFactory interface {
	NewProvider() LiveProviderAdapter
}

// LiveProviderAdapter is the minimal slice of the kernel's LiveProvider
// interface the Server-Target adapter needs. Keeping it narrow makes it
// trivial for tests to stub without pulling in provider SDK types.
type LiveProviderAdapter interface {
	Connect(ctx context.Context, cfg LiveConfigFrame) error
	SendAudio(chunk []byte) error
	SendAudioStreamEnd() error
	SendText(text string) error
	Receive(ctx context.Context) (*LiveMessage, error)
	Close() error
	Name() string
}

// LiveInstructionUpdater is implemented by providers that can update their
// active host instructions without treating the update as a user turn.
type LiveInstructionUpdater interface {
	UpdateInstructions(ctx context.Context, cfg LiveConfigFrame) error
}

// LiveToolResponder is implemented by providers that accept host-side tool
// results from the client.
type LiveToolResponder interface {
	SendToolResponse(ToolResponseFrame) error
}

// LiveResponseCanceller is implemented by providers whose realtime protocol
// has a client-initiated cancel for the in-flight agent response (OpenAI
// Realtime: `response.cancel`). Deepgram Voice Agent and AssemblyAI Voice
// Agent expose no such client message — interruption there
// is speech-driven server-side — so their sessions rely on the adapter
// suppressing downlink audio until the current turn ends (see MsgCancel).
type LiveResponseCanceller interface {
	CancelResponse() error
}

// LiveConfigFrame is the subset of configuration the adapter derives from a
// StartFrame and the persona/role resolver. Kept as a separate type so the
// test double doesn't need to depend on the kernel's concrete LiveConfig
// (which may embed provider-specific types).
type LiveConfigFrame struct {
	PersonaID string
	RoleID    string
	// Sequence metadata is internal runtime state derived from the
	// persona/role resolver. It lets the WebSocket adapter report and advance
	// workflow steps without coupling to the persona package.
	SequenceID         string
	SequenceCompletion string
	SequenceMaxTurns   int
	StepID             string
	StepIndex          int
	StepCount          int
	StepInstruction    string
	StepExitCriteria   string
	StepMaxTurns       int

	Model string
	// FallbackModel is forwarded to providers that support same-provider
	// fallback (when a provider supports a same-provider retry after the primary
	// connect fails). Empty disables the fallback.
	FallbackModel    string
	APIKey           string
	Voice            string
	SystemPrompt     string
	RefinementPrompt string
	Locale           string
	// Raw activity-detection passthrough; adapter translates to the
	// kernel's internal types.
	Automatic         bool
	StartSensitivity  string
	EndSensitivity    string
	PrefixPaddingMs   int32
	SilenceDurationMs int32
	ActivityHandling  string
	TurnCoverage      string
	Speaker           speaker.Options
	// Tools are server-executed tool definitions merged in by the adapter
	// from the SessionToolRouter (tool bridge). Provider bridges that support
	// session tools map these onto the kernel's LiveConfig.Tools; providers
	// without tool support ignore them.
	Tools []ToolDefinitionFrame
	// Registered-agent fields are trusted session state captured at mint time,
	// never values accepted from the WebSocket start frame.
	AgentTargetID   string
	AgentEndpoint   string
	CapabilityLease string
	VoiceSessionID  string
	AISessionID     string
	OwnerUserID     string
	OwnerOrgID      string
	OwnerPlan       string
	// OboSubjectToken is the short-lived delegated AI session credential
	// captured at mint time. It is never the owner's raw login JWT.
	OboSubjectToken string
}

// LiveMessage is the subset of pkg/speechkit/voiceagent/live.LiveMessage the
// adapter relays to the client. Matching field names keep the translation
// trivial.
type LiveMessage struct {
	EventType              string
	EventTypes             []string
	ProviderMetadata       map[string]any
	Audio                  []byte
	Done                   bool
	OutputTranscript       string
	OutputTranscriptDone   bool
	InputTranscript        string
	InputTranscriptDone    bool
	InputSpeakerLabel      string
	InputPersonID          string
	InputDisplayName       string
	InputSpeakerConfidence float64
	ToolCalls              []ToolCall
	Interrupted            bool
	GoAway                 bool
	SessionResumable       bool
}

type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// PersonaResolver derives a LiveConfigFrame from a StartFrame. The server's
// persona registry implements this in M5; for M4 a stub resolver is used
// that echoes the StartFrame through with sensible defaults.
type PersonaResolver interface {
	Resolve(StartFrame) (LiveConfigFrame, error)
}
