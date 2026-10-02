//go:build linux

package voiceagent

import (
	"context"
	"strings"
	"sync"
)

// StepResolver is the optional extension implemented by persona resolvers that
// can compose a LiveConfigFrame for a specific sequence step.
type StepResolver interface {
	ResolveStep(StartFrame, int) (LiveConfigFrame, error)
}

// SequenceTransition describes the frames and provider update produced by an
// advance operation.
type SequenceTransition struct {
	Completed         *SequenceStepFrame
	Entered           *SequenceStepFrame
	NextConfig        LiveConfigFrame
	SequenceCompleted bool
}

// SequenceRunner owns per-session workflow state for one Voice Agent
// WebSocket. It is small on purpose: persona resolution remains the source of
// truth for prompts and defaults.
type SequenceRunner struct {
	mu       sync.Mutex
	start    StartFrame
	current  LiveConfigFrame
	resolver StepResolver

	stepTurns  int
	totalTurns int
	completed  bool
}

func NewSequenceRunner(start StartFrame, initial LiveConfigFrame, resolver StepResolver) *SequenceRunner {
	return &SequenceRunner{
		start:    start,
		current:  initial,
		resolver: resolver,
	}
}

func (r *SequenceRunner) Active() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeLocked()
}

func (r *SequenceRunner) Current() LiveConfigFrame {
	if r == nil {
		return LiveConfigFrame{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

func (r *SequenceRunner) InitialEnteredFrame() *SequenceStepFrame {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.activeLocked() {
		return nil
	}
	return enteredFrame(r.current)
}

func (r *SequenceRunner) RecordUserTurn() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.activeLocked() || r.completed {
		return false
	}
	r.stepTurns++
	r.totalTurns++
	if r.current.StepMaxTurns > 0 && r.stepTurns >= r.current.StepMaxTurns {
		return true
	}
	if r.current.SequenceMaxTurns > 0 && r.totalTurns >= r.current.SequenceMaxTurns {
		return true
	}
	return false
}

func (r *SequenceRunner) Advance(ctx context.Context, frame AdvanceStepFrame) (SequenceTransition, error) {
	_ = ctx
	if r == nil {
		return SequenceTransition{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.activeLocked() || r.completed {
		return SequenceTransition{}, nil
	}

	transition := SequenceTransition{
		Completed: completedFrame(r.current, frame.Reason),
	}
	nextIndex := r.current.StepIndex + 1
	if r.current.StepCount > 0 && nextIndex >= r.current.StepCount {
		r.completed = true
		transition.SequenceCompleted = true
		return transition, nil
	}
	if r.resolver == nil {
		r.completed = true
		transition.SequenceCompleted = true
		return transition, nil
	}

	next, err := r.resolver.ResolveStep(r.start, nextIndex)
	if err != nil {
		return SequenceTransition{}, err
	}
	r.current = next
	r.stepTurns = 0
	transition.NextConfig = next
	transition.Entered = enteredFrame(next)
	return transition, nil
}

func (r *SequenceRunner) activeLocked() bool {
	return strings.TrimSpace(r.current.SequenceID) != "" && strings.TrimSpace(r.current.StepID) != ""
}

func enteredFrame(cfg LiveConfigFrame) *SequenceStepFrame {
	return &SequenceStepFrame{
		Type:       MsgSequenceStep,
		SequenceID: cfg.SequenceID,
		StepID:     cfg.StepID,
		StepIndex:  cfg.StepIndex,
		Status:     "entered",
	}
}

func completedFrame(cfg LiveConfigFrame, reason string) *SequenceStepFrame {
	return &SequenceStepFrame{
		Type:       MsgSequenceStep,
		SequenceID: cfg.SequenceID,
		StepID:     cfg.StepID,
		StepIndex:  cfg.StepIndex,
		Status:     "completed",
		Reason:     reason,
	}
}

func sequenceCompletedFrame(cfg LiveConfigFrame, reason string) SequenceStepFrame {
	return SequenceStepFrame{
		Type:       MsgSequenceStep,
		SequenceID: cfg.SequenceID,
		StepID:     cfg.StepID,
		StepIndex:  cfg.StepIndex,
		Status:     "sequence_completed",
		Reason:     reason,
	}
}
