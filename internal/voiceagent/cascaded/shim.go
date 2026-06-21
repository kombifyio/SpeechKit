// Package cascaded re-exports the public pkg/speechkit/voiceagent/cascaded
// turn-based Voice Agent provider so existing kernel/adapter call sites
// (internal/server/voiceagent, internal/voiceagent, cmd/sk-localprobe, scripts)
// compile unchanged. It additionally provides the Genkit agent-flow adapter
// (NewAgentFlowAdapter) that bridges a flows.DefineAgentFlow Genkit flow to the
// public cascaded.Agent interface — kept here (internal) so the public package
// carries no Genkit dependency. New cascaded code goes in the public package.
package cascaded

import (
	"context"
	"errors"

	"github.com/firebase/genkit/go/core"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	pkgcascaded "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// Re-exported public types.
type (
	Provider        = pkgcascaded.Provider
	STT             = pkgcascaded.STT
	Agent           = pkgcascaded.Agent
	TTS             = pkgcascaded.TTS
	Deps            = pkgcascaded.Deps
	SessionConfig   = pkgcascaded.SessionConfig
	Message         = pkgcascaded.Message
	AgentInput      = pkgcascaded.AgentInput
	AgentOutput     = pkgcascaded.AgentOutput
	Config          = pkgcascaded.Config
	SpeakerStreamer = pkgcascaded.SpeakerStreamer
)

// Re-exported public functions.
var (
	NewProvider   = pkgcascaded.NewProvider
	ChunkAudio    = pkgcascaded.ChunkAudio
	ChunkRMS      = pkgcascaded.ChunkRMS
	PCMDurationMs = pkgcascaded.PCMDurationMs
)

// agentFlowAdapter bridges a Genkit agent flow to the public cascaded.Agent
// interface, converting between the public AgentInput/AgentOutput and the
// internal flows.AgentInput/AgentOutput types.
type agentFlowAdapter struct {
	Flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
}

// NewAgentFlowAdapter wraps a Genkit agent flow so it satisfies Agent.
func NewAgentFlowAdapter(flow *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]) Agent {
	return &agentFlowAdapter{Flow: flow}
}

func (a *agentFlowAdapter) Run(ctx context.Context, input AgentInput) (AgentOutput, error) {
	if a.Flow == nil {
		return AgentOutput{}, errors.New("cascaded: agent flow is nil")
	}
	out, err := a.Flow.Run(ctx, flows.AgentInput{
		Utterance:         input.Utterance,
		Locale:            input.Locale,
		Selection:         input.Selection,
		LastTranscription: input.LastTranscription,
		SystemPrompt:      input.SystemPrompt,
	})
	if err != nil {
		return AgentOutput{}, err
	}
	return AgentOutput{Text: out.Text, Action: out.Action}, nil
}
