//go:build linux

package voiceagent

import (
	"context"
	"log/slog"
	"strings"
)

func (a *Adapter) recordUserTurn(ctx context.Context) {
	if a.flow == nil || !a.flow.RecordUserTurn() {
		return
	}
	if err := a.advanceWorkflowStep(ctx, AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "max_turns"}); err != nil {
		slog.Warn("voiceagent: max-turn workflow advance failed", "err", err)
		a.sendError(ctx, "advance_step_failed", err.Error())
	}
}

func (a *Adapter) advanceWorkflowStep(ctx context.Context, frame AdvanceStepFrame) error {
	if a.flow == nil || !a.flow.Active() {
		return nil
	}
	transition, err := a.flow.Advance(ctx, frame)
	if err != nil {
		return err
	}
	if transition.Completed != nil {
		a.sendJSON(ctx, *transition.Completed)
	}
	if transition.SequenceCompleted {
		current := a.flow.Current()
		a.sendJSON(ctx, sequenceCompletedFrame(current, frame.Reason))
		return nil
	}
	if transition.Entered != nil {
		if err := a.applyInstructionUpdate(ctx, transition.NextConfig); err != nil {
			slog.Warn("voiceagent: workflow instruction update failed", "err", err)
			a.sendError(ctx, "instruction_update_failed", err.Error())
		}
		a.sendJSON(ctx, *transition.Entered)
	}
	return nil
}

func (a *Adapter) applyInstructionUpdate(ctx context.Context, cfg LiveConfigFrame) error {
	if updater, ok := a.Provider.(LiveInstructionUpdater); ok {
		return updater.UpdateInstructions(ctx, cfg)
	}
	text := RenderHostInstructionUpdate(cfg)
	if text == "" {
		return nil
	}
	return a.Provider.SendText(text)
}

func RenderHostInstructionUpdate(cfg LiveConfigFrame) string {
	var b strings.Builder
	if strings.TrimSpace(cfg.StepID) == "" && strings.TrimSpace(cfg.SystemPrompt) == "" {
		return ""
	}
	b.WriteString("Host instruction update: the active Voice Agent workflow step has changed.")
	if cfg.SequenceID != "" || cfg.StepID != "" {
		b.WriteString("\nSequence: ")
		b.WriteString(cfg.SequenceID)
		b.WriteString("\nStep: ")
		b.WriteString(cfg.StepID)
	}
	if strings.TrimSpace(cfg.StepInstruction) != "" {
		b.WriteString("\nStep instruction:\n")
		b.WriteString(strings.TrimSpace(cfg.StepInstruction))
	}
	if strings.TrimSpace(cfg.StepExitCriteria) != "" {
		b.WriteString("\nExit criteria:\n")
		b.WriteString(strings.TrimSpace(cfg.StepExitCriteria))
	}
	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		b.WriteString("\nComposed behavior prompt:\n")
		b.WriteString(strings.TrimSpace(cfg.SystemPrompt))
	}
	return b.String()
}
