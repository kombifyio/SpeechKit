// Package voiceeval contains deterministic dialogue checks for Voice Agent
// workflow tests. It is intentionally small and local: production moderation
// still belongs to the realtime provider, while tests use these rules to prove
// that scripted turns stay aligned with the active instructions.
package voiceeval

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrMissingRequiredText = errors.New("voiceeval: missing required text")
	ErrForbiddenText       = errors.New("voiceeval: forbidden text present")
)

type Rule struct {
	Contains    []string
	NotContains []string
}

type Turn struct {
	Speaker string
	StepID  string
	Text    string
	Rule    Rule
}

func EvaluateTranscript(turns []Turn) error {
	for i, turn := range turns {
		if err := EvaluateTurn(turn); err != nil {
			return fmt.Errorf("turn %d speaker=%q step=%q: %w", i, turn.Speaker, turn.StepID, err)
		}
	}
	return nil
}

func EvaluateTurn(turn Turn) error {
	text := strings.ToLower(turn.Text)
	for _, required := range turn.Rule.Contains {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		if !strings.Contains(text, strings.ToLower(required)) {
			return fmt.Errorf("%w: %q", ErrMissingRequiredText, required)
		}
	}
	for _, forbidden := range turn.Rule.NotContains {
		forbidden = strings.TrimSpace(forbidden)
		if forbidden == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(forbidden)) {
			return fmt.Errorf("%w: %q", ErrForbiddenText, forbidden)
		}
	}
	return nil
}
