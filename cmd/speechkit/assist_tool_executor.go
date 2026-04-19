package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type assistToolExecutor struct {
	actions *quickActionCoordinator
}

func newAssistToolExecutor(actions *quickActionCoordinator) *assistToolExecutor {
	return &assistToolExecutor{actions: actions}
}

func (e *assistToolExecutor) Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	if e == nil || e.actions == nil {
		return assist.ToolResult{}, fmt.Errorf("assist tool executor not configured")
	}

	switch call.Intent {
	case shortcuts.IntentCopyLast:
		if err := e.actions.copyLast(); err != nil {
			return assist.ToolResult{}, err
		}
		text := localizedAssistActionText(call.Locale, call.Intent)
		return assist.ToolResult{
			Text:      text,
			SpeakText: text,
			Action:    "execute",
			Locale:    call.Locale,
			Surface:   assist.ResultSurfaceActionAck,
			Kind:      assist.ResultKindUtilityAction,
		}, nil
	case shortcuts.IntentInsertLast:
		if err := e.actions.insertLast(ctx, speechkit.Transcript{Language: call.Locale}, call.Target); err != nil {
			return assist.ToolResult{}, err
		}
		text := localizedAssistActionText(call.Locale, call.Intent)
		return assist.ToolResult{
			Text:      text,
			SpeakText: text,
			Action:    "execute",
			Locale:    call.Locale,
			Surface:   assist.ResultSurfaceActionAck,
			Kind:      assist.ResultKindUtilityAction,
		}, nil
	case shortcuts.IntentSummarize:
		selection := strings.TrimSpace(call.Selection)
		if selection == "" {
			if e.actions.captureSelection == nil {
				e.actions.captureSelection = output.CaptureSelectedText
			}
			capturedSelection, err := e.actions.captureSelection(ctx)
			if err != nil {
				return assist.ToolResult{}, fmt.Errorf("capture selection: %w", err)
			}
			selection = strings.TrimSpace(capturedSelection)
		}

		canInsertResult := selection != "" && outputTarget(call.Target).HWND != 0
		if canInsertResult {
			summary, err := e.actions.summarizeAndPaste(ctx, selection, call.Target, call.Payload, call.Locale)
			if err != nil {
				return assist.ToolResult{}, err
			}
			if summary == "" {
				return assist.ToolResult{
					Action:  "silent",
					Locale:  call.Locale,
					Surface: assist.ResultSurfaceSilent,
					Kind:    assist.ResultKindUtilityAction,
				}, nil
			}
			text := localizedAssistSummaryInsertedText(call.Locale)
			return assist.ToolResult{
				Text:      text,
				SpeakText: text,
				Action:    "execute",
				Locale:    call.Locale,
				Surface:   assist.ResultSurfaceActionAck,
				Kind:      assist.ResultKindUtilityAction,
			}, nil
		}

		summary, resolvedLocale, err := e.actions.generateSummary(ctx, selection, call.Payload, call.Locale)
		if err != nil {
			return assist.ToolResult{}, err
		}
		if summary == "" {
			return assist.ToolResult{
				Action:  "silent",
				Locale:  call.Locale,
				Surface: assist.ResultSurfaceSilent,
				Kind:    assist.ResultKindWorkProduct,
			}, nil
		}
		return assist.ToolResult{
			Text:      summary,
			SpeakText: summary,
			Action:    "respond",
			Locale:    resolvedLocale,
			Surface:   assist.ResultSurfacePanel,
			Kind:      assist.ResultKindWorkProduct,
		}, nil
	default:
		return assist.ToolResult{}, fmt.Errorf("unsupported assist intent %q", call.Intent)
	}
}

func localizedAssistActionText(locale string, intent shortcuts.Intent) string {
	switch intent {
	case shortcuts.IntentCopyLast:
		switch locale {
		case "de", "de-DE":
			return "In die Zwischenablage kopiert."
		case "fr", "fr-FR":
			return "Copie dans le presse-papiers."
		case "es", "es-ES":
			return "Copiado al portapapeles."
		default:
			return "Copied to clipboard."
		}
	case shortcuts.IntentInsertLast:
		switch locale {
		case "de", "de-DE":
			return "Eingefuegt."
		case "fr", "fr-FR":
			return "Insere."
		case "es", "es-ES":
			return "Insertado."
		default:
			return "Inserted."
		}
	default:
		return ""
	}
}

func localizedAssistSummaryInsertedText(locale string) string {
	switch locale {
	case "de", "de-DE":
		return "Zusammenfassung eingefuegt."
	case "fr", "fr-FR":
		return "Resume insere."
	case "es", "es-ES":
		return "Resumen insertado."
	default:
		return "Summary inserted."
	}
}
