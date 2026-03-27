package textactions

import (
	"context"
	"strings"
)

func (t SummaryTool) Run(ctx context.Context, input Input) (string, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return "", nil
	}

	if t.Summarizer != nil {
		return t.Summarizer.Summarize(ctx, input)
	}

	return "", ErrSummarizerNotConfigured
}
