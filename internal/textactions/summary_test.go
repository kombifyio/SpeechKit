package textactions

import (
	"context"
	"errors"
	"testing"
)

func TestResolveSummarizeContextPrefersSelection(t *testing.T) {
	input := ResolveSummarizeContext(SummarizeContext{
		Selection:         "selected text",
		LastTranscription: "last transcription",
	})

	if got, want := input.Text, "selected text"; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
	if got, want := input.Source, SourceSelection; got != want {
		t.Fatalf("Source = %q, want %q", got, want)
	}
}

func TestResolveSummarizeContextDoesNotUseUtteranceAsSourceText(t *testing.T) {
	input := ResolveSummarizeContext(SummarizeContext{
		Utterance:         "Summarize this",
		LastTranscription: "last transcription",
	})

	if got, want := input.Text, ""; got != want {
		t.Fatalf("Text = %q, want empty when no selection is available", got)
	}
	if got, want := input.Source, Source(""); got != want {
		t.Fatalf("Source = %q, want %q", got, want)
	}
	if got, want := input.Instruction, "Summarize this"; got != want {
		t.Fatalf("Instruction = %q, want %q", got, want)
	}
}

func TestSummaryToolUsesInjectedSummarizer(t *testing.T) {
	tool := SummaryTool{
		Summarizer: SummarizerFunc(func(ctx context.Context, input Input) (string, error) {
			if got, want := input.Text, "selected text"; got != want {
				t.Fatalf("text = %q, want %q", got, want)
			}
			if got, want := input.Instruction, "in two bullet points"; got != want {
				t.Fatalf("instruction = %q, want %q", got, want)
			}
			return "summary", nil
		}),
	}

	got, err := tool.Run(context.Background(), Input{
		Text:        "selected text",
		Instruction: "in two bullet points",
		Source:      SourceSelection,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got != "summary" {
		t.Fatalf("Run() = %q, want %q", got, "summary")
	}
}

func TestSummaryToolRequiresConfiguredSummarizer(t *testing.T) {
	tool := SummaryTool{}

	got, err := tool.Run(context.Background(), Input{
		Text:   "This is a long sentence. This is the second sentence.",
		Source: SourceSelection,
	})
	if !errors.Is(err, ErrSummarizerNotConfigured) {
		t.Fatalf("Run() error = %v, want %v", err, ErrSummarizerNotConfigured)
	}
	if got != "" {
		t.Fatalf("Run() = %q, want empty output when no summarizer is configured", got)
	}
}

func TestSummaryToolReturnsErrorFromInjectedSummarizer(t *testing.T) {
	wantErr := errors.New("boom")
	tool := SummaryTool{
		Summarizer: SummarizerFunc(func(context.Context, Input) (string, error) {
			return "", wantErr
		}),
	}

	if _, err := tool.Run(context.Background(), Input{Text: "selected text"}); !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
}
