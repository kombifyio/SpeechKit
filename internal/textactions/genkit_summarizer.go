package textactions

import (
	"context"
	"fmt"

	"github.com/firebase/genkit/go/core"

	"github.com/kombifyio/SpeechKit/internal/ai/flows"
)

// FlowSummarizer implements Summarizer using a Genkit summarize flow.
type FlowSummarizer struct {
	Flow *core.Flow[flows.SummarizeInput, string, struct{}]
}

func (s *FlowSummarizer) Summarize(ctx context.Context, input Input) (string, error) {
	if s.Flow == nil {
		return "", ErrSummarizerNotConfigured
	}
	if input.Text == "" {
		return "", ErrSummarizeInputUnavailable
	}

	result, err := s.Flow.Run(ctx, flows.SummarizeInput{
		Text:        input.Text,
		Instruction: input.Instruction,
		Locale:      input.Locale,
	})
	if err != nil {
		return "", fmt.Errorf("genkit summarize: %w", err)
	}
	return result, nil
}
