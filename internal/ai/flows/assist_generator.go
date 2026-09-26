package flows

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
)

// AssistGenerator adapts an Assist flow to the public assist.Generator
// contract so both reference hosts (desktop and server) feed the same LLM
// flow into an assist.Service. A nil flow yields a nil Generator, which the
// service reports through HasGenerator() == false.
//
// The adapter owns the LLM-path defaults the private pipeline used to apply:
// every generated result is a panel answer (Surface panel, Kind answer), and a
// flow failure is wrapped as "assist: LLM failed: <err>" so the hosts' error
// classification keeps seeing the provider message.
func AssistGenerator(flow *Flow[AssistInput, AssistOutput]) assist.Generator {
	if flow == nil {
		return nil
	}
	return assistGenerator{flow: flow}
}

type assistGenerator struct {
	flow *Flow[AssistInput, AssistOutput]
}

func (g assistGenerator) GenerateAssist(ctx context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	output, err := g.flow.Run(ctx, AssistInput{
		Utterance: req.Text,
		Locale:    req.Locale,
		Selection: req.Selection,
		Context:   req.Context,
	})
	if err != nil {
		return speechkit.AssistResult{}, fmt.Errorf("assist: LLM failed: %w", err)
	}
	return speechkit.AssistResult{
		Text:      output.Text,
		SpeakText: output.SpeakText,
		Action:    output.Action,
		Locale:    output.Locale,
		Surface:   speechkit.AssistSurfacePanel,
		Kind:      assist.KindAnswer,
	}, nil
}
