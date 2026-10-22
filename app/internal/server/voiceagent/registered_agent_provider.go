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

	publiccascaded "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/voiceagent/a2a"
)

// RegisteredAgentProvider preserves SpeechKit's existing STT/TTS pipeline but
// replaces its local LLM leg with the Gateway-authorized registered A2A agent.
type RegisteredAgentProvider struct {
	deps              CascadedDeps
	signingSecret     func() string
	mu                sync.Mutex
	inner             *CascadedProvider
	expiresAt         time.Time
	cancel            context.CancelFunc
	conversationAgent *a2a.Agent
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
	expiresAt, err := registeredAuthorizationExpiry(cfg.CapabilityLease, cfg.OboSubjectToken, cfg.CredentialExpiresAt)
	if err != nil {
		return err
	}
	if !time.Now().Before(expiresAt) {
		return errAuthorizationExpired
	}
	ctx, cancel := context.WithDeadline(ctx, expiresAt)
	agent, err := a2a.New(a2a.Config{
		Endpoint:      cfg.AgentEndpoint,
		TargetAgentID: cfg.AgentTargetID,
		SessionID:     firstNonBlank(cfg.AISessionID, cfg.VoiceSessionID),
		Headers:       registeredAgentHeaders(cfg, secret),
		Conversation:  cfg.Conversation,
	})
	if err != nil {
		cancel()
		return err
	}
	deps := p.deps
	deps.Agent = &disclosingAgent{inner: agent}
	inner := NewCascadedProvider(deps)
	if err := inner.Connect(ctx, cfg); err != nil {
		cancel()
		_ = inner.Close()
		return err
	}
	p.mu.Lock()
	p.inner = inner
	p.conversationAgent = agent
	p.expiresAt = expiresAt
	p.cancel = cancel
	p.mu.Unlock()
	return nil
}

func registeredAgentHeaders(cfg LiveConfigFrame, secret string) a2a.HeaderProvider {
	return func(_ context.Context, turn a2a.RequestContext) (http.Header, error) {
		now := time.Now().Unix()
		expiresAt, err := registeredAuthorizationExpiry(cfg.CapabilityLease, cfg.OboSubjectToken, cfg.CredentialExpiresAt)
		if err != nil {
			return nil, err
		}
		if now >= expiresAt.Unix() {
			return nil, errAuthorizationExpired
		}
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
			"version": 1, "contract": "kombify.a2a-delegation.v1", "issued_at": now, "expires_at": min(now+300, expiresAt.Unix()),
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
		headers := http.Header{
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
		}
		if cfg.AgentInstanceAuth != "" {
			headers.Set("X-Kombify-Instance-Auth", cfg.AgentInstanceAuth)
			headers.Set("X-Kombify-Instance-Ai-Token", cfg.OboSubjectToken)
		}
		return headers, nil
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
	if a.takeFirst() {
		output.Text = disclosureFor(input.Locale) + output.Text
	}
	return output, nil
}

// Stream speaks the answer while the agent is still producing it. The AI
// disclosure of the first turn leaves first, before the agent has answered, so
// it is spoken while the agent works instead of delaying the answer.
func (a *disclosingAgent) Stream(ctx context.Context, input publiccascaded.AgentInput, emit func(string) error) (publiccascaded.AgentOutput, error) {
	disclosure := ""
	if a.takeFirst() {
		disclosure = disclosureFor(input.Locale)
		if emit != nil {
			if err := emit(disclosure); err != nil {
				return publiccascaded.AgentOutput{}, err
			}
		}
	}
	output, err := a.inner.Stream(ctx, input, emit)
	if err != nil {
		return output, err
	}
	output.Text = disclosure + output.Text
	return output, nil
}

func (a *disclosingAgent) takeFirst() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	first := !a.done
	a.done = true
	return first
}

func disclosureFor(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "de") {
		return "Hinweis: Du sprichst mit einem KI-Assistenten. "
	}
	return "Notice: You are speaking with an AI assistant. "
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
	if !time.Now().Before(p.expiresAt) {
		return nil, errAuthorizationExpired
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
	p.mu.Lock()
	inner := p.inner
	p.inner = nil
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.mu.Unlock()
	if inner == nil {
		return nil
	}
	return inner.Close()
}
func (p *RegisteredAgentProvider) Name() string { return "kombify-agent" }

// HandlesOwnTools reports that the remote agent runs its own tools, so the
// server tool bridge is not used for this provider.
func (p *RegisteredAgentProvider) HandlesOwnTools() bool { return true }

// registeredConversationTarget preserves the catalog-selected hosted command
// surface; the generic control protocol cannot select a target or endpoint.
func registeredConversationTarget(target, endpoint string) bool {
	return target == "workbench-orchestrator" && endpoint == "https://api.kombify.io/a2a/agents/kombify-ai"
}

// LastTurnTiming forwards the registered agent's own phase durations so the
// turn's closing message can carry them.
func (a *disclosingAgent) LastTurnTiming() map[string]int64 { return a.inner.LastTurnTiming() }
