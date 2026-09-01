// Package foundry adapts the OpenAI Realtime provider to Microsoft Foundry.
// Foundry serves the identical OpenAI Realtime WebSocket protocol from the
// Azure-hosted v1 surface (wss://<account-host>/openai/v1/realtime?model=
// <deployment>), so this package wraps live/openai and only changes identity,
// endpoint enforcement, and the default deployment name. Auth is the shared
// Foundry API key sent as a Bearer token, which the v1 surface accepts.
package foundry

import (
	"context"
	"errors"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

// DefaultRealtimeModel is the default Foundry realtime deployment name.
// Foundry's model parameter addresses a deployment, so users who named their
// deployment differently override this via config.
const DefaultRealtimeModel = openai.DefaultRealtimeModel

// Provider implements live.LiveProvider against a Microsoft Foundry realtime
// deployment by delegating the full OpenAI Realtime protocol to live/openai.
type Provider struct {
	*openai.Provider
}

// New returns a fresh Foundry realtime provider.
func New() *Provider {
	return &Provider{Provider: openai.New()}
}

// Name identifies the provider in Voice Agent logs.
func (p *Provider) Name() string { return "foundry-realtime" }

func (p *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider("foundry")
}

// Connect requires the Foundry realtime endpoint
// (wss://<account-host>/openai/v1/realtime) in cfg.Endpoint and then dials via
// the embedded OpenAI Realtime implementation.
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return errors.New("foundry realtime: Endpoint is required (derive it from the Foundry project endpoint, e.g. wss://<account-host>/openai/v1/realtime)")
	}
	return p.Provider.Connect(ctx, cfg)
}
