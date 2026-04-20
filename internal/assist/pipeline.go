// Package assist implements the Assist Mode pipeline:
// STT transcript â†’ Codeword check â†’ LLM â†’ TTS â†’ Result with both text and audio.
package assist

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/firebase/genkit/go/core"
)

// Result is the framework output for Assist Mode.
// Always contains Text. Contains Audio when TTS is enabled.
type Result struct {
	Text      string // Full response text (always present)
	SpeakText string // TTS-optimized text
	Audio     []byte // TTS audio bytes (present when TTS enabled)
	Format    string // Audio format ("mp3", "wav", etc.)
	Action    string // "respond", "execute", "silent", "shortcut"
	Locale    string // Response language
	Shortcut  string // Matched shortcut intent, if any
	Surface   ResultSurface
	Kind      ResultKind
}

// Pipeline orchestrates the Assist Mode flow.
type Pipeline struct {
	router     *Router
	assistFlow *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	executor   ToolExecutor
	ttsRouter  *tts.Router
	ttsEnabled bool
}

type PipelineOption func(*Pipeline)

func WithRouter(router *Router) PipelineOption {
	return func(p *Pipeline) {
		if router != nil {
			p.router = router
		}
	}
}

// NewPipeline creates an Assist Pipeline.
func NewPipeline(assistFlow *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}], executor ToolExecutor, ttsRouter *tts.Router, ttsEnabled bool, opts ...PipelineOption) *Pipeline {
	pipeline := &Pipeline{
		router:     NewRouter(),
		assistFlow: assistFlow,
		executor:   executor,
		ttsRouter:  ttsRouter,
		ttsEnabled: ttsEnabled,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(pipeline)
		}
	}
	return pipeline
}

// Process takes a transcript and produces a Result with text and optional audio.
func (p *Pipeline) Process(ctx context.Context, transcript string, opts ProcessOpts) (*Result, error) {
	if transcript == "" {
		return nil, fmt.Errorf("assist: empty transcript")
	}

	decision := p.router.Decide(transcript, opts)
	if decision.Route == RouteToolIntent {
		return p.handleTool(ctx, transcript, decision, opts)
	}

	return p.handleLLM(ctx, transcript, opts)
}

// ProcessOpts configures a single Assist request.
type ProcessOpts struct {
	Locale    string // "de", "en", etc.
	Selection string // Currently selected text
	Context   string // Additional context
	Target    any    // Host-specific target for insertion/execution
}

func (p *Pipeline) handleTool(ctx context.Context, transcript string, decision Decision, opts ProcessOpts) (*Result, error) {
	if p.executor == nil {
		return nil, fmt.Errorf("assist: no tool executor configured for intent %q", decision.Intent)
	}

	toolResult, err := p.executor.Execute(ctx, ToolCall{
		Intent:     decision.Intent,
		Payload:    decision.Payload,
		Transcript: transcript,
		Locale:     firstNonEmpty(decision.Locale, opts.Locale),
		Selection:  opts.Selection,
		Context:    opts.Context,
		Target:     opts.Target,
	})
	if err != nil {
		return nil, fmt.Errorf("assist: execute tool intent %q: %w", decision.Intent, err)
	}

	result := &Result{
		Text:      toolResult.Text,
		SpeakText: firstNonEmpty(toolResult.SpeakText, toolResult.Text),
		Action:    firstNonEmpty(toolResult.Action, "execute"),
		Locale:    firstNonEmpty(toolResult.Locale, decision.Locale, opts.Locale),
		Shortcut:  string(decision.Intent),
		Surface:   firstNonEmptySurface(toolResult.Surface, ResultSurfaceActionAck),
		Kind:      firstNonEmptyKind(toolResult.Kind, ResultKindUtilityAction),
	}

	if err := p.synthesize(ctx, result); err != nil {
		slog.Warn("assist: TTS for tool result failed", "err", err)
	}

	return result, nil
}

func (p *Pipeline) handleLLM(ctx context.Context, transcript string, opts ProcessOpts) (*Result, error) {
	if p.assistFlow == nil {
		return nil, fmt.Errorf("assist: no LLM flow configured")
	}

	output, err := p.assistFlow.Run(ctx, flows.AssistInput{
		Utterance: transcript,
		Locale:    opts.Locale,
		Selection: opts.Selection,
		Context:   opts.Context,
	})
	if err != nil {
		return nil, fmt.Errorf("assist: LLM failed: %w", err)
	}

	result := &Result{
		Text:      output.Text,
		SpeakText: output.SpeakText,
		Action:    output.Action,
		Locale:    output.Locale,
		Surface:   ResultSurfacePanel,
		Kind:      ResultKindAnswer,
	}

	// Synthesize TTS for the LLM response.
	if err := p.synthesize(ctx, result); err != nil {
		slog.Warn("assist: TTS failed", "err", err)
	}

	return result, nil
}

// synthesize adds TTS audio to the result if TTS is enabled.
func (p *Pipeline) synthesize(ctx context.Context, result *Result) error {
	if !p.ttsEnabled || p.ttsRouter == nil {
		return nil
	}

	text := result.SpeakText
	if text == "" {
		text = result.Text
	}
	if text == "" || result.Action == "silent" {
		return nil
	}

	ttsResult, err := p.ttsRouter.Synthesize(ctx, text, tts.SynthesizeOpts{
		Locale: result.Locale,
	})
	if err != nil {
		return err
	}

	result.Audio = ttsResult.Audio
	result.Format = ttsResult.Format
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptySurface(values ...ResultSurface) ResultSurface {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ResultSurfacePanel
}

func firstNonEmptyKind(values ...ResultKind) ResultKind {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ResultKindAnswer
}
