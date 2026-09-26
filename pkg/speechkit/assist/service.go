// Package assist provides an embeddable Assist Mode service.
//
// Assist is the one-shot pipeline: speech (or text) in, a single useful
// result out (codeword, deterministic utility, or LLM generation), with
// optional TTS playback. It is the middle of the three SpeechKit modes
// (Dictation < Assist < Voice Agent) and the right surface when the user
// wants an answer back, not a transcript and not a dialogue.
//
// Construct an instance with [NewService], passing a generator (LLM) and/or a
// tool executor plus the strict-mode policy fields from the host config.
// The ready-made Voice-Companion skills live in the skills subpackage; the
// codeword catalog they match against lives in the shortcuts subpackage.
package assist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

var (
	// ErrMissingHandler is returned by NewService when neither a Generator
	// nor a ToolExecutor is configured, and by Process when a request needs
	// the Generator (no utility matched) but none was configured.
	ErrMissingHandler = errors.New("speechkit assist: generator or tool executor is required")
	// ErrCleanModeNeedsUtility is returned in ModeBehaviorClean when no
	// deterministic utility matched: clean mode never reaches an LLM.
	ErrCleanModeNeedsUtility = errors.New("speechkit assist: clean mode requires a matched deterministic utility")
	// ErrMissingExecutor is returned when a session has an active skill
	// context or the matcher recognised a utility but the service was built
	// without a ToolExecutor.
	ErrMissingExecutor = errors.New("speechkit assist: no tool executor configured")
	// ErrEmptyText is returned by Process when the request carries no text
	// (or only whitespace); there is nothing to route or generate from.
	ErrEmptyText = errors.New("speechkit assist: text is empty")
)

// Result kinds carried in [ToolResult.Kind] and [speechkit.AssistResult.Kind].
// The value classifies what the host received so it can pick a presentation:
// an answer to read or speak, a work product to insert, or the acknowledgement
// of a utility action.
const (
	// KindAnswer is a direct reply (LLM answer, time, weather, ...).
	KindAnswer = "answer"
	// KindWorkProduct is generated text meant to be inserted or kept
	// (summary, draft).
	KindWorkProduct = "work_product"
	// KindUtilityAction acknowledges a side effect the host performed
	// (copied, inserted, timer set, smart-home command sent).
	KindUtilityAction = "utility_action"
)

// Generator produces the free-form Assist answer when no deterministic
// utility handled the request; normally an LLM.
type Generator interface {
	GenerateAssist(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error)
}

// GenerateFunc adapts a plain function to the Generator contract.
type GenerateFunc func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error)

// GenerateAssist implements Generator.
func (f GenerateFunc) GenerateAssist(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	return f(ctx, req)
}

// ToolCall is one deterministic utility invocation handed to a ToolExecutor.
type ToolCall struct {
	// Intent is the utility id the matcher recognised (a shortcuts.Intent
	// value for the built-in catalog).
	Intent string
	// Payload is the text that followed the matched codeword phrase.
	Payload string
	// Transcript is the complete utterance the call was derived from. Skills
	// that need the whole command rather than the payload after the trigger
	// phrase (Home Assistant) read it; on a multi-turn follow-up it carries
	// the new turn's text.
	Transcript string
	// Locale is the BCP-47 locale of the request.
	Locale string
	// Selection is the text the user had selected when the request started.
	Selection string
	// Context is the host-composed context block (see ComposeContext); on a
	// follow-up turn the stored skill state is appended to it.
	Context string
	// Target is the host destination for insertion or execution, carried
	// unchanged from the recording that triggered the call. Pass a value
	// implementing [speechkit.OutputTarget]; untyped values are accepted until
	// the field becomes OutputTarget in v0.69.0.
	Target any
}

// ToolResult is what a ToolExecutor returns for one ToolCall. Empty
// SpeakText, Action and Surface are filled in by the Service (Text,
// "execute" and action_ack respectively).
type ToolResult struct {
	Text           string
	SpeakText      string
	Action         string
	Kind           string
	Surface        speechkit.AssistSurfaceDecision
	Locale         string
	MessageID      localization.MessageID
	ReasonCode     string
	FollowupNeeded bool
	FollowupState  *FollowupState
}

// FollowupState carries skill-private multi-turn state without making
// ToolResult non-comparable for existing SDK consumers.
type FollowupState map[string]string

// NewFollowupState copies values into a FollowupState; nil for an empty map.
func NewFollowupState(values map[string]string) *FollowupState {
	if len(values) == 0 {
		return nil
	}
	clone := make(FollowupState, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return &clone
}

// Map returns a copy of the state as a plain map; nil when empty.
func (s *FollowupState) Map() map[string]string {
	if s == nil || len(*s) == 0 {
		return nil
	}
	clone := make(map[string]string, len(*s))
	for key, value := range *s {
		clone[key] = value
	}
	return clone
}

// ToolMatcher decides whether a request is a deterministic utility and, if
// so, which ToolCall to execute.
type ToolMatcher interface {
	MatchTool(context.Context, speechkit.AssistRequest) (ToolCall, bool, error)
}

// ToolMatcherFunc adapts a plain function to the ToolMatcher contract.
type ToolMatcherFunc func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error)

// MatchTool implements ToolMatcher.
func (f ToolMatcherFunc) MatchTool(ctx context.Context, req speechkit.AssistRequest) (ToolCall, bool, error) {
	return f(ctx, req)
}

// ToolExecutor runs a matched ToolCall and produces its ToolResult.
type ToolExecutor interface {
	ExecuteTool(context.Context, ToolCall) (ToolResult, error)
}

// ToolExecutorFunc adapts a plain function to the ToolExecutor contract.
type ToolExecutorFunc func(context.Context, ToolCall) (ToolResult, error)

// ExecuteTool implements ToolExecutor.
func (f ToolExecutorFunc) ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	return f(ctx, call)
}

// TTSRouter is the synthesis surface the Service needs; *tts.Router
// satisfies it.
type TTSRouter interface {
	Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error)
}

// TTSOptions tunes every synthesis the Service performs. Hosts derive it from
// their speech configuration; the Service clones both maps on construction.
type TTSOptions struct {
	// Defaults are provider-neutral speech defaults (voice, speed, language)
	// applied to every synthesis.
	Defaults provideropts.Values
	// ProviderOptions holds per-provider overrides keyed by provider id; the
	// router applies the entry of whichever provider wins the request.
	ProviderOptions map[string]provideropts.Values
}

// Options configures a Service. Generator or Executor is required; every
// other field is optional.
type Options struct {
	Behavior      speechkit.ModeBehavior
	Generator     Generator
	Matcher       ToolMatcher
	Executor      ToolExecutor
	SkillContexts SkillContextStore
	TTSRouter     TTSRouter
	TTSEnabled    bool
	// TTS tunes synthesis (speech defaults and per-provider overrides); nil
	// leaves the router's own defaults in charge.
	TTS *TTSOptions
	// TTSBestEffort keeps the text result when synthesis fails: the failure
	// is recorded as [speechkit.OutcomeAssistTTSFailed] and logged, and the
	// result is returned without audio. Hosts that would rather show text
	// than nothing set it; the default fails the request so a broken voice
	// path stays visible.
	TTSBestEffort bool
}

// Service is the embeddable Assist Mode implementation of
// [speechkit.AssistService].
type Service struct {
	behavior      speechkit.ModeBehavior
	generator     Generator
	matcher       ToolMatcher
	executor      ToolExecutor
	skillContexts SkillContextStore
	ttsRouter     TTSRouter
	ttsEnabled    bool
	ttsBestEffort bool
	// tts is the service's private clone of Options.TTS; a pointer keeps
	// Service comparable for existing SDK consumers.
	tts *TTSOptions
}

var _ speechkit.AssistService = (*Service)(nil)

// NewService validates opts and returns a ready Service. It fails with
// ErrMissingHandler when neither Generator nor Executor is set.
func NewService(opts Options) (*Service, error) {
	if opts.Behavior == "" {
		opts.Behavior = speechkit.ModeBehaviorIntelligence
	}
	if opts.Generator == nil && opts.Executor == nil {
		return nil, ErrMissingHandler
	}
	service := &Service{
		behavior:      opts.Behavior,
		generator:     opts.Generator,
		matcher:       opts.Matcher,
		executor:      opts.Executor,
		skillContexts: opts.SkillContexts,
		ttsRouter:     opts.TTSRouter,
		ttsEnabled:    opts.TTSEnabled,
		ttsBestEffort: opts.TTSBestEffort,
	}
	if opts.TTS != nil {
		service.tts = &TTSOptions{
			Defaults:        opts.TTS.Defaults.Clone(),
			ProviderOptions: cloneProviderOptions(opts.TTS.ProviderOptions),
		}
	}
	return service, nil
}

// HasGenerator reports whether the service can answer requests that no
// deterministic utility handles. Hosts use it to explain "no Assist model"
// before recording instead of after.
func (s *Service) HasGenerator() bool {
	return s != nil && s.generator != nil
}

// Process runs one Assist request: an active multi-turn skill context wins,
// then the ToolMatcher, then the Generator (unless the behavior is clean).
// Tool errors are wrapped with the intent; the executor's error stays
// reachable with errors.Is/As.
func (s *Service) Process(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	if s == nil {
		return speechkit.AssistResult{}, ErrMissingHandler
	}
	if strings.TrimSpace(req.Text) == "" {
		return speechkit.AssistResult{}, ErrEmptyText
	}
	if s.skillContexts != nil && req.SessionKey != "" {
		if active, ok := s.skillContexts.Get(req.SessionKey); ok {
			return s.executeTool(ctx, req, ToolCall{
				Intent:     active.Intent,
				Payload:    req.Text,
				Transcript: req.Text,
				Locale:     req.Locale,
				Selection:  req.Selection,
				Context:    appendFollowupState(req.Context, active.State),
				Target:     req.Target,
			})
		}
	}
	if s.matcher != nil {
		call, matched, err := s.matcher.MatchTool(ctx, req)
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		if matched {
			return s.executeTool(ctx, req, call)
		}
	}

	if s.behavior == speechkit.ModeBehaviorClean {
		return speechkit.AssistResult{}, ErrCleanModeNeedsUtility
	}
	if s.generator == nil {
		return speechkit.AssistResult{}, ErrMissingHandler
	}
	return s.generate(ctx, req)
}

func (s *Service) executeTool(ctx context.Context, req speechkit.AssistRequest, call ToolCall) (speechkit.AssistResult, error) {
	if s.executor == nil {
		return speechkit.AssistResult{}, fmt.Errorf("%w for intent %q", ErrMissingExecutor, call.Intent)
	}
	result, err := s.executor.ExecuteTool(ctx, call)
	if err != nil {
		return speechkit.AssistResult{}, fmt.Errorf("speechkit assist: execute tool intent %q: %w", call.Intent, err)
	}
	return s.finalizeToolResult(ctx, req, call, result)
}

func (s *Service) generate(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	result, err := s.generator.GenerateAssist(ctx, req)
	if err != nil {
		return speechkit.AssistResult{}, err
	}
	return s.synthesize(ctx, result)
}

func (s *Service) finalizeToolResult(ctx context.Context, req speechkit.AssistRequest, call ToolCall, result ToolResult) (speechkit.AssistResult, error) {
	// A silent, empty tool result means the matched skill recognized the
	// intent but declined this specific payload (e.g. Math on non-math text).
	// Fall through to the generator when one is configured. Clean mode never
	// uses the LLM, a host without a generator keeps the silent result as-is,
	// and a silent result that carries text is an explicit quiet
	// acknowledgement the host chose, not a request for the model.
	if isSilentFallthrough(result) && s.generator != nil && s.behavior != speechkit.ModeBehaviorClean {
		if s.skillContexts != nil && req.SessionKey != "" {
			s.skillContexts.Clear(req.SessionKey)
		}
		return s.generate(ctx, req)
	}
	if s.skillContexts != nil && req.SessionKey != "" {
		if result.FollowupNeeded {
			s.skillContexts.Set(req.SessionKey, call.Intent, result.FollowupState.Map())
		} else {
			s.skillContexts.Clear(req.SessionKey)
		}
	}
	return s.synthesize(ctx, assistResultFromTool(call, result))
}

func isSilentFallthrough(result ToolResult) bool {
	return result.Surface == speechkit.AssistSurfaceSilent && strings.TrimSpace(result.Text) == ""
}

func (s *Service) synthesize(ctx context.Context, result speechkit.AssistResult) (speechkit.AssistResult, error) {
	if s == nil || !s.ttsEnabled || s.ttsRouter == nil {
		return result, nil
	}
	text := result.SpeakText
	if text == "" {
		text = result.Text
	}
	if text == "" || result.Surface == speechkit.AssistSurfaceSilent {
		if text == "" && result.Surface != speechkit.AssistSurfaceSilent {
			speechkit.RecordOutcome(ctx, speechkit.OutcomeAssistEmptySpeak, errors.New("assist empty speak"))
		}
		return result, nil
	}
	synthOpts := tts.SynthesizeOpts{Locale: result.Locale}
	if s.tts != nil {
		synthOpts.Options = s.tts.Defaults.Clone()
		synthOpts.ProviderOptionsByProvider = cloneProviderOptions(s.tts.ProviderOptions)
	}
	audio, err := s.ttsRouter.Synthesize(ctx, text, synthOpts)
	if err != nil {
		if s.ttsBestEffort {
			speechkit.RecordOutcome(ctx, speechkit.OutcomeAssistTTSFailed, err)
			slog.Warn("speechkit assist: TTS failed; returning the text result without audio", "err", err)
			return result, nil
		}
		return result, err
	}
	if audio != nil {
		result.Audio = speechkit.NewAudioData(audio.Audio)
		result.Format = audio.Format
	}
	return result, nil
}

func assistResultFromTool(call ToolCall, result ToolResult) speechkit.AssistResult {
	surface := result.Surface
	if surface == "" {
		surface = speechkit.AssistSurfaceActionAck
	}
	locale := result.Locale
	if locale == "" {
		locale = call.Locale
	}
	speakText := result.SpeakText
	if speakText == "" {
		speakText = result.Text
	}
	action := result.Action
	if action == "" {
		action = "execute"
	}
	return speechkit.AssistResult{
		Text:       result.Text,
		SpeakText:  speakText,
		Action:     action,
		Kind:       result.Kind,
		Surface:    surface,
		ShortcutID: call.Intent,
		Locale:     locale,
		MessageID:  result.MessageID,
		ReasonCode: result.ReasonCode,
	}
}

func appendFollowupState(base string, state map[string]string) string {
	if len(state) == 0 {
		return base
	}
	var b strings.Builder
	if strings.TrimSpace(base) != "" {
		b.WriteString(base)
		b.WriteString("\n--\n")
	}
	for key, value := range state {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteString("\n")
		}
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(value)
	}
	return b.String()
}

func cloneProviderOptions(input map[string]provideropts.Values) map[string]provideropts.Values {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]provideropts.Values, len(input))
	for provider, values := range input {
		if len(values) > 0 {
			out[provider] = values.Clone()
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
