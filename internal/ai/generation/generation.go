// Package generation defines SpeechKit's provider-neutral text generation
// boundary. Provider SDK types stay behind adapters so workflows can select
// models, enforce privacy policy, and handle context limits consistently.
package generation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Purpose string

const (
	PurposeUtility           Purpose = "utility"
	PurposeAssist            Purpose = "assist"
	PurposeMeetingExtraction Purpose = "meeting_extraction"
	PurposeMeetingSynthesis  Purpose = "meeting_synthesis"
	PurposeVoiceAgentThink   Purpose = "voice_agent_think"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type Request struct {
	Purpose         Purpose
	Locale          string
	System          string
	Prompt          string
	Messages        []Message
	ModelID         string
	AffinityKey     string
	StructuredHint  string
	MaxOutputTokens int
	Temperature     float64
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type Result struct {
	Text         string
	Provider     string
	Model        string
	FinishReason string
	Usage        *Usage
	Latency      time.Duration
}

type Model struct {
	ID                       string
	Provider                 string
	Name                     string
	Purposes                 []Purpose
	ContextWindowTokens      int
	SupportsStructuredOutput bool
	Cloud                    bool
}

func (m Model) Supports(purpose Purpose) bool {
	if purpose == "" || len(m.Purposes) == 0 {
		return true
	}
	for _, supported := range m.Purposes {
		if supported == purpose {
			return true
		}
	}
	return false
}

type ModelQuery struct {
	Purpose Purpose
}

type Catalog struct {
	Models []Model
}

type Generator interface {
	Generate(context.Context, Request) (Result, error)
	Models(context.Context, ModelQuery) (Catalog, error)
}

type ErrorKind string

const (
	ErrorConfiguration  ErrorKind = "configuration"
	ErrorAuthentication ErrorKind = "authentication"
	ErrorConsent        ErrorKind = "consent"
	ErrorQuota          ErrorKind = "quota"
	ErrorContextLimit   ErrorKind = "context_limit"
	ErrorTransient      ErrorKind = "transient"
	ErrorInvalidOutput  ErrorKind = "invalid_output"
	ErrorCancelled      ErrorKind = "cancelled"
	ErrorPermanent      ErrorKind = "permanent"
)

type Error struct {
	Kind      ErrorKind
	Operation string
	Provider  string
	Model     string
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	scope := strings.TrimSpace(strings.Join([]string{e.Provider, e.Model}, "/"))
	if scope == "" {
		scope = "generation"
	}
	if e.Operation != "" {
		scope += " " + e.Operation
	}
	if e.Err == nil {
		return fmt.Sprintf("%s: %s", scope, e.Kind)
	}
	return fmt.Sprintf("%s: %s: %v", scope, e.Kind, e.Err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Kind(err error) ErrorKind {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrorCancelled
	}
	var generationErr *Error
	if errors.As(err, &generationErr) {
		return generationErr.Kind
	}
	return ErrorPermanent
}

const (
	DefaultContextWindowTokens = 8192
	DefaultPromptReserveTokens = 1024
)

func SafeInputBudget(model Model, outputTokens int) int {
	contextTokens := model.ContextWindowTokens
	if contextTokens <= 0 {
		contextTokens = DefaultContextWindowTokens
	}
	if outputTokens <= 0 {
		outputTokens = 1024
	}
	budget := contextTokens - outputTokens - DefaultPromptReserveTokens
	if budget < 512 {
		return 512
	}
	return budget
}

// EstimateTokens intentionally overestimates multilingual meeting text. Exact
// provider tokenizers are not available for every selectable model, so a
// conservative common estimate is safer than retrying oversized prompts.
func EstimateTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return (runes + 2) / 3
}

func ConservativeContextWindow(provider, model string) int {
	name := strings.ToLower(provider + "/" + model)
	switch {
	case strings.Contains(name, "qwen3.5-4b-32k"),
		strings.Contains(name, "mixtral-8x7b-32768"),
		strings.Contains(name, "llama-3.1"),
		strings.Contains(name, "llama-3.3"),
		strings.Contains(name, "gpt-4"),
		strings.Contains(name, "gpt-5"),
		strings.Contains(name, "gemini"):
		return 32768
	default:
		return DefaultContextWindowTokens
	}
}
