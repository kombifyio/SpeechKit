package cascaded

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

var (
	// ErrNotConfigured is returned by Connect when a required dependency
	// (STT or Agent) was not supplied to New.
	ErrNotConfigured = errors.New("cascaded: dependency not configured")
	// ErrClosed is returned by Receive once the provider has been closed.
	ErrClosed = errors.New("cascaded: provider closed")
)

// Provider is a turn-based STT -> LLM -> TTS voice agent. It implements
// the small contract documented on this type's methods; [LiveProvider]
// wraps it into the richer [live.LiveProvider] interface realtime session
// hosts expect.
type Provider struct {
	stt             STT
	agent           Agent
	tts             TTS
	speakerStreamer speaker.StreamingProvider
	cfg             Config

	// Per-session live config from Connect()
	locale       string
	voice        string
	systemPrompt string
	refinement   string
	speaker      speaker.Options

	// Turn state
	mu          sync.Mutex
	buffer      []byte
	lastVoiceAt time.Time
	history     []conversationTurn

	// Processing channels
	messages  chan *Message
	triggers  chan struct{}
	closeOnce sync.Once
	closedCh  chan struct{}

	speakerStreamMu     sync.Mutex
	speakerStream       speaker.SpeakerStream
	speakerStreamCancel context.CancelFunc
}

// STT is the STT surface the provider uses. In production this is the
// same *stt.Router that serves /v1/dictation/transcribe.
type STT interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Agent is the LLM surface the provider uses. In production this is the
// Genkit agent flow defined by flows.DefineAgentFlow.
type Agent interface {
	Run(ctx context.Context, input AgentInput) (AgentOutput, error)
}

// TTS is the TTS surface the provider uses. Optional; nil drops audio
// frames and keeps OutputTranscript-only emission.
type TTS interface {
	Synthesize(ctx context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error)
}

// Deps bundles everything the bootstrap hands to the provider.
type Deps struct {
	STT             STT
	Agent           Agent
	TTS             TTS
	SpeakerStreamer speaker.StreamingProvider
	Config          Config
}

// NewProvider constructs a provider without starting background work.
// Connect() initializes the goroutine that performs turn processing.
func NewProvider(deps Deps) *Provider {
	cfg := deps.Config.WithDefaults()
	return &Provider{
		stt:             deps.STT,
		agent:           deps.Agent,
		tts:             deps.TTS,
		speakerStreamer: deps.SpeakerStreamer,
		cfg:             cfg,
		messages:        make(chan *Message, 16),
		triggers:        make(chan struct{}, 4),
		closedCh:        make(chan struct{}),
	}
}

// Bridging a host's LLM flow to Agent is the host's job so this public package
// carries no AI-runtime dependency; SpeechKit's own server does it in
// internal/ai/flows.NewCascadedAgent. Embedders supply their own Agent.
