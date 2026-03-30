package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/textactions"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type transcriptInterceptor interface {
	Intercept(ctx context.Context, transcript speechkit.Transcript, target any) (bool, error)
}

type transcriptPaster interface {
	Handle(ctx context.Context, result *stt.Result, target output.Target) error
}

type quickActionKind string

const (
	quickActionCopyLast   quickActionKind = "copy_last"
	quickActionInsertLast quickActionKind = "insert_last"
	quickActionSummarize  quickActionKind = "summarize"
)

type quickActionDecision struct {
	kind        quickActionKind
	instruction string
}

type quickActionInvocation struct {
	transcript       speechkit.Transcript
	target           any
	captureClipboard bool // true for voice-triggered (Intercept), false for command-triggered (Execute)
}

type quickActionCoordinator struct {
	state            *appState
	paster           transcriptPaster
	summarizer       textactions.SummaryTool
	captureSelection func(context.Context) (string, error)
}

func newQuickActionCoordinator(state *appState, paster transcriptPaster) *quickActionCoordinator {
	return &quickActionCoordinator{
		state:            state,
		paster:           paster,
		summarizer:       textactions.SummaryTool{},
		captureSelection: output.CaptureSelectedText,
	}
}

func (q *quickActionCoordinator) Intercept(ctx context.Context, transcript speechkit.Transcript, target any) (bool, error) {
	decision, handled := q.resolveDecisionFromTranscript(transcript)
	if !handled {
		return false, nil
	}
	// Publish shortcut match event for UI feedback.
	q.state.publishSpeechKitEvent(speechkit.Event{
		Type:     speechkit.EventShortcutMatched,
		Message:  fmt.Sprintf("Shortcut: %s", decision.kind),
		Text:     transcript.Text,
		Shortcut: string(decision.kind),
	})
	return true, q.executeDecision(ctx, decision, quickActionInvocation{
		transcript:       transcript,
		target:           target,
		captureClipboard: true,
	})
}

func (q *quickActionCoordinator) Execute(ctx context.Context, command speechkit.Command) error {
	decision, handled := q.resolveDecisionFromCommand(command)
	if !handled {
		return nil
	}
	return q.executeDecision(ctx, decision, quickActionInvocation{
		transcript: speechkit.Transcript{
			Text: command.Text,
		},
		target: command.Target,
	})
}

func (q *quickActionCoordinator) resolveDecisionFromTranscript(transcript speechkit.Transcript) (quickActionDecision, bool) {
	resolution := shortcuts.Resolve(transcript.Text)
	switch resolution.Intent {
	case shortcuts.IntentCopyLast:
		return quickActionDecision{kind: quickActionCopyLast}, true
	case shortcuts.IntentInsertLast:
		return quickActionDecision{kind: quickActionInsertLast}, true
	case shortcuts.IntentSummarize:
		return quickActionDecision{
			kind:        quickActionSummarize,
			instruction: resolution.Payload,
		}, true
	default:
		return quickActionDecision{}, false
	}
}

func (q *quickActionCoordinator) resolveDecisionFromCommand(command speechkit.Command) (quickActionDecision, bool) {
	switch command.Type {
	case speechkit.CommandCopyLastTranscription:
		return quickActionDecision{kind: quickActionCopyLast}, true
	case speechkit.CommandInsertLastTranscription:
		return quickActionDecision{kind: quickActionInsertLast}, true
	case speechkit.CommandSummarizeSelection:
		return quickActionDecision{
			kind:        quickActionSummarize,
			instruction: command.Text,
		}, true
	default:
		return quickActionDecision{}, false
	}
}

func (q *quickActionCoordinator) executeDecision(ctx context.Context, decision quickActionDecision, invocation quickActionInvocation) error {
	switch decision.kind {
	case quickActionCopyLast:
		return q.copyLast()
	case quickActionInsertLast:
		return q.insertLast(ctx, invocation.transcript, invocation.target)
	case quickActionSummarize:
		if invocation.captureClipboard {
			return q.summarizeSelection(ctx, invocation.transcript, invocation.target, decision.instruction)
		}
		return q.summarizeText(ctx, invocation.transcript.Text, invocation.target, decision.instruction, invocation.transcript.Language)
	default:
		return nil
	}
}

func (q *quickActionCoordinator) copyLast() error {
	last := q.lastTranscription()
	if last == "" {
		return nil
	}
	if err := output.SetClipboardText(last); err != nil {
		return fmt.Errorf("copy last: %w", err)
	}
	if q.state != nil {
		q.state.addLog("Last transcription copied", "success")
	}
	return nil
}

func (q *quickActionCoordinator) insertLast(ctx context.Context, transcript speechkit.Transcript, target any) error {
	last := q.lastTranscription()
	if last == "" {
		last = transcript.Text
	}
	if last == "" || q.paster == nil {
		return nil
	}
	return q.paster.Handle(ctx, &stt.Result{
		Text:       last,
		Language:   transcript.Language,
		Duration:   transcript.Duration,
		Provider:   transcript.Provider,
		Confidence: transcript.Confidence,
	}, outputTarget(target))
}

func (q *quickActionCoordinator) summarizeSelection(ctx context.Context, transcript speechkit.Transcript, target any, instruction string) error {
	if q.captureSelection == nil {
		q.captureSelection = output.CaptureSelectedText
	}

	selection, err := q.captureSelection(ctx)
	if err != nil {
		return fmt.Errorf("capture selection: %w", err)
	}
	return q.summarizeText(ctx, selection, target, instruction, transcript.Language)
}

func (q *quickActionCoordinator) summarizeText(ctx context.Context, selection string, target any, instruction, locale string) error {
	if q.paster == nil {
		return nil
	}

	ctxInput := textactions.ResolveSummarizeContext(textactions.SummarizeContext{
		Selection:         selection,
		LastTranscription: q.lastTranscription(),
		Utterance:         instruction,
		Locale:            locale,
	})
	summary, err := q.summarizer.Run(ctx, ctxInput)
	if err != nil {
		if errors.Is(err, textactions.ErrSummarizeInputUnavailable) {
			if q.state != nil {
				q.state.addLog(msgSummarizeInputMissing, "warn")
			}
			return nil
		}
		if errors.Is(err, textactions.ErrSummarizerNotConfigured) {
			if q.state != nil {
				q.state.addLog("Summary model not configured", "warn")
			}
			return nil
		}
		return fmt.Errorf("summarize: %w", err)
	}
	if summary == "" {
		return nil
	}

	return q.paster.Handle(ctx, &stt.Result{
		Text:       summary,
		Language:   ctxInput.Locale,
		Provider:   "local-summary",
		Duration:   0,
		Confidence: 0,
	}, outputTarget(target))
}

func (q *quickActionCoordinator) lastTranscription() string {
	if q.state == nil {
		return ""
	}
	q.state.mu.Lock()
	defer q.state.mu.Unlock()
	return q.state.lastTranscriptionText
}

func outputTarget(target any) output.Target {
	if typed, ok := target.(output.Target); ok {
		return typed
	}
	return output.Target{}
}
