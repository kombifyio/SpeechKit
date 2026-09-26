package live

import (
	"context"
	"strings"
	"testing"
)

func TestSessionAppliesInitialWorkflowStep(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Base role prompt.",
		Workflow: &WorkflowConfig{
			SequenceID: "meeting",
			Steps: []WorkflowStep{
				{ID: "open", Instruction: "Open the dialog and clarify the goal.", MaxTurns: 1},
				{ID: "close", Instruction: "Close with the agreed next step.", MaxTurns: 1},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	cfg := providerConfigSnapshot(provider)
	if !strings.Contains(cfg.FrameworkPrompt, "Base role prompt.") {
		t.Fatalf("FrameworkPrompt = %q, want base prompt", cfg.FrameworkPrompt)
	}
	if !strings.Contains(cfg.FrameworkPrompt, "[Current step: open]") {
		t.Fatalf("FrameworkPrompt = %q, want initial step marker", cfg.FrameworkPrompt)
	}
	if !strings.Contains(cfg.FrameworkPrompt, "Open the dialog") {
		t.Fatalf("FrameworkPrompt = %q, want initial step instruction", cfg.FrameworkPrompt)
	}
}

func TestSessionAutoAdvancesWorkflowStepAfterMaxTurns(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Moderate the session.",
		Workflow: &WorkflowConfig{
			SequenceID: "meeting",
			Steps: []WorkflowStep{
				{ID: "open", Instruction: "Open the dialog.", MaxTurns: 1},
				{ID: "decide", Instruction: "Drive toward a concrete decision.", MaxTurns: 1},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	provider.messages <- &LiveMessage{InputTranscript: "We should start.", InputTranscriptDone: true}

	update := waitForSentText(t, provider)
	if !strings.Contains(update, "Host instruction update") {
		t.Fatalf("instruction update = %q, want host update marker", update)
	}
	if !strings.Contains(update, "Step: decide") {
		t.Fatalf("instruction update = %q, want next step", update)
	}
	if !strings.Contains(update, "Drive toward a concrete decision.") {
		t.Fatalf("instruction update = %q, want next step instruction", update)
	}
}

func TestSessionAdvanceWorkflowStepSendsInstructionUpdate(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Moderate the session.",
		Workflow: &WorkflowConfig{
			SequenceID: "meeting",
			Steps: []WorkflowStep{
				{ID: "open", Instruction: "Open the dialog.", MaxTurns: 3},
				{ID: "wrap", Instruction: "Wrap up with the action items.", MaxTurns: 1},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	if err := session.AdvanceWorkflowStep(ctx, "test"); err != nil {
		t.Fatalf("AdvanceWorkflowStep() error = %v", err)
	}

	update := waitForSentText(t, provider)
	if !strings.Contains(update, "Step: wrap") {
		t.Fatalf("instruction update = %q, want wrap step", update)
	}
	if !strings.Contains(update, "Wrap up with the action items.") {
		t.Fatalf("instruction update = %q, want wrap instruction", update)
	}
}

func TestSessionEvaluatesWorkflowDialogSimulation(t *testing.T) {
	provider := newSessionTestProvider()
	events := make(chan dialogueEvent, 16)
	session := NewSession(provider, Callbacks{
		OnInputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "user", text: text}
			}
		},
		OnOutputTranscript: func(text string, done bool) {
			if done {
				events <- dialogueEvent{side: "agent", text: text}
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := session.Start(ctx, LiveConfig{
		Model:           "gemini-live-test",
		FrameworkPrompt: "Moderate an interactive product strategy meeting.",
		Workflow: &WorkflowConfig{
			SequenceID: "strategy-meeting",
			Steps: []WorkflowStep{
				{ID: "frame", Instruction: "Clarify the goal and constraints.", MaxTurns: 3},
				{ID: "diverge", Instruction: "Explore alternatives and blind spots.", MaxTurns: 3},
				{ID: "converge", Instruction: "Converge on the next step.", MaxTurns: 3},
			},
		},
	}, IdleConfig{}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Stop()

	dialog := []struct {
		stepID string
		user   string
		agent  string
		rule   dialogueRule
	}{
		{
			stepID: "frame",
			user:   "We need to plan the launch meeting.",
			agent:  "Goal first: align the launch decision, capture constraints, and define a useful outcome.",
			rule:   dialogueRule{Contains: []string{"goal", "constraints", "outcome"}, NotContains: []string{"final recommendation"}},
		},
		{
			stepID: "diverge",
			user:   "The main constraint is only two engineers.",
			agent:  "Alternative paths: reduce scope or stagger rollout. One blind spot is support load after release.",
			rule:   dialogueRule{Contains: []string{"alternative", "blind spot"}, NotContains: []string{"diagnosis"}},
		},
		{
			stepID: "converge",
			user:   "Let us pick the staged rollout.",
			agent:  "Recommendation: choose staged rollout and set the next step as a 30-minute owner review.",
			rule:   dialogueRule{Contains: []string{"recommendation", "next step"}, NotContains: []string{"another hour"}},
		},
	}

	var transcript []dialogueTurn
	for i, turn := range dialog {
		if err := session.SendText(turn.user); err != nil {
			t.Fatalf("send text %q: %v", turn.user, err)
		}
		provider.messages <- &LiveMessage{InputTranscript: turn.user, InputTranscriptDone: true}
		waitForDialogueEvent(t, events, "user", turn.user)

		provider.messages <- &LiveMessage{OutputTranscript: turn.agent, OutputTranscriptDone: true}
		waitForDialogueEvent(t, events, "agent", turn.agent)
		transcript = append(transcript, dialogueTurn{
			Speaker: "agent",
			StepID:  turn.stepID,
			Text:    turn.agent,
			Rule:    turn.rule,
		})

		if i < len(dialog)-1 {
			if err := session.AdvanceWorkflowStep(ctx, "simulation"); err != nil {
				t.Fatalf("advance workflow after %s: %v", turn.stepID, err)
			}
		}
	}

	if err := evaluateDialogueTranscript(transcript); err != nil {
		t.Fatalf("dialog simulation failed instruction checks: %v", err)
	}
}
