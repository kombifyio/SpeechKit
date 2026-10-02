//go:build linux

package voiceagent

import (
	"context"
	"errors"
	"testing"
)

type sequenceStepResolverFake struct {
	frames map[int]LiveConfigFrame
	err    error
}

func (r sequenceStepResolverFake) ResolveStep(_ StartFrame, stepIndex int) (LiveConfigFrame, error) {
	if r.err != nil {
		return LiveConfigFrame{}, r.err
	}
	frame, ok := r.frames[stepIndex]
	if !ok {
		return LiveConfigFrame{}, errors.New("missing step")
	}
	return frame, nil
}

func TestSequenceRunnerAdvanceResolvesNextStep(t *testing.T) {
	resolver := sequenceStepResolverFake{frames: map[int]LiveConfigFrame{
		1: {
			SequenceID:      "meeting",
			StepID:          "discussion",
			StepIndex:       1,
			StepCount:       2,
			StepInstruction: "Moderate the main discussion.",
			SystemPrompt:    "Role prompt\n\n[Current step: discussion]\nModerate the main discussion.",
		},
	}}
	runner := NewSequenceRunner(StartFrame{SequenceID: "meeting"}, LiveConfigFrame{
		SequenceID:      "meeting",
		StepID:          "opening",
		StepIndex:       0,
		StepCount:       2,
		StepInstruction: "Open the meeting.",
		SystemPrompt:    "Role prompt\n\n[Current step: opening]\nOpen the meeting.",
	}, resolver)

	transition, err := runner.Advance(context.Background(), AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "host"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if transition.Completed == nil || transition.Completed.StepID != "opening" || transition.Completed.Status != "completed" {
		t.Fatalf("completed frame = %+v", transition.Completed)
	}
	if transition.Entered == nil || transition.Entered.StepID != "discussion" || transition.Entered.StepIndex != 1 {
		t.Fatalf("entered frame = %+v", transition.Entered)
	}
	if transition.SequenceCompleted {
		t.Fatalf("SequenceCompleted = true, want false")
	}
	if runner.Current().StepID != "discussion" {
		t.Fatalf("current step = %q, want discussion", runner.Current().StepID)
	}
}

func TestSequenceRunnerAdvanceCompletesSequenceAtEnd(t *testing.T) {
	runner := NewSequenceRunner(StartFrame{SequenceID: "meeting"}, LiveConfigFrame{
		SequenceID: "meeting",
		StepID:     "close",
		StepIndex:  1,
		StepCount:  2,
	}, sequenceStepResolverFake{})

	transition, err := runner.Advance(context.Background(), AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "host"})
	if err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if transition.Completed == nil || transition.Completed.StepID != "close" {
		t.Fatalf("completed frame = %+v", transition.Completed)
	}
	if transition.Entered != nil {
		t.Fatalf("entered frame = %+v, want nil", transition.Entered)
	}
	if !transition.SequenceCompleted {
		t.Fatalf("SequenceCompleted = false, want true")
	}
}

func TestSequenceRunnerRecordUserTurnHonorsStepMaxTurns(t *testing.T) {
	runner := NewSequenceRunner(StartFrame{SequenceID: "meeting"}, LiveConfigFrame{
		SequenceID:   "meeting",
		StepID:       "discussion",
		StepIndex:    1,
		StepCount:    3,
		StepMaxTurns: 2,
	}, sequenceStepResolverFake{})

	if runner.RecordUserTurn() {
		t.Fatalf("first turn should not trigger max-turn advance")
	}
	if !runner.RecordUserTurn() {
		t.Fatalf("second turn should trigger max-turn advance")
	}
}
