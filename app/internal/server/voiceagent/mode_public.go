//go:build linux

package voiceagent

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/app/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// Hosted mode authority is supplied only by the hosting integration.
var errModeUnavailable = errors.New("voiceagent: hosted mode integration unavailable")

const modeThinkModelLabel = "hosted-mode-unavailable"

func ModeTurnSignerPreflight() error { return errModeUnavailable }

func (cfg LiveConfigFrame) ModeNow() (string, string, string, int64) {
	if cfg.Mode != nil {
		return cfg.Mode.Current()
	}
	return cfg.ModeBinding, cfg.ModeSignature, cfg.ModeEndpoint, cfg.ModeExpiresAt
}

// Detect supplied hosted authority so it is refused rather than silently
// falling back to an unrelated local reasoner.
func (cfg LiveConfigFrame) hasMode() bool {
	binding, signature, endpoint, expires := cfg.ModeNow()
	return binding != "" || signature != "" || endpoint != "" || expires != 0
}
func (cfg LiveConfigFrame) modeOnly() bool {
	return cfg.DirectEndpoint == nil && cfg.AgentTargetID == "" && cfg.hasMode()
}

type turnObserver interface{ Observe(string, string) }
type modeChatAgent struct{}

func newModeChatAgent(LiveConfigFrame, []ToolDefinitionFrame, func(context.Context, ToolCall) (map[string]any, bool)) (*modeChatAgent, error) {
	return nil, errModeUnavailable
}
func (*modeChatAgent) Run(context.Context, cascaded.AgentInput) (cascaded.AgentOutput, error) {
	return cascaded.AgentOutput{}, errModeUnavailable
}
func (*modeChatAgent) StreamTurn(context.Context, cascaded.AgentInput, string, func(string) error) (cascaded.AgentOutput, error) {
	return cascaded.AgentOutput{}, errModeUnavailable
}
func (*modeChatAgent) SetRefusalNotifier(func(string)) {}
func (*modeChatAgent) SetInstructions(string, string)  {}

// No hosted session can be admitted by this standalone integration.
func reportModeSessionEnd(string, wssession.Identity, *wssession.ModeAuthority, string) {}

func modeEndReason(reason string) string {
	switch reason {
	case "client":
		return "client_ended"
	case "shutdown":
		return "server_closed"
	case "voice_budget_exhausted", "authorization_expired":
		return "budget_expired"
	case "error":
		return "provider_failed"
	default:
		return "socket_closed"
	}
}

// ModeAgentSlot preserves the standalone host's configured agent.
type ModeAgentSlot struct {
	mu             sync.RWMutex
	fallback, mode cascaded.Agent
}

func NewModeAgentSlot(fallback cascaded.Agent) *ModeAgentSlot {
	return &ModeAgentSlot{fallback: fallback}
}
func (s *ModeAgentSlot) Bind(agent cascaded.Agent) { s.mu.Lock(); defer s.mu.Unlock(); s.mode = agent }
func (s *ModeAgentSlot) current() cascaded.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.mode != nil {
		return s.mode
	}
	return s.fallback
}
func (s *ModeAgentSlot) Run(ctx context.Context, input cascaded.AgentInput) (cascaded.AgentOutput, error) {
	if agent := s.current(); agent != nil {
		return agent.Run(ctx, input)
	}
	return cascaded.AgentOutput{}, errors.New("voiceagent: no agent model is configured")
}
func (s *ModeAgentSlot) Stream(ctx context.Context, input cascaded.AgentInput, emit func(string) error) (cascaded.AgentOutput, error) {
	agent := s.current()
	if agent == nil {
		return cascaded.AgentOutput{}, errors.New("voiceagent: no agent model is configured")
	}
	if streamer, ok := agent.(cascaded.StreamingAgent); ok {
		return streamer.Stream(ctx, input, emit)
	}
	out, err := agent.Run(ctx, input)
	if err == nil && emit != nil && strings.TrimSpace(out.Text) != "" {
		err = emit(out.Text)
	}
	return out, err
}
