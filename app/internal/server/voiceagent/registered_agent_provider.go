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
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
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
	client := agentHTTPClient()
	agent, err := a2a.New(a2a.Config{
		Endpoint:      cfg.AgentEndpoint,
		TargetAgentID: cfg.AgentTargetID,
		SessionID:     firstNonBlank(cfg.AISessionID, cfg.VoiceSessionID),
		HTTPClient:    client,
		Headers:       registeredAgentHeaders(cfg, secret),
		Conversation:  cfg.Conversation,
	})
	if err != nil {
		cancel()
		return err
	}
	deps := p.deps
	// p.deps.Agent, when the host wired one, is the plain voice-agent model: it answers a turn the Companion
	// cannot serve (unavailable, failed), with a spoken notice that the Companion did not answer.
	deps.Agent = &disclosingAgent{inner: agent, fallback: p.deps.Agent, ack: companionAckEnabled()}
	if warm := gatewayPolicyWarmer(cfg.AgentEndpoint, client); warm != nil {
		// The Gateway reads the agent plugin policy before every turn; asking for that read when the session opens and
		// when the user starts speaking has it under way (and the agent host awake) by the time the turn arrives.
		deps.OnSpeechStart = warm
		go warm()
	}
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

// agentHTTPClient is the client one voice session's agent turns use, as a2a would build it. The session's warm-up
// shares it (gatewayPolicyWarmer), so the connection to the Gateway is already open when the first turn is sent.
func agentHTTPClient() *http.Client {
	return netsec.NewClient(netsec.ClientOptions{Timeout: 60 * time.Second})
}

// gatewayPolicyWarmer returns a function that asks the Gateway, at most every ten seconds, to read the policy behind the
// next agent turn ahead of it (the public liveness route with warm=agents). It never blocks and never fails the session;
// nil when the agent endpoint is not an https Gateway address or the warm-up is switched off
// (SPEECHKIT_GATEWAY_WARM=off). It is read-only and carries no credential, so it may run before a turn exists
// (AI-VOICE-SPEECHKIT-TARGET.md "Speculative live dialogue"). Sent through agentClient (the session's agent client), it
// also leaves that client's TLS connection to the Gateway open for the turn, which a cold session otherwise pays on its
// first answer.
func gatewayPolicyWarmer(agentEndpoint string, agentClient *http.Client) func() {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SPEECHKIT_GATEWAY_WARM")), "off") {
		return nil
	}
	parsed, err := url.Parse(agentEndpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil
	}
	target := (&url.URL{Scheme: "https", Host: parsed.Host, Path: "/v1/speechkit/healthz", RawQuery: "warm=agents"}).String()
	client := &http.Client{}
	if agentClient != nil {
		// The agent client's transport, and with it the connection the turn reuses.
		client.Transport = agentClient.Transport
	}
	client.Timeout = 4 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var mu sync.Mutex
	var last time.Time
	return func() {
		mu.Lock()
		if time.Since(last) < 10*time.Second {
			mu.Unlock()
			return
		}
		last = time.Now()
		mu.Unlock()
		request, err := http.NewRequest(http.MethodGet, target, http.NoBody)
		if err != nil {
			return
		}
		response, err := client.Do(request)
		if err != nil {
			return
		}
		// Read to the end so the connection goes back to the pool the turn draws from.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		_ = response.Body.Close()
	}
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
	// fallback answers a turn the Companion could not serve; nil keeps the typed error.
	fallback publiccascaded.Agent
	// ack speaks a short acknowledgement when the Companion has said nothing companionAckDelay into a turn.
	ack  bool
	mu   sync.Mutex
	done bool
}

// companionAckDelay is how long a turn waits for the Companion's first words before it says it is on it.
// The Companion needs 3 to 8 s to its first sentence (model first token 2 to 5 s, run setup 1 to 3 s,
// measured 2026-11-02); silence for that long reads as a device that did not hear.
const companionAckDelay = 1200 * time.Millisecond

// companionAckEnabled keeps the acknowledgement on unless SPEECHKIT_COMPANION_ACK=off.
func companionAckEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("SPEECHKIT_COMPANION_ACK")), "off")
}

func (a *disclosingAgent) Run(ctx context.Context, input publiccascaded.AgentInput) (publiccascaded.AgentOutput, error) {
	output, err := a.inner.Run(ctx, input)
	if err != nil {
		if !a.fallbackServes(ctx, err) {
			return output, err
		}
		output, err = a.runFallback(ctx, input, nil)
		if err != nil {
			return output, err
		}
	}
	if a.takeFirst() {
		output.Text = disclosureFor(input.Locale) + output.Text
	}
	return output, nil
}

// Stream speaks the answer while the agent is still producing it. The AI
// disclosure of the first turn leaves first, before the agent has answered, so
// it is spoken while the agent works instead of delaying the answer. A later
// turn that has heard nothing companionAckDelay after it left speaks a short
// acknowledgement instead; it is not part of the answer or the history.
func (a *disclosingAgent) Stream(ctx context.Context, input publiccascaded.AgentInput, emit func(string) error) (publiccascaded.AgentOutput, error) {
	disclosure := ""
	if a.takeFirst() {
		disclosure = disclosureFor(input.Locale)
	}
	return a.streamTurn(ctx, input, disclosure, a.ack, func(guarded func(string) error) (publiccascaded.AgentOutput, error) {
		return a.inner.Stream(ctx, input, guarded)
	}, emit)
}

// streamTurn runs one Companion turn through run. prefix (the first turn's disclosure) is spoken
// first and leads the returned text; with ack, a turn that has said nothing companionAckDelay after
// it left speaks a short acknowledgement; a turn the Companion cannot serve is answered by the plain
// model after a notice (fallbackServes).
func (a *disclosingAgent) streamTurn(ctx context.Context, input publiccascaded.AgentInput, prefix string, ack bool,
	run func(func(string) error) (publiccascaded.AgentOutput, error), emit func(string) error) (publiccascaded.AgentOutput, error) {
	var (
		mu       sync.Mutex
		spoken   bool // anything was handed to the speaker
		answered bool // the Companion itself said something
		over     bool
	)
	guarded := func(delta string, answer bool) error {
		mu.Lock()
		defer mu.Unlock()
		if over {
			return nil
		}
		if strings.TrimSpace(delta) != "" {
			spoken = true
			answered = answered || answer
		}
		if emit == nil {
			return nil
		}
		return emit(delta)
	}
	if prefix != "" {
		if err := guarded(prefix, false); err != nil {
			return publiccascaded.AgentOutput{}, err
		}
	}
	if ack && emit != nil {
		timer := time.AfterFunc(companionAckDelay, func() {
			mu.Lock()
			defer mu.Unlock()
			if spoken || over {
				return
			}
			spoken = true
			_ = emit(acknowledgementFor(input.Locale))
		})
		defer timer.Stop()
	}
	output, err := run(func(delta string) error { return guarded(delta, true) })
	mu.Lock()
	companionAnswered := answered
	mu.Unlock()
	if err != nil && !companionAnswered && a.fallbackServes(ctx, err) {
		fallback, fallbackErr := a.runFallback(ctx, input, func(delta string) error { return guarded(delta, false) })
		mu.Lock()
		over = true
		mu.Unlock()
		if fallbackErr != nil {
			return output, err
		}
		fallback.Text = prefix + fallback.Text
		return fallback, nil
	}
	if err != nil && !companionAnswered && ctx.Err() == nil && turnOutage(err) {
		// The person may already have heard "One moment."; a turn must never end on it in silence. An
		// outage is answered with a spoken notice as the turn's answer: an error frame would make the
		// adapter discard the queued audio of the notice (and the acknowledgement's tail) unplayed. A
		// limit, right, consent or expiry keeps its typed error, which the client shows as a denial.
		notice := companionFailedFor(input.Locale)
		slog.Warn("voiceagent: companion turn failed; spoken notice instead of an error frame",
			"event", "companion_turn_failed", "code", turnErrorCode(err))
		_ = guarded(notice, false)
		mu.Lock()
		over = true
		mu.Unlock()
		return publiccascaded.AgentOutput{Text: prefix + notice}, nil
	}
	mu.Lock()
	over = true
	mu.Unlock()
	if err != nil {
		return output, err
	}
	output.Text = prefix + output.Text
	return output, nil
}

// nativeCompanionAgent is the Companion behind a native media callback (the Box's Deepgram route):
// the callback speaks the disclosure itself, so this adds the acknowledgement on later turns and the
// plain-model answer with its notice when the Companion is unreachable (disclosingAgent.streamTurn).
type nativeCompanionAgent struct {
	inner  a2a.TurnStreamer
	turns  *disclosingAgent // fallback and ack only; its inner agent is unused
	called sync.Once
	first  bool
}

// ack is false for a vendor that receives the whole answer at once (a delegated function result):
// an acknowledgement would only lead the finished answer.
func newNativeCompanionAgent(inner a2a.TurnStreamer, plain publiccascaded.Agent, ack bool) a2a.TurnStreamer {
	return &nativeCompanionAgent{inner: inner, turns: &disclosingAgent{fallback: plain, ack: ack && companionAckEnabled()}}
}

func (n *nativeCompanionAgent) StreamTurn(ctx context.Context, input publiccascaded.AgentInput, turnID string, emit func(string) error) (publiccascaded.AgentOutput, error) {
	first := false
	n.called.Do(func() { first = true })
	// The first turn opens with the callback's disclosure, which covers the Companion's start.
	return n.turns.streamTurn(ctx, input, "", n.turns.ack && !first, func(guarded func(string) error) (publiccascaded.AgentOutput, error) {
		return n.inner.StreamTurn(ctx, input, turnID, guarded)
	}, emit)
}

// fallbackServes reports whether a failed Companion turn is answered by the plain model: only when the
// Companion was unreachable (provider_unavailable: no connection, or 502/503/504 before any answer), so
// the turn never ran. Every other outcome keeps its typed error: a denial or limit (the plain model
// would answer what the person may not do), a broken or failed run, a timeout or an answer without
// text (the Companion may already have acted, and a second answer would repeat or contradict it).
func (a *disclosingAgent) fallbackServes(ctx context.Context, err error) bool {
	if a.fallback == nil || ctx.Err() != nil {
		return false
	}
	var coded publiccascaded.CodedError
	if !errors.As(err, &coded) {
		return false
	}
	return coded.Code() == "provider_unavailable"
}

// runFallback answers with the plain model after saying that the Companion did not answer, so a
// person never takes the plain model's reply for the Companion's (no memory, no tools).
func (a *disclosingAgent) runFallback(ctx context.Context, input publiccascaded.AgentInput, emit func(string) error) (publiccascaded.AgentOutput, error) {
	slog.Warn("voiceagent: companion turn failed; answering with the plain voice model", "event", "companion_fallback")
	notice := companionUnavailableFor(input.Locale)
	input.SystemPrompt = strings.TrimSpace(input.SystemPrompt + "\n\n" + plainFallbackPrompt)
	if emit != nil {
		if err := emit(notice); err != nil {
			return publiccascaded.AgentOutput{}, err
		}
		if streamer, ok := a.fallback.(publiccascaded.StreamingAgent); ok {
			out, err := streamer.Stream(ctx, input, emit)
			if err != nil {
				return out, err
			}
			out.Text = notice + out.Text
			return out, nil
		}
	}
	out, err := a.fallback.Run(ctx, input)
	if err != nil {
		return out, err
	}
	if emit != nil && strings.TrimSpace(out.Text) != "" {
		if err := emit(out.Text); err != nil {
			return publiccascaded.AgentOutput{}, err
		}
	}
	out.Text = notice + out.Text
	return out, nil
}

// plainFallbackPrompt keeps the plain model honest about what it is: the person already heard that
// the Companion did not answer, and this model has none of the Companion's memory or equipment.
const plainFallbackPrompt = "You answer only because the person's kombify Companion is not reachable right now; " +
	"they have just been told so. You have no access to their memory, account, data, homelab or tools and cannot act " +
	"for them. Answer general questions briefly in the person's language. When the request needs their data or an " +
	"action, say plainly that only the Companion can do that and that they can ask again in a moment."

func acknowledgementFor(locale string) string {
	switch primaryLanguage(locale) {
	case "de":
		return "Einen Moment. "
	case "es":
		return "Un momento. "
	default:
		return "One moment. "
	}
}

// turnErrorCode is the safe typed code of a turn error; an untyped error (an answer without text) is a
// failed turn.
func turnErrorCode(err error) string {
	var coded publiccascaded.CodedError
	if errors.As(err, &coded) {
		return publiccascaded.SafeFailureCode(coded.Code())
	}
	return "turn_failed"
}

// turnOutage reports a failure on the service's side, where trying again helps; denials, limits,
// missing consent and expired sessions are not outages and keep their typed error for the client.
func turnOutage(err error) bool {
	return publiccascaded.FailureClass(turnErrorCode(err)) == publiccascaded.ClassOutage
}

// companionFailedFor closes a turn the Companion could not finish, so it never ends on the acknowledgement.
func companionFailedFor(locale string) string {
	switch primaryLanguage(locale) {
	case "de":
		return "Das hat gerade nicht geklappt. Versuch es bitte gleich noch einmal."
	case "es":
		return "Eso no ha funcionado ahora. Inténtalo de nuevo en un momento."
	default:
		return "That did not work just now. Please try again in a moment."
	}
}

func companionUnavailableFor(locale string) string {
	switch primaryLanguage(locale) {
	case "de":
		return "Der Companion ist gerade nicht erreichbar, ich antworte ohne ihn. "
	case "es":
		return "El Companion no está disponible ahora, respondo sin él. "
	default:
		return "The Companion is not reachable right now, so I am answering without it. "
	}
}

func primaryLanguage(locale string) string {
	tag := strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(tag, "-_"); i >= 0 {
		tag = tag[:i]
	}
	return tag
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
