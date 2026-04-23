// Package assist provides an embeddable Assist service constructor.
package assist

import (
	"context"
	"errors"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

var (
	ErrMissingHandler        = errors.New("speechkit assist: generator or tool executor is required")
	ErrCleanModeNeedsUtility = errors.New("speechkit assist: clean mode requires a matched deterministic utility")
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
	Target    any
}

type ToolResult struct {
	Text      string
	SpeakText string
	Action    string
	Kind      string
	Surface   speechkit.AssistSurfaceDecision
	Locale    string
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

type Options struct {
	Behavior  speechkit.ModeBehavior
	Generator Generator
	Matcher   ToolMatcher
	Executor  ToolExecutor
}

type Service struct {
	behavior  speechkit.ModeBehavior
	generator Generator
	matcher   ToolMatcher
	executor  ToolExecutor
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
		behavior:  opts.Behavior,
		generator: opts.Generator,
		matcher:   opts.Matcher,
		executor:  opts.Executor,
	}, nil
}

func (s *Service) Process(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	if s == nil {
		return speechkit.AssistResult{}, ErrMissingHandler
	}
	if s.matcher != nil {
		call, matched, err := s.matcher.MatchTool(ctx, req)
		if err != nil {
			return speechkit.AssistResult{}, err
		}
		if matched {
			if s.executor == nil {
				return speechkit.AssistResult{}, fmt.Errorf("speechkit assist: no executor configured for intent %q", call.Intent)
			}
			result, err := s.executor.ExecuteTool(ctx, call)
			if err != nil {
				return speechkit.AssistResult{}, err
			}
			return assistResultFromTool(call, result), nil
		}
	}

	if s.behavior == speechkit.ModeBehaviorClean {
		return speechkit.AssistResult{}, ErrCleanModeNeedsUtility
	}
	if s.generator == nil {
		return speechkit.AssistResult{}, ErrMissingHandler
	}
	return s.generator.GenerateAssist(ctx, req)
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
	}
}
