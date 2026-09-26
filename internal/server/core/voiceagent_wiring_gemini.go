//go:build linux

package core

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/gemini"
)

// ── Gemini Live provider factory + bridge ───────────────────────────────────
//
// Gemini Live is an opt-in BYOK provider: buildProviderFactory registers it
// only when [providers.google] is enabled or the deployment explicitly selects
// it as the Voice Agent provider. It is never an implicit default.

type geminiProviderFactory struct {
	region string
}

func (f *geminiProviderFactory) NewProvider() vsserver.LiveProviderAdapter {
	return &geminiLiveBridge{inner: gemini.New(), region: f.region}
}

// geminiLiveBridge adapts the public gemini.Provider to the narrow interface
// the WebSocket handler consumes. The translation is mostly field-for-field;
// live enum types are rebuilt from the string fields on
// vsserver.LiveConfigFrame.
type geminiLiveBridge struct {
	inner  *gemini.Provider
	region string
}

func (b *geminiLiveBridge) Connect(ctx context.Context, cfg vsserver.LiveConfigFrame) error {
	if cfg.APIKey == "" {
		return errors.New("voiceagent: no Google API key configured for this deployment")
	}
	liveCfg := live.LiveConfig{
		Provider:         "google",
		Model:            cfg.Model,
		FallbackModel:    cfg.FallbackModel,
		APIKey:           cfg.APIKey,
		Voice:            cfg.Voice,
		FrameworkPrompt:  cfg.SystemPrompt,
		RefinementPrompt: cfg.RefinementPrompt,
		Locale:           cfg.Locale,
		Region:           b.region,
		Speaker:          cfg.Speaker,
		Tools:            kernelToolDefinitions(cfg.Tools),
		Policies: live.LivePolicies{
			EnableInputAudioTranscription:  true,
			EnableOutputAudioTranscription: true,
			ActivityDetection: live.ActivityDetectionPolicy{
				Automatic:         cfg.Automatic,
				StartSensitivity:  live.StartSensitivity(strings.ToLower(cfg.StartSensitivity)),
				EndSensitivity:    live.EndSensitivity(strings.ToLower(cfg.EndSensitivity)),
				PrefixPaddingMs:   cfg.PrefixPaddingMs,
				SilenceDurationMs: cfg.SilenceDurationMs,
				ActivityHandling:  live.ActivityHandling(strings.ToLower(cfg.ActivityHandling)),
				TurnCoverage:      live.TurnCoverage(strings.ToLower(cfg.TurnCoverage)),
			},
		},
	}
	if err := b.inner.Connect(ctx, liveCfg); err != nil {
		slog.Warn("voiceagent: Gemini Live connect failed", "err", err)
		return err
	}
	return nil
}

func (b *geminiLiveBridge) SendAudio(chunk []byte) error { return b.inner.SendAudio(chunk) }
func (b *geminiLiveBridge) SendAudioStreamEnd() error    { return b.inner.SendAudioStreamEnd() }
func (b *geminiLiveBridge) SendText(text string) error   { return b.inner.SendText(text) }
func (b *geminiLiveBridge) Close() error                 { return b.inner.Close() }
func (b *geminiLiveBridge) Name() string                 { return b.inner.Name() }
func (b *geminiLiveBridge) SupportsLiveKitTransport() bool {
	return true
}

func (b *geminiLiveBridge) UpdateInstructions(_ context.Context, cfg vsserver.LiveConfigFrame) error {
	text := vsserver.RenderHostInstructionUpdate(cfg)
	if text == "" {
		return nil
	}
	return b.inner.SendText(text)
}

func (b *geminiLiveBridge) SendToolResponse(frame vsserver.ToolResponseFrame) error {
	return b.inner.SendToolResponse(live.ToolResponse{
		ID:       frame.ID,
		Name:     frame.Name,
		Response: frame.Response,
	})
}

func (b *geminiLiveBridge) Receive(ctx context.Context) (*vsserver.LiveMessage, error) {
	msg, err := b.inner.Receive(ctx)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, nil
	}
	return mapKernelLiveMessage(msg), nil
}
