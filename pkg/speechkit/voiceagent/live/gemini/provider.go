// Package gemini adapts the Google Gemini Live API (via the
// google.golang.org/genai SDK) to [live.LiveProvider]. It is an opt-in
// bring-your-own-key provider: callers supply their own Google AI API key in
// the [live.LiveConfig], and SpeechKit never selects it by default.
package gemini

import (
	"context"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"log/slog"
	"strings"
	"sync"

	"google.golang.org/genai"
)

type geminiLiveSession interface {
	SendRealtimeInput(input genai.LiveRealtimeInput) error
	SendToolResponse(input genai.LiveToolResponseInput) error
	Receive() (*genai.LiveServerMessage, error)
	Close() error
}

// Provider implements live.LiveProvider using the Google GenAI Live API.
type Provider struct {
	mu         sync.RWMutex
	client     *genai.Client
	session    geminiLiveSession
	resume     *live.ResumeHandle // live.Session resumption handle: DPAPI-at-rest on Windows, TTL-bounded everywhere.
	lastConfig *live.LiveConfig   // Stored for reconnection
}

// defaultGeminiLiveModel is the kernel-level default for realtime sessions
// when the caller does not specify a Model in live.LiveConfig. It tracks the
// current Gemini Live family primary model.
//
// As of April 2026 this is gemini-3.1-flash-live-preview. When Google
// promotes a model to GA or publishes a newer preview, bump this constant
// and update both defaults in internal/config/config.go
// (defaultGeminiNativeAudioModel / fallbackGeminiNativeAudioModel) so the
// whole Framework picks up the new default without breaking running
// deployments that pinned an explicit live.LiveConfig.Model.
const defaultGeminiLiveModel = "gemini-3.1-flash-live-preview"

// New creates a Gemini Live provider.
func New() *Provider {
	return &Provider{
		resume: live.NewResumeHandle(),
	}
}

// Connect opens a Gemini Live session for cfg, trying cfg.FallbackModel when
// the primary model fails to connect. It implements [live.LiveProvider].
func (g *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if err := validateGeminiLiveConfig(cfg); err != nil {
		return err
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      cfg.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: buildGeminiLiveHTTPOptions(cfg),
	})
	if err != nil {
		return fmt.Errorf("gemini live: create client: %w", err)
	}
	g.client = client

	connectCfg := buildGeminiLiveConnectConfig(cfg)

	// Build the candidate list: primary model first, then optional fallback.
	// live.ShouldTryFallback decides whether the fallback is distinct enough from
	// the primary to be worth attempting; the function is tested in isolation
	// because the genai SDK is awkward to mock end-to-end.
	primary := resolvedGeminiLiveModel(cfg)
	candidates := []string{primary}
	if fallback := strings.TrimSpace(cfg.FallbackModel); live.ShouldTryFallback(primary, fallback) {
		candidates = append(candidates, fallback)
	}

	var lastErr error
	for i, model := range candidates {
		session, err := client.Live.Connect(ctx, model, connectCfg)
		if err == nil {
			g.mu.Lock()
			g.session = session
			g.lastConfig = &cfg
			g.mu.Unlock()
			if i == 0 {
				slog.Info("Gemini Live connected",
					"model", model,
					"voice", connectCfg.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName,
					// configured_region is logged for compliance evidence only.
					// Gemini Live (May 2026) uses a single global endpoint; the
					// region field does NOT redirect traffic. Data residency is
					// controlled at the Google Cloud project level.
					// See docs/compliance/byok-gemini-region-pinning.md.
					"configured_region", cfg.Region,
				)
			} else {
				slog.Warn("Gemini Live connected via fallback model",
					"primary", candidates[0],
					"fallback", model,
					"primary_err", lastErr,
					"voice", connectCfg.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName,
					"configured_region", cfg.Region,
				)
			}
			return nil
		}
		lastErr = err
		if i+1 < len(candidates) {
			slog.Warn("Gemini Live primary connect failed; trying fallback",
				"primary", model,
				"err", err,
			)
		}
	}
	return fmt.Errorf("gemini live: connect to %v: %w", candidates, lastErr)
}

// SendAudio streams one 16 kHz S16 mono PCM chunk to the session.
func (g *Provider) SendAudio(chunk []byte) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: %w", live.ErrNotConnected)
	}

	return session.SendRealtimeInput(genai.LiveRealtimeInput{
		Audio: &genai.Blob{
			MIMEType: "audio/pcm;rate=16000",
			Data:     chunk,
		},
	})
}

// SendAudioStreamEnd tells the model the current audio stream has ended.
func (g *Provider) SendAudioStreamEnd() error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: %w", live.ErrNotConnected)
	}

	return session.SendRealtimeInput(genai.LiveRealtimeInput{
		AudioStreamEnd: true,
	})
}

// Receive blocks for the next server message and maps it to a
// [live.LiveMessage].
func (g *Provider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("gemini live: %w", live.ErrNotConnected)
	}

	// genai.Session.Receive() blocks until a message arrives.
	// Context cancellation is handled by the caller closing the session/WebSocket.
	resp, err := session.Receive()
	if err != nil {
		return nil, fmt.Errorf("gemini live: receive: %w", err)
	}

	msg := &live.LiveMessage{}

	if resp.ServerContent != nil {
		if resp.ServerContent.ModelTurn != nil {
			for _, part := range resp.ServerContent.ModelTurn.Parts {
				if part.InlineData != nil && len(part.InlineData.Data) > 0 {
					msg.Audio = append(msg.Audio, part.InlineData.Data...)
				}
				if part.Text != "" {
					msg.Text += part.Text
				}
			}
		}
		msg.Done = resp.ServerContent.TurnComplete

		// Transcription fields.
		if resp.ServerContent.InputTranscription != nil {
			msg.InputTranscript = resp.ServerContent.InputTranscription.Text
			msg.InputTranscriptDone = resp.ServerContent.InputTranscription.Finished
		}
		if resp.ServerContent.OutputTranscription != nil {
			msg.OutputTranscript = resp.ServerContent.OutputTranscription.Text
			msg.OutputTranscriptDone = resp.ServerContent.OutputTranscription.Finished
		}

		// Barge-in: user interrupted model.
		if resp.ServerContent.Interrupted {
			msg.Interrupted = true
		}
	}

	if resp.ToolCall != nil {
		for _, call := range resp.ToolCall.FunctionCalls {
			if call == nil {
				continue
			}
			msg.ToolCalls = append(msg.ToolCalls, live.ToolCall{
				ID:   call.ID,
				Name: call.Name,
				Args: call.Args,
			})
		}
	}

	if resp.ToolCallCancellation != nil {
		msg.ToolCallCancellationIDs = append(msg.ToolCallCancellationIDs, resp.ToolCallCancellation.IDs...)
	}

	// GoAway: server signals imminent session end.
	if resp.GoAway != nil {
		msg.GoAway = true
		slog.Warn("Gemini Live GoAway received — session will end soon")
	}

	// live.Session resumption: store handle for reconnection.
	// The handle is kept in a DPAPI-protected container with a TTL so a stale
	// memory dump cannot replay old sessions indefinitely.
	if resp.SessionResumptionUpdate != nil && resp.SessionResumptionUpdate.NewHandle != "" {
		g.resume.Set(resp.SessionResumptionUpdate.NewHandle)
		msg.SessionResumable = true
	}

	return live.NormalizeMessageEvents(msg, "live.server_message"), nil
}

// SendText sends a text turn to the session.
func (g *Provider) SendText(text string) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: %w", live.ErrNotConnected)
	}

	return session.SendRealtimeInput(genai.LiveRealtimeInput{
		Text: text,
	})
}

// SendToolResponse returns a tool result to the model.
func (g *Provider) SendToolResponse(response live.ToolResponse) error {
	g.mu.RLock()
	session := g.session
	g.mu.RUnlock()
	if session == nil {
		return fmt.Errorf("gemini live: %w", live.ErrNotConnected)
	}

	return session.SendToolResponse(genai.LiveToolResponseInput{
		FunctionResponses: []*genai.FunctionResponse{
			{
				ID:           response.ID,
				Name:         response.Name,
				Response:     response.Response,
				Scheduling:   mapToolResponseScheduling(response.Scheduling),
				WillContinue: response.WillContinue,
			},
		},
	})
}

// Reconnect re-establishes the session using the stored resumption handle.
// If the handle has expired (TTL) or been cleared, a fresh session is opened.
func (g *Provider) Reconnect(ctx context.Context) error {
	g.mu.RLock()
	lastCfg := g.lastConfig
	g.mu.RUnlock()
	resumeHandleValue := g.resume.Get()

	if lastCfg == nil {
		return fmt.Errorf("gemini live: no stored config for reconnect")
	}
	if err := validateGeminiLiveConfig(*lastCfg); err != nil {
		return err
	}

	// Close existing session. Log, but do not abort the reconnect — the old
	// session may already be half-dead on the wire, and aborting here would
	// leave the caller without a working session at all.
	if err := g.Close(); err != nil {
		slog.Warn("gemini live: close prior session during reconnect", "err", err)
	}

	// Re-create client.
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      lastCfg.APIKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: buildGeminiLiveHTTPOptions(*lastCfg),
	})
	if err != nil {
		return fmt.Errorf("gemini live: reconnect create client: %w", err)
	}

	model := resolvedGeminiLiveModel(*lastCfg)

	connectCfg := buildGeminiLiveConnectConfig(*lastCfg)
	connectCfg.SessionResumption = buildGeminiLiveSessionResumptionConfig(resumeHandleValue)

	session, err := client.Live.Connect(ctx, model, connectCfg)
	if err != nil {
		return fmt.Errorf("gemini live: reconnect to %s: %w", model, err)
	}

	g.mu.Lock()
	g.client = client
	g.session = session
	g.mu.Unlock()

	slog.Info("Gemini Live reconnected", "model", model, "had_resume_handle", resumeHandleValue != "")
	return nil
}

// Close ends the session and clears the stored resumption handle.
func (g *Provider) Close() error {
	g.mu.Lock()
	session := g.session
	g.session = nil
	g.client = nil
	g.mu.Unlock()

	// Wipe the resumption handle on close. Any caller that intends to
	// reconnect must have already captured it via Reconnect() before calling
	// Close(); keeping it around after a completed session only widens the
	// window for misuse if memory is later inspected.
	if g.resume != nil {
		g.resume.Clear()
	}

	if session != nil {
		if err := session.Close(); err != nil {
			return fmt.Errorf("gemini live: close session: %w", err)
		}
	}
	return nil
}

// Name returns the provider name "gemini-live".
func (g *Provider) Name() string { return "gemini-live" }

// SessionCapabilities reports the Gemini Live descriptor capabilities.
func (g *Provider) SessionCapabilities() live.SessionCapabilities {
	return live.SessionCapabilitiesForProvider("google")
}
