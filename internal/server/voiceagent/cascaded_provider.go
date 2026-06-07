//go:build linux

package voiceagent

import (
	"context"

	"github.com/kombifyio/SpeechKit/internal/voiceagent/cascaded"
)

// The cascaded provider implementation lives in
// internal/voiceagent/cascaded (cross-platform). This file is the
// Linux-only adapter that translates between the server's local
// LiveConfigFrame / LiveMessage types and the cross-platform
// cascaded.SessionConfig / cascaded.Message types so the existing call
// sites in internal/server/core/voiceagent_wiring.go keep compiling
// against the same public names.

// Re-export the cascaded types under their historical server-package
// names. These aliases are intentionally type aliases (not new types)
// so existing code that passes app.STTRouter / app.TTSRouter etc.
// continues to type-check without conversions.
type (
	CascadedConfig = cascaded.Config
	CascadedSTT    = cascaded.STT
	CascadedAgent  = cascaded.Agent
	CascadedTTS    = cascaded.TTS
	CascadedDeps   = cascaded.Deps
)

// CascadedProvider wraps cascaded.Provider and adapts its
// SessionConfig/Message types to the server's LiveConfigFrame/
// LiveMessage types so the result satisfies LiveProviderAdapter.
type CascadedProvider struct {
	inner *cascaded.Provider
}

// NewCascadedProvider preserves the historical constructor name.
func NewCascadedProvider(deps CascadedDeps) *CascadedProvider {
	return &CascadedProvider{inner: cascaded.NewProvider(deps)}
}

// NewAgentFlowAdapter re-exports the cross-platform helper used by
// internal/server/core/voiceagent_wiring.go.
var NewAgentFlowAdapter = cascaded.NewAgentFlowAdapter

// Connect translates the rich server LiveConfigFrame into a minimal
// cascaded.SessionConfig and forwards.
func (p *CascadedProvider) Connect(ctx context.Context, cfg LiveConfigFrame) error {
	return p.inner.Connect(ctx, cascaded.SessionConfig{
		Locale:           cfg.Locale,
		Voice:            cfg.Voice,
		SystemPrompt:     cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Speaker:          cfg.Speaker,
	})
}

// UpdateInstructions translates rich LiveConfigFrame to SessionConfig.
func (p *CascadedProvider) UpdateInstructions(ctx context.Context, cfg LiveConfigFrame) error {
	return p.inner.UpdateInstructions(ctx, cascaded.SessionConfig{
		Locale:           cfg.Locale,
		Voice:            cfg.Voice,
		SystemPrompt:     cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Speaker:          cfg.Speaker,
	})
}

// SendAudio delegates.
func (p *CascadedProvider) SendAudio(chunk []byte) error { return p.inner.SendAudio(chunk) }

// SendAudioStreamEnd delegates.
func (p *CascadedProvider) SendAudioStreamEnd() error { return p.inner.SendAudioStreamEnd() }

// SendText delegates.
func (p *CascadedProvider) SendText(text string) error { return p.inner.SendText(text) }

// Receive blocks for the next cascaded.Message and translates it into
// the server's LiveMessage type.
func (p *CascadedProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	msg, err := p.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	return &LiveMessage{
		Audio:                  msg.Audio,
		InputTranscript:        msg.InputTranscript,
		InputTranscriptDone:    msg.InputTranscriptDone,
		InputSpeakerLabel:      msg.InputSpeakerLabel,
		InputPersonID:          msg.InputPersonID,
		InputDisplayName:       msg.InputDisplayName,
		InputSpeakerConfidence: msg.InputSpeakerConfidence,
		OutputTranscript:       msg.OutputTranscript,
		OutputTranscriptDone:   msg.OutputTranscriptDone,
	}, nil
}

// Close delegates.
func (p *CascadedProvider) Close() error { return p.inner.Close() }

// Name returns the provider identifier.
func (p *CascadedProvider) Name() string { return p.inner.Name() }
