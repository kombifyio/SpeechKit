package cascaded

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// Provider is a turn-based STT -> LLM -> TTS voice agent. It implements
// the small contract documented on this type's methods; adapters in
// internal/server/voiceagent and internal/voiceagent wrap it into the
// richer LiveProvider interface their callers expect.
type Provider struct {
	stt   STT
	agent Agent
	tts   TTS
	cfg   Config

	// Per-session live config from Connect()
	locale       string
	voice        string
	systemPrompt string
	refinement   string

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
}

// STT is the STT surface the provider uses. In production this is the
// same *internal/router.Router that serves /v1/dictation/transcribe.
type STT interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Agent is the LLM surface the provider uses. In production this is the
// Genkit agent flow defined by flows.DefineAgentFlow.
type Agent interface {
	Run(ctx context.Context, input flows.AgentInput) (flows.AgentOutput, error)
}

// TTS is the TTS surface the provider uses. Optional; nil drops audio
// frames and keeps OutputTranscript-only emission.
type TTS interface {
	Synthesize(ctx context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error)
}

// Deps bundles everything the bootstrap hands to the provider.
type Deps struct {
	STT    STT
	Agent  Agent
	TTS    TTS
	Config Config
}

// NewProvider constructs a provider without starting background work.
// Connect() initializes the goroutine that performs turn processing.
func NewProvider(deps Deps) *Provider {
	cfg := deps.Config.WithDefaults()
	return &Provider{
		stt:      deps.STT,
		agent:    deps.Agent,
		tts:      deps.TTS,
		cfg:      cfg,
		messages: make(chan *Message, 16),
		triggers: make(chan struct{}, 4),
		closedCh: make(chan struct{}),
	}
}

// Connect validates that the required dependencies are satisfied and
// starts the processor loop. Unlike Gemini Live, no external handshake
// happens.
func (p *Provider) Connect(ctx context.Context, cfg SessionConfig) error {
	if p.stt == nil {
		return errors.New("cascaded: STT router not configured")
	}
	if p.agent == nil {
		return errors.New("cascaded: Agent flow not configured (no LLM models available)")
	}
	if p.tts == nil {
		// TTS is optional - without it we still return OutputTranscript
		// text frames so the client can render subtitles or speak via
		// its own TTS stack.
		slog.Info("cascaded: TTS not configured; sessions will be text-only")
	}

	p.mu.Lock()
	p.locale = firstNonEmpty(cfg.Locale, "en")
	p.voice = cfg.Voice
	p.systemPrompt = firstNonEmpty(cfg.SystemPrompt, "")
	p.refinement = cfg.RefinementPrompt
	p.mu.Unlock()

	go p.processorLoop(ctx)
	return nil
}

// UpdateInstructions changes future-turn host instructions without
// creating a synthetic user turn.
func (p *Provider) UpdateInstructions(_ context.Context, cfg SessionConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cfg.Locale != "" {
		p.locale = cfg.Locale
	}
	if cfg.Voice != "" {
		p.voice = cfg.Voice
	}
	if cfg.SystemPrompt != "" {
		p.systemPrompt = cfg.SystemPrompt
	}
	if cfg.RefinementPrompt != "" {
		p.refinement = cfg.RefinementPrompt
	}
	return nil
}

// SendAudio appends PCM to the current turn buffer and triggers
// processing when a silence boundary is reached.
func (p *Provider) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.buffer = append(p.buffer, chunk...)
	if rms := ChunkRMS(chunk); rms > p.cfg.SilenceRMSThreshold {
		p.lastVoiceAt = time.Now()
	}
	p.maybeTriggerLocked()
	return nil
}

// SendAudioStreamEnd forces the current buffer to be treated as a
// complete turn, even if silence has not yet been detected.
func (p *Provider) SendAudioStreamEnd() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.buffer) == 0 {
		return nil
	}
	p.fire()
	return nil
}

// SendText injects a text turn (skipping STT). Useful for testing and
// for clients that already have a transcript from their own STT.
func (p *Provider) SendText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	go func() {
		if err := p.runTurn(context.Background(), text, false); err != nil {
			p.emitError("turn_failed", err.Error())
		}
	}()
	return nil
}

// Receive blocks until the next message is ready or the provider closes.
func (p *Provider) Receive(ctx context.Context) (*Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.closedCh:
		return nil, errors.New("cascaded: provider closed")
	case msg, ok := <-p.messages:
		if !ok {
			return nil, errors.New("cascaded: message channel closed")
		}
		return msg, nil
	}
}

// Close stops the processor loop and drains any pending buffer.
func (p *Provider) Close() error {
	p.closeOnce.Do(func() {
		close(p.closedCh)
	})
	return nil
}

// Name returns the provider identifier used in logs and observability.
func (p *Provider) Name() string { return "cascaded" }

// -- internals --------------------------------------------------------

func (p *Provider) maybeTriggerLocked() {
	bufMs := PCMDurationMs(p.buffer)
	if bufMs < int64(p.cfg.MinTurnMs) {
		return
	}
	now := time.Now()
	silentFor := now.Sub(p.lastVoiceAt).Milliseconds()
	if silentFor >= int64(p.cfg.SilenceTurnMs) || bufMs >= int64(p.cfg.MaxTurnMs) {
		p.fire()
	}
}

func (p *Provider) fire() {
	select {
	case p.triggers <- struct{}{}:
	default:
	}
}

func (p *Provider) processorLoop(parent context.Context) {
	for {
		select {
		case <-parent.Done():
			return
		case <-p.closedCh:
			return
		case <-p.triggers:
			p.processOneTurn(parent)
		}
	}
}

func (p *Provider) processOneTurn(ctx context.Context) {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return
	}
	pcm := p.buffer
	p.buffer = nil
	p.lastVoiceAt = time.Time{}
	p.mu.Unlock()

	duration := float64(PCMDurationMs(pcm)) / 1000.0
	emit := func(m *Message) {
		select {
		case p.messages <- m:
		case <-p.closedCh:
		}
	}

	sttResult, err := p.stt.Route(ctx, pcm, duration, stt.TranscribeOpts{Language: p.locale})
	if err != nil {
		p.emitError("stt_failed", err.Error())
		return
	}
	if sttResult == nil || strings.TrimSpace(sttResult.Text) == "" {
		// Silent or empty turn - drop quietly.
		return
	}
	emit(&Message{InputTranscript: sttResult.Text, InputTranscriptDone: true})

	if err := p.runTurn(ctx, sttResult.Text, true); err != nil {
		p.emitError("turn_failed", err.Error())
	}
}

func (p *Provider) runTurn(ctx context.Context, userText string, skipInputTranscript bool) error {
	if !skipInputTranscript {
		select {
		case p.messages <- &Message{InputTranscript: userText, InputTranscriptDone: true}:
		case <-p.closedCh:
			return nil
		}
	}

	locale, voice, systemPrompt := p.currentInstructionSnapshot()
	historyBlurb := p.renderHistorySnapshot()
	agentInput := flows.AgentInput{
		Utterance:         userText,
		Locale:            locale,
		Selection:         "",
		LastTranscription: historyBlurb,
		SystemPrompt:      systemPrompt,
	}
	out, err := p.agent.Run(ctx, agentInput)
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}
	responseText := strings.TrimSpace(out.Text)
	if responseText == "" {
		return nil
	}
	p.appendHistory(userText, responseText)

	select {
	case p.messages <- &Message{OutputTranscript: responseText, OutputTranscriptDone: true}:
	case <-p.closedCh:
		return nil
	}

	if p.tts == nil {
		return nil
	}
	ttsResult, err := p.tts.Synthesize(ctx, responseText, tts.SynthesizeOpts{
		Locale: locale,
		Voice:  voice,
		Speed:  p.cfg.TTSSpeed,
		Format: p.cfg.TTSFormat,
	})
	if err != nil {
		return fmt.Errorf("tts: %w", err)
	}
	if ttsResult != nil && len(ttsResult.Audio) > 0 {
		for _, frag := range ChunkAudio(ttsResult.Audio, 8192) {
			select {
			case p.messages <- &Message{Audio: frag}:
			case <-p.closedCh:
				return nil
			}
		}
	}
	return nil
}

func (p *Provider) currentInstructionSnapshot() (locale, voice, systemPrompt string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.locale, p.voice, renderSystemPrompt(p.systemPrompt, p.refinement)
}

func (p *Provider) appendHistory(user, assistant string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.history = append(p.history, conversationTurn{User: user, Assistant: assistant})
	if len(p.history) > p.cfg.HistoryTurns {
		p.history = p.history[len(p.history)-p.cfg.HistoryTurns:]
	}
}

func (p *Provider) renderHistorySnapshot() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return renderHistory(p.history)
}

func (p *Provider) emitError(code, message string) {
	slog.Warn("cascaded: emit error", "code", code, "err", message)
	select {
	case p.messages <- &Message{OutputTranscript: "[" + code + "] " + message, OutputTranscriptDone: true}:
	case <-p.closedCh:
	}
}

// -- agent flow adapter -----------------------------------------------

type agentFlowAdapter struct {
	Flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
}

// NewAgentFlowAdapter wraps a Genkit agent flow so it satisfies Agent.
// Returned as a separate named type to keep production wiring readable.
func NewAgentFlowAdapter(flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]) Agent {
	return &agentFlowAdapter{Flow: flow}
}

func (a *agentFlowAdapter) Run(ctx context.Context, input flows.AgentInput) (flows.AgentOutput, error) {
	if a.Flow == nil {
		return flows.AgentOutput{}, errors.New("cascaded: agent flow is nil")
	}
	return a.Flow.Run(ctx, input)
}
