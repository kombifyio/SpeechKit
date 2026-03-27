package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	resolution := shortcuts.Resolve(transcript.Text)
	if resolution.Intent == shortcuts.IntentNone {
		return false, nil
	}

	switch resolution.Intent {
	case shortcuts.IntentCopyLast:
		return true, q.copyLast()
	case shortcuts.IntentInsertLast:
		return true, q.insertLast(ctx, transcript, target)
	case shortcuts.IntentSummarize:
		return true, q.summarizeSelection(ctx, transcript, target, resolution)
	default:
		return true, nil
	}
}

func (q *quickActionCoordinator) Execute(ctx context.Context, command speechkit.Command) error {
	switch command.Type {
	case speechkit.CommandCopyLastTranscription:
		return q.copyLast()
	case speechkit.CommandInsertLastTranscription:
		return q.insertLast(ctx, speechkit.Transcript{Text: command.Text}, command.Target)
	case speechkit.CommandSummarizeSelection:
		return q.summarizeText(ctx, command.Text, command.Target, "", "")
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

func (q *quickActionCoordinator) summarizeSelection(ctx context.Context, transcript speechkit.Transcript, target any, resolution shortcuts.Resolution) error {
	if q.captureSelection == nil {
		q.captureSelection = output.CaptureSelectedText
	}

	selection, err := q.captureSelection(ctx)
	if err != nil {
		return fmt.Errorf("capture selection: %w", err)
	}
	return q.summarizeText(ctx, selection, target, resolution.Payload, transcript.Language)
}

func (q *quickActionCoordinator) summarizeText(ctx context.Context, selection string, target any, instruction, locale string) error {
	if strings.TrimSpace(selection) == "" {
		if q.state != nil {
			q.state.addLog("No text selection available for summarize", "warn")
		}
		return nil
	}
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
