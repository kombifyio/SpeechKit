package voiceeval

import (
	"errors"
	"testing"
)

func TestEvaluateTranscriptAcceptsStepRules(t *testing.T) {
	err := EvaluateTranscript([]Turn{
		{
			Speaker: "agent",
			StepID:  "frame",
			Text:    "Let us frame the goal, constraints, and a useful outcome.",
			Rule: Rule{
				Contains:    []string{"goal", "constraints"},
				NotContains: []string{"final answer"},
			},
		},
		{
			Speaker: "agent",
			StepID:  "converge",
			Text:    "Recommendation: pick option A and schedule the next review.",
			Rule:    Rule{Contains: []string{"recommendation", "next review"}},
		},
	})
	if err != nil {
		t.Fatalf("EvaluateTranscript() error = %v", err)
	}
}

func TestEvaluateTranscriptRejectsMissingInstructionSignal(t *testing.T) {
	err := EvaluateTranscript([]Turn{
		{
			Speaker: "agent",
			StepID:  "diverge",
			Text:    "Here is one idea.",
			Rule:    Rule{Contains: []string{"blind spot"}},
		},
	})
	if !errors.Is(err, ErrMissingRequiredText) {
		t.Fatalf("EvaluateTranscript() error = %v, want %v", err, ErrMissingRequiredText)
	}
}

func TestEvaluateTranscriptRejectsForbiddenText(t *testing.T) {
	err := EvaluateTranscript([]Turn{
		{
			Speaker: "agent",
			StepID:  "support",
			Text:    "I cannot help. This is a diagnosis.",
			Rule:    Rule{NotContains: []string{"diagnosis"}},
		},
	})
	if !errors.Is(err, ErrForbiddenText) {
		t.Fatalf("EvaluateTranscript() error = %v, want %v", err, ErrForbiddenText)
	}
}
