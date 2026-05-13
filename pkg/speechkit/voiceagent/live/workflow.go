package live

import (
	"context"
	"log/slog"
	"strings"
)

// workflowState is the private runtime helper for the WorkflowConfig
// defined in types.go.
type workflowState struct {
	cfg        WorkflowConfig
	basePrompt string
	stepIndex  int
	stepTurns  int
	totalTurns int
	completed  bool
}

func prepareWorkflowStart(cfg LiveConfig) (LiveConfig, *workflowState) {
	if cfg.Workflow == nil || len(cfg.Workflow.Steps) == 0 {
		return cfg, nil
	}
	wf := cloneWorkflowConfig(*cfg.Workflow)
	stepIndex := wf.InitialStep
	if stepIndex < 0 || stepIndex >= len(wf.Steps) {
		stepIndex = 0
	}
	basePrompt := strings.TrimSpace(wf.BasePrompt)
	if basePrompt == "" {
		basePrompt = firstWorkflowPrompt(cfg.FrameworkPrompt, cfg.Instruction, cfg.SystemPrompt)
	}
	next, ok := applyWorkflowStepToConfig(cfg, wf, basePrompt, stepIndex)
	if !ok {
		return cfg, nil
	}
	return next, &workflowState{
		cfg:        wf,
		basePrompt: basePrompt,
		stepIndex:  stepIndex,
	}
}

func (s *Session) recordWorkflowUserTurn(ctx context.Context) {
	if !s.workflowAdvanceDue() {
		return
	}
	if err := s.AdvanceWorkflowStep(ctx, "max_turns"); err != nil {
		slog.Warn("voiceagent: workflow advance failed", "err", err)
		if s.callbacks.OnError != nil {
			s.callbacks.OnError(err)
		}
	}
}

func (s *Session) workflowAdvanceDue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.workflow == nil || s.workflow.completed || len(s.workflow.cfg.Steps) == 0 {
		return false
	}
	if s.workflow.stepIndex < 0 || s.workflow.stepIndex >= len(s.workflow.cfg.Steps) {
		s.workflow.completed = true
		return false
	}
	s.workflow.stepTurns++
	s.workflow.totalTurns++
	step := s.workflow.cfg.Steps[s.workflow.stepIndex]
	if step.MaxTurns > 0 && s.workflow.stepTurns >= step.MaxTurns {
		return true
	}
	if s.workflow.cfg.MaxTurns > 0 && s.workflow.totalTurns >= s.workflow.cfg.MaxTurns {
		return true
	}
	return false
}

func (s *Session) prepareWorkflowAdvance(reason string) (LiveConfig, bool) {
	_ = strings.TrimSpace(reason)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.workflow == nil || s.workflow.completed || len(s.workflow.cfg.Steps) == 0 {
		return LiveConfig{}, false
	}
	nextIndex := s.workflow.stepIndex + 1
	if nextIndex >= len(s.workflow.cfg.Steps) {
		s.workflow.completed = true
		return LiveConfig{}, false
	}
	next, ok := applyWorkflowStepToConfig(s.lastCfg, s.workflow.cfg, s.workflow.basePrompt, nextIndex)
	if !ok {
		s.workflow.completed = true
		return LiveConfig{}, false
	}
	s.workflow.stepIndex = nextIndex
	s.workflow.stepTurns = 0
	s.lastCfg = next
	return next, true
}

// RenderHostInstructionUpdate turns a workflow step change into a provider
// text update. Providers that implement LiveInstructionUpdater receive the
// structured LiveConfig instead.
func RenderHostInstructionUpdate(cfg LiveConfig) string {
	if cfg.Workflow == nil || len(cfg.Workflow.Steps) == 0 {
		return ""
	}
	stepIndex := cfg.Workflow.InitialStep
	if stepIndex < 0 || stepIndex >= len(cfg.Workflow.Steps) {
		return ""
	}
	step := cfg.Workflow.Steps[stepIndex]
	if strings.TrimSpace(step.ID) == "" && strings.TrimSpace(step.Instruction) == "" && strings.TrimSpace(cfg.FrameworkPrompt) == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("Host instruction update: the active Voice Agent workflow step has changed.")
	if strings.TrimSpace(cfg.Workflow.SequenceID) != "" || strings.TrimSpace(step.ID) != "" {
		b.WriteString("\nSequence: ")
		b.WriteString(strings.TrimSpace(cfg.Workflow.SequenceID))
		b.WriteString("\nStep: ")
		b.WriteString(strings.TrimSpace(step.ID))
	}
	if strings.TrimSpace(step.Instruction) != "" {
		b.WriteString("\nStep instruction:\n")
		b.WriteString(strings.TrimSpace(step.Instruction))
	}
	if strings.TrimSpace(step.ExitCriteria) != "" {
		b.WriteString("\nExit criteria:\n")
		b.WriteString(strings.TrimSpace(step.ExitCriteria))
	}
	if strings.TrimSpace(cfg.FrameworkPrompt) != "" {
		b.WriteString("\nComposed behavior prompt:\n")
		b.WriteString(strings.TrimSpace(cfg.FrameworkPrompt))
	}
	return b.String()
}

func applyWorkflowStepToConfig(cfg LiveConfig, wf WorkflowConfig, basePrompt string, stepIndex int) (LiveConfig, bool) {
	if stepIndex < 0 || stepIndex >= len(wf.Steps) {
		return cfg, false
	}
	wf = cloneWorkflowConfig(wf)
	wf.InitialStep = stepIndex
	wf.BasePrompt = basePrompt
	step := wf.Steps[stepIndex]
	cfg.FrameworkPrompt = composeWorkflowPrompt(basePrompt, step)
	cfg.Instruction = cfg.FrameworkPrompt
	cfg.SystemPrompt = cfg.FrameworkPrompt
	cfg.Workflow = &wf
	return cfg, true
}

func composeWorkflowPrompt(basePrompt string, step WorkflowStep) string {
	basePrompt = strings.TrimSpace(basePrompt)
	stepID := strings.TrimSpace(step.ID)
	instruction := strings.TrimSpace(step.Instruction)
	exitCriteria := strings.TrimSpace(step.ExitCriteria)
	if instruction == "" && exitCriteria == "" {
		return basePrompt
	}

	var b strings.Builder
	if basePrompt != "" {
		b.WriteString(basePrompt)
		b.WriteString("\n\n")
	}
	if stepID != "" {
		b.WriteString("[Current step: ")
		b.WriteString(stepID)
		b.WriteString("]\n")
	}
	if instruction != "" {
		b.WriteString(instruction)
	}
	if exitCriteria != "" {
		if instruction != "" {
			b.WriteString("\n")
		}
		b.WriteString("Exit criteria: ")
		b.WriteString(exitCriteria)
	}
	return strings.TrimSpace(b.String())
}

func firstWorkflowPrompt(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneWorkflowConfig(wf WorkflowConfig) WorkflowConfig {
	wf.Steps = append([]WorkflowStep(nil), wf.Steps...)
	for i := range wf.Steps {
		wf.Steps[i].RequireTools = append([]string(nil), wf.Steps[i].RequireTools...)
	}
	return wf
}
