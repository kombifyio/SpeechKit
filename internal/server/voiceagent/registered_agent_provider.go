//go:build linux

package voiceagent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
	publiccascaded "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// RegisteredAgentProvider preserves SpeechKit's existing STT/TTS pipeline but
// replaces its local LLM leg with the Gateway-authorized registered A2A agent.
type RegisteredAgentProvider struct {
	deps          CascadedDeps
	signingSecret func() string
	mu            sync.Mutex
	inner         *CascadedProvider
}

func NewRegisteredAgentProvider(deps CascadedDeps, signingSecret func() string) *RegisteredAgentProvider {
	return &RegisteredAgentProvider{deps: deps, signingSecret: signingSecret}
}

func (p *RegisteredAgentProvider) Connect(ctx context.Context, cfg LiveConfigFrame) error {
	if strings.TrimSpace(cfg.AgentTargetID) == "" || strings.TrimSpace(cfg.AgentEndpoint) == "" || strings.TrimSpace(cfg.CapabilityLease) == "" {
		return errors.New("voiceagent: registered agent binding is required")
	}
	if strings.TrimSpace(cfg.OboSubjectToken) == "" {
		return errors.New("voiceagent: registered agent delegated subject token is required")
	}
	secret := ""
	if p.signingSecret != nil {
		secret = strings.TrimSpace(p.signingSecret())
	}
	if secret == "" {
		return errors.New("voiceagent: A2A delegation signer is unavailable")
	}
	agent, err := a2a.New(a2a.Config{
		Endpoint:      cfg.AgentEndpoint,
		TargetAgentID: cfg.AgentTargetID,
		SessionID:     firstNonBlank(cfg.AISessionID, cfg.VoiceSessionID),
		Headers:       registeredAgentHeaders(cfg, secret),
	})
	if err != nil {
		return err
	}
	deps := p.deps
	deps.Agent = &disclosingAgent{inner: agent}
	inner := NewCascadedProvider(deps)
	if err := inner.Connect(ctx, cfg); err != nil {
		return err
	}
	p.mu.Lock()
	p.inner = inner
	p.mu.Unlock()
	return nil
}

func registeredAgentHeaders(cfg LiveConfigFrame, secret string) a2a.HeaderProvider {
	return func(_ context.Context, turn a2a.RequestContext) (http.Header, error) {
		now := time.Now().Unix()
		parsed, err := url.Parse(cfg.AgentEndpoint)
		if err != nil {
			return nil, err
		}
		binding := sha256.Sum256([]byte("POST\n" + parsed.Path + "\n" + string(turn.RequestBody)))
		runID := fmt.Sprintf("speechkit-%d", time.Now().UnixNano())
		capabilities, err := leaseCapabilities(cfg.CapabilityLease)
		if err != nil {
			return nil, err
		}
		delegation := map[string]any{
			"version": 1, "contract": "kombify.a2a-delegation.v1", "issued_at": now, "expires_at": now + 300,
			"source_agent": "speechkit-server", "delegated_agent_id": cfg.AgentTargetID,
			"trace_id": runID, "session_id": turn.SessionID, "run_id": runID,
			"subject": cfg.OwnerUserID, "org_id": cfg.OwnerOrgID, "actor_type": "user",
			"scopes": []string{"openid"}, "entitlements": capabilities, "tier": cfg.OwnerPlan,
			"act_sub": nil, "act_claim_available": false,
			"request_binding": base64.RawURLEncoding.EncodeToString(binding[:]),
			"policy":          map[string]string{"decision": "allowed"},
		}
		payload, err := json.Marshal(delegation)
		if err != nil {
			return nil, err
		}
		encoded := base64.RawURLEncoding.EncodeToString(payload)
		timestamp := fmt.Sprint(now)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte("v1\nprimary\n" + timestamp + "\n" + encoded))
		return http.Header{
			"Authorization":                      {"Bearer " + cfg.OboSubjectToken},
			"X-Kombify-Capability-Lease":         {cfg.CapabilityLease},
			"X-Kombify-A2a-Delegation-Context":   {encoded},
			"X-Kombify-A2a-Delegation-Key-Id":    {"primary"},
			"X-Kombify-A2a-Delegation-Timestamp": {timestamp},
			"X-Kombify-A2a-Delegation-Signature": {"v1=" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))},
			"X-Kombify-Trace-Id":                 {runID},
			"X-Kombify-Session-Id":               {turn.SessionID},
			"X-Kombify-Run-Id":                   {runID},
			"X-Kombify-Agent-Id":                 {"speechkit-server"},
		}, nil
	}
}

func leaseCapabilities(token string) ([]string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("voiceagent: invalid bound capability lease")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("voiceagent: invalid bound capability lease")
	}
	var claims struct {
		Capabilities []string `json:"capabilities"`
	}
	if json.Unmarshal(payload, &claims) != nil || len(claims.Capabilities) == 0 {
		return nil, errors.New("voiceagent: invalid bound capability lease")
	}
	return claims.Capabilities, nil
}

type disclosingAgent struct {
	inner *a2a.Agent
	mu    sync.Mutex
	done  bool
}

func (a *disclosingAgent) Run(ctx context.Context, input publiccascaded.AgentInput) (publiccascaded.AgentOutput, error) {
	output, err := a.inner.Run(ctx, input)
	if err != nil {
		return output, err
	}
	a.mu.Lock()
	first := !a.done
	a.done = true
	a.mu.Unlock()
	if first {
		disclosure := "Notice: You are speaking with an AI assistant. "
		if strings.HasPrefix(strings.ToLower(input.Locale), "de") {
			disclosure = "Hinweis: Du sprichst mit einem KI-Assistenten. "
		}
		output.Text = disclosure + output.Text
	}
	return output, nil
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (p *RegisteredAgentProvider) current() (*CascadedProvider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inner == nil {
		return nil, errors.New("voiceagent: registered agent provider is not connected")
	}
	return p.inner, nil
}

func (p *RegisteredAgentProvider) SendAudio(value []byte) error {
	inner, err := p.current()
	if err != nil {
		return err
	}
	return inner.SendAudio(value)
}
func (p *RegisteredAgentProvider) SendAudioStreamEnd() error {
	inner, err := p.current()
	if err != nil {
		return err
	}
	return inner.SendAudioStreamEnd()
}
func (p *RegisteredAgentProvider) SendText(value string) error {
	inner, err := p.current()
	if err != nil {
		return err
	}
	return inner.SendText(value)
}
func (p *RegisteredAgentProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	inner, err := p.current()
	if err != nil {
		return nil, err
	}
	return inner.Receive(ctx)
}
func (p *RegisteredAgentProvider) Close() error {
	inner, err := p.current()
	if err != nil {
		return nil
	}
	return inner.Close()
}
func (p *RegisteredAgentProvider) Name() string { return "kombify-agent" }
