package textactions

import "context"
import "strings"

var ErrSummarizerNotConfigured = &summaryError{message: "summarizer not configured"}

type Source string

const (
	SourceSelection         Source = "selection"
	SourceLastTranscription Source = "last_transcription"
	SourceUtterance         Source = "utterance"
)

type Input struct {
	Text        string
	Instruction string
	Locale      string
	Source      Source
}

type Summarizer interface {
	Summarize(ctx context.Context, input Input) (string, error)
}

type SummarizerFunc func(context.Context, Input) (string, error)

func (f SummarizerFunc) Summarize(ctx context.Context, input Input) (string, error) {
	return f(ctx, input)
}

type SummaryTool struct {
	Summarizer Summarizer
}

type SummarizeContext struct {
	Selection         string
	LastTranscription string
	Utterance         string
	Locale            string
}

func ResolveSummarizeContext(ctx SummarizeContext) Input {
	instruction := normalize(ctx.Utterance)
	switch {
	case hasText(ctx.Selection):
		return Input{
			Text:        normalize(ctx.Selection),
			Instruction: instruction,
			Locale:      ctx.Locale,
			Source:      SourceSelection,
		}
	default:
		return Input{
			Text:        "",
			Instruction: instruction,
			Locale:      ctx.Locale,
			Source:      "",
		}
	}
}

func hasText(value string) bool {
	return normalize(value) != ""
}

func normalize(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

type summaryError struct {
	message string
}

func (e *summaryError) Error() string {
	return e.message
}
