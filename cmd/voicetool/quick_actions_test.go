package main

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/output"
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
		Summarizer: textactions.SummarizerFunc(func(context.Context, textactions.Input) (string, error) {
			t.Fatal("summarizer should not run without selection")
			return "", nil
		}),
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
}
