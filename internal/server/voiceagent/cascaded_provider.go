//go:build linux

package voiceagent

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/tts"
)

// CascadedProvider implements LiveProviderAdapter as a turn-based STT â†’ LLM â†’
// TTS pipeline. It is the self-hosted-friendly alternative to real-time
// providers like Gemini Live and Moshi.
//
// Turn detection uses energy-based silence detection (RMS of the 16 kHz S16
// PCM stream). When the caller wants tighter control it can send an
// audio_end frame which triggers immediate processing regardless of the
// silence heuristic.
//
// The provider is intentionally self-contained and testable with pure
// fakes â€” CascadedSTT, CascadedAgent, and CascadedTTS are narrow interfaces
// the bootstrap wires production implementations into.
type CascadedProvider struct {
	stt   CascadedSTT
	agent CascadedAgent
	tts   CascadedTTS
	cfg   CascadedConfig

	// Per-session live config from Connect()
	locale       string
	voice        string
	systemPrompt string
	refinement   string

	// Turn state
	mu          sync.Mutex
	buffer      []byte
	lastVoiceAt time.Time
	history     []conversationTurn // rolling window

	// Processing channels
	messages  chan *LiveMessage
	triggers  chan struct{}
	closeOnce sync.Once
	closedCh  chan struct{}
}

// CascadedConfig tunes turn detection and processing. Zero values are filled
// by NewCascadedProvider with sensible defaults.
type CascadedConfig struct {
	// SilenceRMSThreshold is the RMS level (0.0â€“1.0) below which a frame is
	// considered silence. Default 0.02.
	SilenceRMSThreshold float64
	// SilenceTurnMs is the minimum silence duration before we commit the
	// current turn. Default 800 ms.
	SilenceTurnMs int
	// MinTurnMs is the minimum accumulated non-silence needed to treat a
	// buffer as a real turn. Default 300 ms â€” filters coughs and clicks.
	MinTurnMs int
	// MaxTurnMs caps a runaway turn before forcing processing. Default
	// 30 000 ms.
	MaxTurnMs int
	// HistoryTurns is how many (user, assistant) turns to keep as rolling
	// context for the agent flow. Default 5.
	HistoryTurns int
	// TTSFormat is the audio format the TTS provider should emit. Default
	// "mp3" (provider passthrough, client transcodes if needed).
	TTSFormat string
	// TTSSpeed is the TTS speech rate. Default 1.0.
	TTSSpeed float64
}

// CascadedSTT is the STT surface the provider uses. In production this is
// the same *internal/router.Router that serves /v1/dictation/transcribe.
type CascadedSTT interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// CascadedAgent is the LLM surface the provider uses. In production this is
// the Genkit agent flow defined by flows.DefineAgentFlow.
type CascadedAgent interface {
	Run(ctx context.Context, input flows.AgentInput) (flows.AgentOutput, error)
}

// CascadedTTS is the TTS surface the provider uses. In production this is
// the *internal/tts.Router that serves Assist responses.
type CascadedTTS interface {
	Synthesize(ctx context.Context, text string, opts tts.SynthesizeOpts) (*tts.Result, error)
}

// CascadedDeps bundles everything the bootstrap hands to the provider.
type CascadedDeps struct {
	STT    CascadedSTT
	Agent  CascadedAgent
	TTS    CascadedTTS
	Config CascadedConfig
}

type conversationTurn struct {
	User      string
	Assistant string
}

// NewCascadedProvider constructs a provider without starting background work.
// Connect() initializes the goroutine that performs turn processing.
func NewCascadedProvider(deps CascadedDeps) *CascadedProvider {
	cfg := deps.Config
	if cfg.SilenceRMSThreshold <= 0 {
		cfg.SilenceRMSThreshold = 0.02
	}
	if cfg.SilenceTurnMs <= 0 {
		cfg.SilenceTurnMs = 800
	}
	if cfg.MinTurnMs <= 0 {
		cfg.MinTurnMs = 300
	}
	if cfg.MaxTurnMs <= 0 {
		cfg.MaxTurnMs = 30000
	}
	if cfg.HistoryTurns <= 0 {
		cfg.HistoryTurns = 5
	}
	if cfg.TTSFormat == "" {
		cfg.TTSFormat = "mp3"
	}
	if cfg.TTSSpeed == 0 {
		cfg.TTSSpeed = 1.0
	}
	return &CascadedProvider{
		stt:      deps.STT,
		agent:    deps.Agent,
		tts:      deps.TTS,
		cfg:      cfg,
		messages: make(chan *LiveMessage, 16),
		triggers: make(chan struct{}, 4),
		closedCh: make(chan struct{}),
	}
}

// Connect validates that the required dependencies for LiveConfigFrame are
// satisfied and starts the processor loop. Unlike Gemini Live, no external
// handshake happens.
func (p *CascadedProvider) Connect(ctx context.Context, cfg LiveConfigFrame) error {
	if p.stt == nil {
		return errors.New("cascaded: STT router not configured")
	}
	if p.agent == nil {
		return errors.New("cascaded: Agent flow not configured (no LLM models available)")
	}
	if p.tts == nil {
		// TTS is optional for cascaded â€” without it we still return
		// OutputTranscript text frames so the client can render subtitles
		// or speak via its own TTS stack.
		slog.Info("cascaded: TTS not configured; sessions will be text-only")
	}

	p.mu.Lock()
	p.locale = firstNonEmptyCascaded(cfg.Locale, "en")
	p.voice = cfg.Voice
	p.systemPrompt = firstNonEmptyCascaded(cfg.SystemPrompt, "")
	p.refinement = cfg.RefinementPrompt
	p.mu.Unlock()

	go p.processorLoop(ctx)
	return nil
}

// UpdateInstructions changes future-turn host instructions without creating a
// synthetic user turn. This is the cascaded counterpart to live provider
// system-instruction updates.
func (p *CascadedProvider) UpdateInstructions(_ context.Context, cfg LiveConfigFrame) error {
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

// SendAudio appends PCM to the current turn buffer and triggers processing
// when a silence boundary is reached.
func (p *CascadedProvider) SendAudio(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	p.buffer = append(p.buffer, chunk...)
	if rms := chunkRMS(chunk); rms > p.cfg.SilenceRMSThreshold {
		p.lastVoiceAt = time.Now()
	}
	p.maybeTriggerLocked()
	return nil
}

// SendAudioStreamEnd forces the current buffer to be treated as a complete
// turn, even if silence has not yet been detected.
func (p *CascadedProvider) SendAudioStreamEnd() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.buffer) == 0 {
		return nil
	}
	p.fire()
	return nil
}

// SendText injects a text turn (skipping STT). Useful for testing and for
// clients that already have a transcript from their own STT.
func (p *CascadedProvider) SendText(text string) error {
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
func (p *CascadedProvider) Receive(ctx context.Context) (*LiveMessage, error) {
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
func (p *CascadedProvider) Close() error {
	p.closeOnce.Do(func() {
		close(p.closedCh)
	})
	return nil
}

// Name returns the provider identifier used in logs + /admin/status.
func (p *CascadedProvider) Name() string { return "cascaded" }

// â”€â”€ internals â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// maybeTriggerLocked fires a turn-end trigger when the silence threshold or
// max-duration threshold is exceeded. Caller must hold p.mu.
func (p *CascadedProvider) maybeTriggerLocked() {
	bufMs := pcmDurationMs(p.buffer)
	if bufMs < int64(p.cfg.MinTurnMs) {
		return
	}
	now := time.Now()
	silentFor := now.Sub(p.lastVoiceAt).Milliseconds()
	if silentFor >= int64(p.cfg.SilenceTurnMs) || bufMs >= int64(p.cfg.MaxTurnMs) {
		p.fire()
	}
}

func (p *CascadedProvider) fire() {
	select {
	case p.triggers <- struct{}{}:
	default:
	}
}

func (p *CascadedProvider) processorLoop(parent context.Context) {
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

func (p *CascadedProvider) processOneTurn(ctx context.Context) {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return
	}
	pcm := p.buffer
	p.buffer = nil
	p.lastVoiceAt = time.Time{}
	p.mu.Unlock()

	duration := float64(pcmDurationMs(pcm)) / 1000.0
	emit := func(m *LiveMessage) {
		select {
		case p.messages <- m:
		case <-p.closedCh:
		}
	}

	// STT.
	sttResult, err := p.stt.Route(ctx, pcm, duration, stt.TranscribeOpts{Language: p.locale})
	if err != nil {
		p.emitError("stt_failed", err.Error())
		return
	}
	if sttResult == nil || strings.TrimSpace(sttResult.Text) == "" {
		// Silent/empty turn â€” drop quietly.
		return
	}
	emit(&LiveMessage{InputTranscript: sttResult.Text, InputTranscriptDone: true})

	if err := p.runTurn(ctx, sttResult.Text, true); err != nil {
		p.emitError("turn_failed", err.Error())
	}
}

func (p *CascadedProvider) runTurn(ctx context.Context, userText string, skipInputTranscript bool) error {
	if !skipInputTranscript {
		select {
		case p.messages <- &LiveMessage{InputTranscript: userText, InputTranscriptDone: true}:
		case <-p.closedCh:
			return nil
		}
	}

	locale, voice, systemPrompt := p.currentInstructionSnapshot()
	historyBlurb := p.renderHistory()
	agentInput := flows.AgentInput{
		Utterance:         userText,
		Locale:            locale,
		Selection:         "",
		LastTranscription: historyBlurb,
		SystemPrompt:      systemPrompt,
	}
	if out, err := p.agent.Run(ctx, agentInput); err == nil {
		responseText := strings.TrimSpace(out.Text)
		if responseText == "" {
			return nil
		}
		p.appendHistory(userText, responseText)

		select {
		case p.messages <- &LiveMessage{OutputTranscript: responseText, OutputTranscriptDone: true}:
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
			// Stream TTS audio in ~40 ms chunks so the client can start
			// playback before the full synthesis lands. For mp3/wav we
			// stream as-is (no frame boundaries to respect) â€” the client
			// concatenates and hands the result to its own decoder.
			for _, frag := range chunkAudio(ttsResult.Audio, 8192) {
				select {
				case p.messages <- &LiveMessage{Audio: frag}:
				case <-p.closedCh:
					return nil
				}
			}
		}
		return nil
	} else {
		return fmt.Errorf("agent: %w", err)
	}
}

func (p *CascadedProvider) currentInstructionSnapshot() (locale string, voice string, systemPrompt string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.locale, p.voice, renderCascadedSystemPrompt(p.systemPrompt, p.refinement)
}

func (p *CascadedProvider) appendHistory(user, assistant string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.history = append(p.history, conversationTurn{User: user, Assistant: assistant})
	if len(p.history) > p.cfg.HistoryTurns {
		p.history = p.history[len(p.history)-p.cfg.HistoryTurns:]
	}
}

func (p *CascadedProvider) renderHistory() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.history) == 0 {
		return ""
	}
	var b strings.Builder
	for _, t := range p.history {
		b.WriteString("User: ")
		b.WriteString(t.User)
		b.WriteString("\nAssistant: ")
		b.WriteString(t.Assistant)
		b.WriteString("\n")
	}
	return b.String()
}

func (p *CascadedProvider) emitError(code, message string) {
	slog.Warn("cascaded: emit error", "code", code, "err", message)
	// Errors are carried as OutputTranscript so clients that render
	// transcripts still see them. Callers that want structured errors get
	// them via the WebSocket adapter's error frame channel (which runs on
	// the Receive() return value, not in-band).
	select {
	case p.messages <- &LiveMessage{OutputTranscript: "[" + code + "] " + message, OutputTranscriptDone: true}:
	case <-p.closedCh:
	}
}

// â”€â”€ helpers (pure, testable) â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€

// chunkRMS computes the RMS level (0.0â€“1.0) of an S16LE PCM buffer. Matches
// the formula in internal/audio/pcm.PCMLevel; duplicated here to avoid a
// dependency on internal/audio from the Server-Target build (that package
// carries the oto/v3 cgo linkage).
func chunkRMS(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	samples := len(pcm) / 2
	var sumSq float64
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(binary.LittleEndian.Uint16(pcm[i : i+2]))
		v := float64(s) / 32768.0
		sumSq += v * v
	}
	level := math.Sqrt(sumSq / float64(samples))
	if level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}

func pcmDurationMs(pcm []byte) int64 {
	// 16 kHz S16 mono â†’ 32 000 bytes per second.
	return int64(len(pcm)) * 1000 / 32000
}

func chunkAudio(data []byte, size int) [][]byte {
	if size <= 0 || len(data) <= size {
		return [][]byte{data}
	}
	out := make([][]byte, 0, (len(data)+size-1)/size)
	for i := 0; i < len(data); i += size {
		end := i + size
		if end > len(data) {
			end = len(data)
		}
		out = append(out, data[i:end])
	}
	return out
}

func firstNonEmptyCascaded(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func renderCascadedSystemPrompt(systemPrompt, refinement string) string {
	systemPrompt = strings.TrimSpace(systemPrompt)
	refinement = strings.TrimSpace(refinement)
	if refinement == "" {
		return systemPrompt
	}
	if systemPrompt == "" {
		return refinement
	}
	return systemPrompt + "\n\nPersonal refinement:\n" + refinement
}

// agentFlowAdapter lets the bootstrap pass a Genkit *core.Flow into the
// CascadedProvider via the CascadedAgent interface.
type agentFlowAdapter struct {
	Flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
}

// NewAgentFlowAdapter wraps a Genkit agent flow so it satisfies
// CascadedAgent. Returned as a separate named type to keep the production
// wiring readable.
func NewAgentFlowAdapter(flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]) CascadedAgent {
	return &agentFlowAdapter{Flow: flow}
}

func (a *agentFlowAdapter) Run(ctx context.Context, input flows.AgentInput) (flows.AgentOutput, error) {
	if a.Flow == nil {
		return flows.AgentOutput{}, errors.New("cascaded: agent flow is nil")
	}
	return a.Flow.Run(ctx, input)
}
