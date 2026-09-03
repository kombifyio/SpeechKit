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
package assist

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

var (
	ErrMissingHandler        = errors.New("speechkit assist: generator or tool executor is required")
	ErrCleanModeNeedsUtility = errors.New("speechkit assist: clean mode requires a matched deterministic utility")
	// ErrMissingExecutor is returned when a session has an active skill
	// context but the service was built without a ToolExecutor.
	ErrMissingExecutor = errors.New("speechkit assist: no tool executor configured")
)

type Generator interface {
	GenerateAssist(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error)
}

type GenerateFunc func(context.Context, speechkit.AssistRequest) (speechkit.AssistResult, error)

func (f GenerateFunc) GenerateAssist(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	return f(ctx, req)
}

type ToolCall struct {
	Intent    string
	Payload   string
	Locale    string
	Selection string
	Context   string
	// Target is the host destination for insertion or execution, carried
	// unchanged from the recording that triggered the call. Pass a value
	// implementing [speechkit.OutputTarget]; untyped values are accepted until
	// the field becomes OutputTarget in v0.69.0.
	Target any
}

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

type ToolMatcher interface {
	MatchTool(context.Context, speechkit.AssistRequest) (ToolCall, bool, error)
}

type ToolMatcherFunc func(context.Context, speechkit.AssistRequest) (ToolCall, bool, error)

func (f ToolMatcherFunc) MatchTool(ctx context.Context, req speechkit.AssistRequest) (ToolCall, bool, error) {
	return f(ctx, req)
}

type ToolExecutor interface {
	ExecuteTool(context.Context, ToolCall) (ToolResult, error)
}

type ToolExecutorFunc func(context.Context, ToolCall) (ToolResult, error)

func (f ToolExecutorFunc) ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	return f(ctx, call)
}

type SkillContext struct {
	Intent    string
	State     map[string]string
	ExpiresAt time.Time
}

type SkillContextStore interface {
	Get(key string) (SkillContext, bool)
	Set(key string, intent string, state map[string]string)
	Clear(key string)
}

type TTSRouter interface {
	Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error)
}

type Options struct {
	Behavior      speechkit.ModeBehavior
	Generator     Generator
	Matcher       ToolMatcher
	Executor      ToolExecutor
	SkillContexts SkillContextStore
	TTSRouter     TTSRouter
	TTSEnabled    bool
}

type Service struct {
	behavior      speechkit.ModeBehavior
	generator     Generator
	matcher       ToolMatcher
	executor      ToolExecutor
	skillContexts SkillContextStore
	ttsRouter     TTSRouter
	ttsEnabled    bool
}

var _ speechkit.AssistService = (*Service)(nil)

func NewService(opts Options) (*Service, error) {
	if opts.Behavior == "" {
		opts.Behavior = speechkit.ModeBehaviorIntelligence
	}
	if opts.Generator == nil && opts.Executor == nil {
		return nil, ErrMissingHandler
	}
	return &Service{
		behavior:      opts.Behavior,
		generator:     opts.Generator,
		matcher:       opts.Matcher,
		executor:      opts.Executor,
		skillContexts: opts.SkillContexts,
		ttsRouter:     opts.TTSRouter,
		ttsEnabled:    opts.TTSEnabled,
	}, nil
}

func (s *Service) Process(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	if s == nil {
		return speechkit.AssistResult{}, ErrMissingHandler
	}
	if s.skillContexts != nil && req.SessionKey != "" {
		if active, ok := s.skillContexts.Get(req.SessionKey); ok {
			if s.executor == nil {
				return speechkit.AssistResult{}, fmt.Errorf("%w for intent %q", ErrMissingExecutor, active.Intent)
			}
			result, err := s.executor.ExecuteTool(ctx, ToolCall{
				Intent:    active.Intent,
				Payload:   req.Text,
				Locale:    req.Locale,
				Selection: req.Selection,
				Context:   appendFollowupState(req.Context, active.State),
			})
			if err != nil {
				return speechkit.AssistResult{}, err
			}
			return s.finalizeToolResult(ctx, req, ToolCall{Intent: active.Intent, Locale: req.Locale}, result)
		}
	}
	if s.matcher != nil {
		call, matched, err := s.matcher.MatchTool(ctx, req)
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		if matched {
			if s.executor == nil {
				return speechkit.AssistResult{}, fmt.Errorf("%w for intent %q", ErrMissingExecutor, call.Intent)
			}
			result, err := s.executor.ExecuteTool(ctx, call)
			if err != nil {
				return speechkit.AssistResult{}, err
			}
			return s.finalizeToolResult(ctx, req, call, result)
		}
	}

	if s.behavior == speechkit.ModeBehaviorClean {
		return speechkit.AssistResult{}, ErrCleanModeNeedsUtility
	}
	if s.generator == nil {
		return speechkit.AssistResult{}, ErrMissingHandler
	}
	result, err := s.generator.GenerateAssist(ctx, req)
	if err != nil {
		return speechkit.AssistResult{}, err
	}
	return s.synthesize(ctx, result)
}

func (s *Service) finalizeToolResult(ctx context.Context, req speechkit.AssistRequest, call ToolCall, result ToolResult) (speechkit.AssistResult, error) {
	// A silent tool result means the matched skill recognized the intent but
	// declined this specific payload (e.g. Math on non-math text). Fall through
	// to the generator when one is configured, matching the internal Assist
	// pipeline's silent→LLM behavior. Clean mode never uses the LLM, and a host
	// without a generator keeps the silent result as-is.
	if result.Surface == speechkit.AssistSurfaceSilent && s.generator != nil && s.behavior != speechkit.ModeBehaviorClean {
		if s.skillContexts != nil && req.SessionKey != "" {
			s.skillContexts.Clear(req.SessionKey)
		}
		generated, err := s.generator.GenerateAssist(ctx, req)
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		return s.synthesize(ctx, generated)
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
	audio, err := s.ttsRouter.Synthesize(ctx, text, tts.SynthesizeOpts{Locale: result.Locale})
	if err != nil {
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
	return speechkit.AssistResult{
		Text:       result.Text,
		SpeakText:  result.SpeakText,
		Action:     result.Action,
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
