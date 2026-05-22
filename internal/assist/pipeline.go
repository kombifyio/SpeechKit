// Package assist implements the Assist Mode pipeline:
// STT transcript → Codeword check → LLM → TTS → Result with both text and audio.
package assist

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/tts"
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
	router        *Router
	assistFlow    *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	executor      ToolExecutor
	ttsRouter     *tts.Router
	ttsEnabled    bool
	skillContexts SkillContextStore // v0.38.0 multi-turn store; nil = disabled
}

type PipelineOption func(*Pipeline)

func WithRouter(router *Router) PipelineOption {
	return func(p *Pipeline) {
		if router != nil {
			p.router = router
		}
	}
}

// WithSkillContextStore enables v0.38.0 multi-turn skill follow-ups.
// When the host wires a store, callers must also pass
// ProcessOpts.SessionKey on each Process call so the pipeline knows
// which conversation to attach state to. Nil disables follow-ups —
// every utterance is treated as fresh.
func WithSkillContextStore(store SkillContextStore) PipelineOption {
	return func(p *Pipeline) {
		p.skillContexts = store
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

// HasDirectReplyModel reports whether this pipeline can answer non-utility Assist requests.
func (p *Pipeline) HasDirectReplyModel() bool {
	return p != nil && p.assistFlow != nil
}

// CanHandleWithoutDirectReplyModel reports whether a transcript maps to a local
// utility path that does not require an Assist LLM.
func (p *Pipeline) CanHandleWithoutDirectReplyModel(transcript string, opts ProcessOpts) bool {
	if p == nil || p.executor == nil {
		return false
	}
	router := p.router
	if router == nil {
		router = NewRouter()
	}
	decision := router.Decide(transcript, opts)
	return decision.Route == RouteToolIntent && !decision.Utility.RequiresModel
}

// Process takes a transcript and produces a Result with text and optional audio.
func (p *Pipeline) Process(ctx context.Context, transcript string, opts ProcessOpts) (*Result, error) {
	if transcript == "" {
		return nil, fmt.Errorf("assist: empty transcript")
	}

	// v0.38.0 multi-turn: if the caller's session has an active
	// follow-up context, replay the same Intent rather than running
	// keyword routing. The skill receives the full new transcript as
	// the payload + the prior FollowupState via ToolCall.Context.
	if p.skillContexts != nil && opts.SessionKey != "" {
		if active, ok := p.skillContexts.Get(opts.SessionKey); ok {
			decision := Decision{
				Route:   RouteToolIntent,
				Intent:  active.Intent,
				Payload: transcript,
				Locale:  opts.Locale,
			}
			decision.Utility = p.router.UtilityFor(active.Intent)
			return p.handleTool(ctx, transcript, decision, opts.withFollowupState(active.State))
		}
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

	// SessionKey identifies the user conversation for v0.38.0 multi-
	// turn follow-ups. Hosts derive this from the authenticated user
	// (e.g. Identity.UserID) or a per-wake-word session id. Empty
	// disables follow-ups for this call.
	SessionKey string
}

// withFollowupState returns a copy of opts with the FollowupState
// serialised into the Context field so the skill can read it without
// the pipeline having to extend ToolCall.
func (o ProcessOpts) withFollowupState(state map[string]string) ProcessOpts {
	if len(state) == 0 {
		return o
	}
	var b strings.Builder
	for k, v := range state {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
	}
	if o.Context != "" {
		o.Context = o.Context + "\n--\n" + b.String()
	} else {
		o.Context = b.String()
	}
	return o
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

	// Voice-Companion skills emit Action="silent" + empty Text to signal
	// "I matched the intent but cannot answer this specific payload —
	// please defer to the LLM" (e.g. MathSkill on "what is the meaning
	// of life", HomeAssistantSkill when no URL+Token configured,
	// WikipediaSkill on disambiguation pages). When that happens AND an
	// Assist LLM flow is available, fall through to handleLLM so the
	// user gets a useful response instead of silence. Original Action
	// "silent" semantics (host-side QuickNote with no audio reply) are
	// preserved when the skill returned non-empty Text alongside.
	if toolResult.Action == "silent" && strings.TrimSpace(toolResult.Text) == "" && p.assistFlow != nil {
		return p.handleLLM(ctx, transcript, opts)
	}

	// v0.38.0 multi-turn: persist or clear the follow-up state. When
	// the skill reports FollowupNeeded=true we stash the Intent +
	// state so the next user transcript routes back to the same
	// skill. Any other outcome clears the prior context so an
	// orphaned follow-up does not bleed into the next conversation.
	if p.skillContexts != nil && opts.SessionKey != "" {
		if toolResult.FollowupNeeded {
			p.skillContexts.Set(opts.SessionKey, decision.Intent, toolResult.FollowupState)
		} else {
			p.skillContexts.Clear(opts.SessionKey)
		}
	}

	result := &Result{
		Text:      toolResult.Text,
		SpeakText: firstNonEmpty(toolResult.SpeakText, toolResult.Text),
		Action:    firstNonEmpty(toolResult.Action, "execute"),
		Locale:    firstNonEmpty(toolResult.Locale, decision.Locale, opts.Locale),
		Shortcut:  string(decision.Intent),
		Surface:   firstNonEmptySurface(toolResult.Surface, decision.Utility.DefaultSurface, ResultSurfaceActionAck),
		Kind:      firstNonEmptyKind(toolResult.Kind, decision.Utility.DefaultKind, ResultKindUtilityAction),
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
