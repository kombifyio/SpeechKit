package textactions

import (
	"context"
	"strings"
)

func (t SummaryTool) Run(ctx context.Context, input Input) (string, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return "", ErrSummarizeInputUnavailable
	}

	if t.Summarizer != nil {
		return t.Summarizer.Summarize(ctx, input)
	}

	return "", ErrSummarizerNotConfigured
}
