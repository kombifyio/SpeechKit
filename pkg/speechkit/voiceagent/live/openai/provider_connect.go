package openai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Connect dials the OpenAI Realtime WebSocket, sends the configured
// instructions/voice/tools as a session.update, and waits for the
// session.updated acknowledgement before returning.
func (p *Provider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" && cfg.BearerToken == nil {
		return fmt.Errorf("openai realtime: %w", live.ErrMissingAPIKey)
	}
	model := resolveOpenAIRealtimeModel(cfg.Model)
	header, err := p.dialHeaders(ctx, cfg)
	if err != nil {
		return fmt.Errorf("openai realtime: %w", err)
	}
	baseURL := realtimeBaseURL(cfg)

	dialURL := p.dialURL(baseURL, model)
	conn, dialResp, err := websocket.Dial(ctx, dialURL, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		// Same-provider fallback: retry with FallbackModel when distinct.
		if fallback := strings.TrimSpace(cfg.FallbackModel); live.ShouldTryFallback(model, fallback) {
			fallbackURL := p.dialURL(baseURL, fallback)
			conn, dialResp, err = websocket.Dial(ctx, fallbackURL, &websocket.DialOptions{
				HTTPHeader: header,
			})
			if err != nil {
				return fmt.Errorf("openai realtime: dial primary %q + fallback %q failed: %w", model, fallback, err)
			}
			slog.Info("openai realtime: connected via fallback model", "primary", model, "fallback", fallback)
			model = fallback
		} else {
			return fmt.Errorf("openai realtime: dial %q: %w", model, err)
		}
	}
	if dialResp != nil && dialResp.Body != nil {
		_ = dialResp.Body.Close()
	}
	// PCM frames base64-encoded can grow large; OpenAI documents up to ~16 MB
	// per message but we cap at 4 MB to surface runaway-buffer bugs early.
	conn.SetReadLimit(4 << 20)

	cfgCopy := cfg
	cfgCopy.Model = model

	// closed/closeErr belong to closeMu — Close guards them with it. Take
	// closeMu first and mu inside it, the same order Close uses; reversing the
	// two would deadlock against a concurrent Close, and resetting them under
	// mu alone (as this did) races a reconnect against a teardown.
	p.closeMu.Lock()
	p.mu.Lock()
	p.conn = conn
	p.lastConfig = &cfgCopy
	p.mu.Unlock()
	p.closed = false
	p.closeErr = nil
	p.closeMu.Unlock()

	p.resetSessionReady()

	if err := p.sendSessionUpdate(ctx, cfgCopy); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session.update failed")
		return fmt.Errorf("openai realtime: session.update: %w", err)
	}
	return nil
}

func resolveOpenAIRealtimeModel(model string) string {
	if trimmed := strings.TrimSpace(model); trimmed != "" {
		return trimmed
	}
	return defaultOpenAIRealtimeModel
}

// realtimeBaseURL honours the LiveConfig.Endpoint override so OpenAI-protocol
// hosts (e.g. Microsoft Foundry's wss://<host>/openai/v1/realtime) can reuse
// this provider unchanged.
func realtimeBaseURL(cfg live.LiveConfig) string {
	if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
		return endpoint
	}
	return openaiRealtimeBaseURL
}

func openAIRealtimeHeaders(apiKey string) http.Header {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+apiKey)
	return header
}

func (p *Provider) dialURL(baseURL, model string) string {
	if p.DialURL != nil {
		return p.DialURL(baseURL, model)
	}
	return fmt.Sprintf("%s?model=%s", baseURL, model)
}

// dialHeaders prefers a host-supplied bearer token source over the static
// API key so OpenAI-protocol hosts with Entra auth (Foundry) work unchanged.
func (p *Provider) dialHeaders(ctx context.Context, cfg live.LiveConfig) (http.Header, error) {
	if p.DialHeaders != nil {
		return p.DialHeaders(ctx, cfg)
	}
	if cfg.BearerToken != nil {
		token, err := cfg.BearerToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("bearer token: %w", err)
		}
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("bearer token: empty token")
		}
		return openAIRealtimeHeaders(token), nil
	}
	return openAIRealtimeHeaders(cfg.APIKey), nil
}

func (p *Provider) buildSession(cfg live.LiveConfig, model, instructions string) map[string]any {
	if p.BuildSession != nil {
		return p.BuildSession(cfg, model, instructions)
	}
	cfg.Model = model
	session := buildOpenAISession(cfg)
	session["instructions"] = instructions
	return session
}

// live.ShouldTryFallback is reused here intentionally —
// the same "primary != fallback, both non-empty" rule applies for OpenAI.
// No re-declaration; relying on package-level visibility.
