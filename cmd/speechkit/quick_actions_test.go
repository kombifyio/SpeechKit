package main

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/output"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/internal/stt"
	"github.com/kombifyio/SpeechKit/internal/textactions"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type fakeTranscriptPaster struct {
	result *stt.Result
	calls  int
}

func (f *fakeTranscriptPaster) Handle(_ context.Context, result *stt.Result, _ output.Target) error {
	f.calls++
	if result != nil {
		clone := *result
		f.result = &clone
	}
	return nil
}

func TestQuickActionCoordinatorPassesThroughUnknownAgentUtterance(t *testing.T) {
	state := &appState{activeMode: "agent"}
	actions := newQuickActionCoordinator(state, nil)

	handled, err := actions.Intercept(context.Background(), speechkit.Transcript{
		Text: "open the pod bay doors",
	}, nil)
	if err != nil {
		t.Fatalf("Intercept() error = %v", err)
	}
	if handled {
		t.Fatal("Intercept() handled = true, want false for non-shortcut agent utterance")
	}
}

func TestQuickActionCoordinatorPassesThroughUnknownDictation(t *testing.T) {
	state := &appState{activeMode: "dictate"}
	actions := newQuickActionCoordinator(state, nil)

	handled, err := actions.Intercept(context.Background(), speechkit.Transcript{
		Text: "normal dictation text",
	}, nil)
	if err != nil {
		t.Fatalf("Intercept() error = %v", err)
	}
	if handled {
		t.Fatal("Intercept() handled = true, want false in dictate mode")
	}
}

func TestQuickActionCoordinatorSummarizeUsesSelectionAndInstruction(t *testing.T) {
	state := &appState{activeMode: "dictate"}
	paster := &fakeTranscriptPaster{}
	actions := newQuickActionCoordinator(state, paster)
	actions.captureSelection = func(context.Context) (string, error) {
		return "First sentence. Second sentence. Third sentence.", nil
	}
	actions.summarizer = textactions.SummaryTool{
		Summarizer: textactions.SummarizerFunc(func(_ context.Context, input textactions.Input) (string, error) {
			if got, want := input.Text, "First sentence. Second sentence. Third sentence."; got != want {
				t.Fatalf("input.Text = %q, want %q", got, want)
			}
			if got, want := input.Instruction, "in zwei saetzen"; got != want {
				t.Fatalf("input.Instruction = %q, want %q", got, want)
			}
			return "First sentence. Second sentence.", nil
		}),
	}

	handled, err := actions.Intercept(context.Background(), speechkit.Transcript{
		Text: "Zusammenfassung in zwei saetzen",
	}, nil)
	if err != nil {
		t.Fatalf("Intercept() error = %v", err)
	}
	if !handled {
		t.Fatal("Intercept() handled = false, want true for summarize command")
	}
	if paster.calls != 1 {
		t.Fatalf("paster calls = %d, want %d", paster.calls, 1)
	}
	if paster.result == nil || paster.result.Text != "First sentence. Second sentence." {
		t.Fatalf("pasted text = %q, want summarized selection", paster.result.Text)
	}
}

func TestQuickActionCoordinatorSummarizeWorksInAgentMode(t *testing.T) {
	state := &appState{activeMode: "agent"}
	paster := &fakeTranscriptPaster{}
	actions := newQuickActionCoordinator(state, paster)
	actions.captureSelection = func(context.Context) (string, error) {
		return "First sentence. Second sentence.", nil
	}
	actions.summarizer = textactions.SummaryTool{
		Summarizer: textactions.SummarizerFunc(func(_ context.Context, input textactions.Input) (string, error) {
			if got, want := input.Text, "First sentence. Second sentence."; got != want {
				t.Fatalf("input.Text = %q, want %q", got, want)
			}
			return "Short summary.", nil
		}),
	}

	handled, err := actions.Intercept(context.Background(), speechkit.Transcript{
		Text: "summarize",
	}, nil)
	if err != nil {
		t.Fatalf("Intercept() error = %v", err)
	}
	if !handled {
		t.Fatal("Intercept() handled = false, want true for summarize command in agent mode")
	}
	if paster.result == nil || paster.result.Text != "Short summary." {
		t.Fatalf("pasted text = %q, want summarized selection", paster.result.Text)
	}
}

func TestQuickActionCoordinatorSummarizeSkipsWhenSelectionMissing(t *testing.T) {
	state := &appState{activeMode: "dictate"}
	paster := &fakeTranscriptPaster{}
	actions := newQuickActionCoordinator(state, paster)
	actions.captureSelection = func(context.Context) (string, error) {
		return "", nil
	}
	actions.summarizer = textactions.SummaryTool{
		Summarizer: textactions.SummarizerFunc(func(_ context.Context, input textactions.Input) (string, error) {
			if got, want := input.Text, "Stored transcript for fallback."; got != want {
				t.Fatalf("input.Text = %q, want %q", got, want)
			}
			if got, want := input.Source, textactions.SourceLastTranscription; got != want {
				t.Fatalf("input.Source = %q, want %q", got, want)
			}
			return "Fallback summary.", nil
		}),
	}
	state.lastTranscriptionText = "Stored transcript for fallback."

	handled, err := actions.Intercept(context.Background(), speechkit.Transcript{
		Text: "summarize",
	}, nil)
	if err != nil {
		t.Fatalf("Intercept() error = %v", err)
	}
	if !handled {
		t.Fatal("Intercept() handled = false, want true for summarize command")
	}
	if paster.calls != 1 {
		t.Fatalf("paster calls = %d, want %d", paster.calls, 1)
	}
	if paster.result == nil || paster.result.Text != "Fallback summary." {
		t.Fatalf("pasted text = %q, want fallback summary", paster.result.Text)
	}
}

func TestQuickActionCoordinatorExecuteSummarizeFallsBackToLastTranscription(t *testing.T) {
	state := &appState{
		activeMode:            "agent",
		lastTranscriptionText: "Stored transcript for command fallback.",
	}
	paster := &fakeTranscriptPaster{}
	actions := newQuickActionCoordinator(state, paster)
	actions.summarizer = textactions.SummaryTool{
		Summarizer: textactions.SummarizerFunc(func(_ context.Context, input textactions.Input) (string, error) {
			if got, want := input.Text, "Stored transcript for command fallback."; got != want {
				t.Fatalf("input.Text = %q, want %q", got, want)
			}
			if got, want := input.Source, textactions.SourceLastTranscription; got != want {
				t.Fatalf("input.Source = %q, want %q", got, want)
			}
			return "Command fallback summary.", nil
		}),
	}

	err := actions.Execute(context.Background(), speechkit.Command{
		Type: speechkit.CommandSummarizeSelection,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if paster.calls != 1 {
		t.Fatalf("paster calls = %d, want %d", paster.calls, 1)
	}
	if paster.result == nil || paster.result.Text != "Command fallback summary." {
		t.Fatalf("pasted text = %q, want command fallback summary", paster.result.Text)
	}
}

func TestQuickActionCoordinatorSummarizeReportsMissingInputAsWarning(t *testing.T) {
	state := &appState{activeMode: "agent"}
	paster := &fakeTranscriptPaster{}
	actions := newQuickActionCoordinator(state, paster)
	actions.captureSelection = func(context.Context) (string, error) {
		return "", nil
	}

	handled, err := actions.Intercept(context.Background(), speechkit.Transcript{
		Text: "summarize",
	}, nil)
	if err != nil {
		t.Fatalf("Intercept() error = %v", err)
	}
	if !handled {
		t.Fatal("Intercept() handled = false, want true for summarize command")
	}
	if paster.calls != 0 {
		t.Fatalf("paster calls = %d, want %d", paster.calls, 0)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.logEntries) == 0 {
		t.Fatal("logEntries = 0, want warning entry")
	}
	last := state.logEntries[len(state.logEntries)-1]
	if last.Type != "warn" {
		t.Fatalf("last log type = %q, want warn", last.Type)
	}
	if last.Message != msgSummarizeInputMissing {
		t.Fatalf("last log message = %q, want %q", last.Message, msgSummarizeInputMissing)
	}
}

func TestQuickActionCoordinatorResolveDecisionFromTranscriptMatchesCommandCopyLast(t *testing.T) {
	actions := newQuickActionCoordinator(&appState{}, nil)

	fromTranscript, ok := actions.resolveDecisionFromTranscript(speechkit.Transcript{
		Text: "copy last",
	})
	if !ok {
		t.Fatal("resolveDecisionFromTranscript() handled = false, want true")
	}

	fromCommand, ok := actions.resolveDecisionFromCommand(speechkit.Command{
		Type: speechkit.CommandCopyLastTranscription,
	})
	if !ok {
		t.Fatal("resolveDecisionFromCommand() handled = false, want true")
	}

	if fromTranscript != fromCommand {
		t.Fatalf("decision mismatch: transcript=%+v command=%+v", fromTranscript, fromCommand)
	}
}

func TestQuickActionCoordinatorResolveDecisionFromTranscriptMatchesCommandInsertLast(t *testing.T) {
	actions := newQuickActionCoordinator(&appState{}, nil)

	fromTranscript, ok := actions.resolveDecisionFromTranscript(speechkit.Transcript{
		Text: "insert last",
	})
	if !ok {
		t.Fatal("resolveDecisionFromTranscript() handled = false, want true")
	}

	fromCommand, ok := actions.resolveDecisionFromCommand(speechkit.Command{
		Type: speechkit.CommandInsertLastTranscription,
	})
	if !ok {
		t.Fatal("resolveDecisionFromCommand() handled = false, want true")
	}

	if fromTranscript != fromCommand {
		t.Fatalf("decision mismatch: transcript=%+v command=%+v", fromTranscript, fromCommand)
	}
}

func TestQuickActionCoordinatorResolveDecisionFromTranscriptMatchesCommandSummarize(t *testing.T) {
	actions := newQuickActionCoordinator(&appState{}, nil)

	fromTranscript, ok := actions.resolveDecisionFromTranscript(speechkit.Transcript{
		Text: "summarize in two bullet points",
	})
	if !ok {
		t.Fatal("resolveDecisionFromTranscript() handled = false, want true")
	}

	fromCommand, ok := actions.resolveDecisionFromCommand(speechkit.Command{
		Type: speechkit.CommandSummarizeSelection,
		Text: "in two bullet points",
	})
	if !ok {
		t.Fatal("resolveDecisionFromCommand() handled = false, want true")
	}

	if fromTranscript != fromCommand {
		t.Fatalf("decision mismatch: transcript=%+v command=%+v", fromTranscript, fromCommand)
	}
}

func TestQuickActionCoordinatorUsesInjectedResolver(t *testing.T) {
	registry := shortcuts.NewRegistry()
	registry.RegisterLeadingFillers("de", "bitte")
	registry.RegisterLexicon(shortcuts.IntentLexicon{
		Intent: shortcuts.IntentSummarize,
		Locale: "de",
		Phrases: []shortcuts.Phrase{
			{Value: "kurzfassung", Prefix: true},
		},
	})

	actions := newQuickActionCoordinator(&appState{}, nil, shortcuts.NewResolver(registry))

	decision, ok := actions.resolveDecisionFromTranscript(speechkit.Transcript{
		Text:     "Bitte Kurzfassung in drei Punkten",
		Language: "de-DE",
	})
	if !ok {
		t.Fatal("resolveDecisionFromTranscript() handled = false, want true")
	}
	if got, want := decision.kind, quickActionSummarize; got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
	if got, want := decision.instruction, "in drei punkten"; got != want {
		t.Fatalf("instruction = %q, want %q", got, want)
	}
}
