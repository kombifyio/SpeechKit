package flows

import (
	"context"
	"errors"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// cascadedAgent bridges an agent flow to the public cascaded.Agent
// interface, converting between the public cascaded.AgentInput/AgentOutput
// and the flow's AgentInput/AgentOutput types. It lives here (not in the
// public cascaded package) so pkg/speechkit carries no AI-runtime
// dependency.
type cascadedAgent struct {
	flow *Flow[AgentInput, AgentOutput]
}

// NewCascadedAgent wraps an agent flow so it satisfies cascaded.Agent.
func NewCascadedAgent(flow *Flow[AgentInput, AgentOutput]) cascaded.Agent {
	return &cascadedAgent{flow: flow}
}

func (a *cascadedAgent) Run(ctx context.Context, input cascaded.AgentInput) (cascaded.AgentOutput, error) {
	if a.flow == nil {
		return cascaded.AgentOutput{}, errors.New("cascaded: agent flow is nil")
	}
	out, err := a.flow.Run(ctx, AgentInput{
		Utterance:         input.Utterance,
		Locale:            input.Locale,
		Selection:         input.Selection,
		LastTranscription: input.LastTranscription,
		SystemPrompt:      input.SystemPrompt,
	})
	if err != nil {
		return cascaded.AgentOutput{}, err
	}
	return cascaded.AgentOutput{Text: out.Text, Action: out.Action}, nil
}
