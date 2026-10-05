//go:build linux

package voiceagent

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/app/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/responses"
)

type nativeVoiceSessions struct {
	publicURL     string
	journal       store.VoiceProviderResourceStore
	agents        assemblyai.Agents
	signingSecret func() string
	consentReader NativeConsentReader
	mu            sync.Mutex
	callbacks     map[string]*a2a.OpenAIHandler
}

func newNativeVoiceSessions(publicURL string, storage store.Store, assemblyKey string, signingSecret func() string, consentReader NativeConsentReader) *nativeVoiceSessions {
	journal, _ := storage.(store.VoiceProviderResourceStore)
	return &nativeVoiceSessions{publicURL: publicURL, journal: journal, agents: assemblyai.Agents{APIKey: assemblyKey}, signingSecret: signingSecret, consentReader: consentReader, callbacks: make(map[string]*a2a.OpenAIHandler)}
}

func (s *nativeVoiceSessions) serveCallback(w http.ResponseWriter, r *http.Request, id string) {
	s.mu.Lock()
	handler := s.callbacks[id]
	s.mu.Unlock()
	if handler == nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	handler.ServeHTTP(w, r)
}

// registeredNativeProvider uses vendor media/turn detection while its callback
// dispatches every reasoning turn to the existing signed registered agent.
// Provider tools never replace the agent's tools/approvals or durable history.
type registeredNativeProvider struct {
	LiveProviderAdapter
	sessions  *nativeVoiceSessions
	provider  string
	resource  store.VoiceProviderResource
	sessionID string
	cancel    context.CancelFunc
	turns     *a2a.FinalTurnBinding
	callback  *a2a.OpenAIHandler
	closeOnce sync.Once
	closeErr  error
	mu        sync.Mutex
	closed    bool
	binding   LiveConfigFrame
	// Set before invoking Create. An interrupted invocation stays uncertain;
	// only an intent known not to have crossed this boundary may be discarded.
	createDispatched bool
}

func (p *registeredNativeProvider) HandlesOwnTools() bool { return true }

func (p *registeredNativeProvider) Connect(ctx context.Context, cfg LiveConfigFrame) error {
	if mode := live.TranscriptionMode(cfg.TranscriptionMode); !mode.Valid() || (mode != "" && p.provider != "assemblyai") {
		return errors.New("voiceagent: unsupported native transcription mode")
	}
	secret := ""
	if p.sessions.signingSecret != nil {
		secret = strings.TrimSpace(p.sessions.signingSecret())
	}
	if (cfg.DirectEndpoint == nil && (cfg.AgentTargetID == "" || cfg.CapabilityLease == "")) || cfg.AgentEndpoint == "" || cfg.OboSubjectToken == "" || secret == "" || cfg.VoiceSessionID == "" {
		return errors.New("voiceagent: complete registered native binding is required")
	}
	parsed, err := url.Parse(p.sessions.publicURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("voiceagent: native callback requires configured public HTTPS URL")
	}
	expiresAt, err := voiceAuthorizationExpiry(cfg)
	if err != nil || !time.Now().Before(expiresAt) {
		return errAuthorizationExpired
	}
	ctx, cancel := context.WithDeadline(ctx, expiresAt)
	if err := p.validateConsent(ctx, cfg); err != nil {
		cancel()
		return err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		cancel()
		return live.ErrNotConnected
	}
	p.cancel = cancel
	p.binding = cfg
	p.mu.Unlock()
	var agent a2a.TurnStreamer
	if cfg.DirectEndpoint != nil {
		headers, headerErr := endpointTurnHeaders(cfg, secret)
		if headerErr != nil {
			p.cancel()
			return headerErr
		}
		agent, err = responses.New(responses.Config{URL: cfg.AgentEndpoint, Connection: cfg.DirectEndpoint.Connection,
			Messages: cfg.DirectEndpoint.Messages, Surface: cfg.DirectEndpoint.Surface, Headers: headers})
	} else {
		agent, err = a2a.New(a2a.Config{Endpoint: cfg.AgentEndpoint, TargetAgentID: cfg.AgentTargetID, SessionID: firstNonBlank(cfg.AISessionID, cfg.VoiceSessionID), Headers: registeredAgentHeaders(cfg, secret)})
	}
	if err != nil {
		p.cancel()
		return err
	}
	callbackToken := rand.Text() + rand.Text()
	callback, err := a2a.NewOpenAIHandler(ctx, agent, callbackToken, expiresAt, cfg.Locale)
	if err != nil {
		p.cancel()
		return err
	}
	if cfg.DirectEndpoint != nil {
		callback.Model = cfg.DirectEndpoint.Connection.Model
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		cancel()
		return live.ErrNotConnected
	}
	p.turns = a2a.NewFinalTurnBinding(cfg.VoiceSessionID)
	callback.BindTurn = p.turns.Resolve
	callback.Revalidate = func(turnCtx context.Context) error {
		if err := p.validateConsent(turnCtx, cfg); err != nil {
			cancel()
			_ = p.closeFor(turnCtx)
			return err
		}
		return nil
	}
	p.callback = callback
	callback.FirstResponsePrefix = "Notice: You are speaking with an AI assistant. "
	if strings.HasPrefix(strings.ToLower(cfg.Locale), "de") {
		callback.FirstResponsePrefix = "Hinweis: Du sprichst mit einem KI-Assistenten. "
	}
	p.sessionID = cfg.VoiceSessionID
	p.sessions.mu.Lock()
	p.sessions.callbacks[p.sessionID] = callback
	p.sessions.mu.Unlock()
	p.mu.Unlock()
	// The signed lifetime ends media as well as callback authority. Receive's
	// caller context may outlive the delegated credential.
	context.AfterFunc(ctx, func() { _ = p.closeFor(ctx) })
	go p.watchConsent(ctx, cfg, cancel)
	callbackURL := wssession.WebSocketURL("", "", p.sessions.publicURL, true, "/voiceagent/sessions/"+url.PathEscape(p.sessionID)+"/llm")
	cfg.NativeLLMBaseURL = strings.Replace(callbackURL, "wss://", "https://", 1)
	cfg.NativeLLMToken = callbackToken
	cfg.NativeLLMModel = cfg.AgentTargetID
	if cfg.DirectEndpoint != nil {
		cfg.NativeLLMModel = cfg.DirectEndpoint.Connection.Model
	}
	cfg.Tools = nil
	if p.provider == "assemblyai" {
		if p.sessions.journal == nil {
			_ = p.closeFor(ctx)
			return errors.New("voiceagent: native AssemblyAI requires durable cleanup journal")
		}
		storedConfig := live.LiveConfig{Voice: cfg.Voice, Locale: cfg.Locale, TranscriptionMode: live.TranscriptionMode(cfg.TranscriptionMode), FrameworkPrompt: cfg.SystemPrompt, RefinementPrompt: cfg.RefinementPrompt,
			Policies: live.LivePolicies{ActivityDetection: live.ActivityDetectionPolicy{
				Automatic: cfg.Automatic, StartSensitivity: live.StartSensitivity(strings.ToLower(cfg.StartSensitivity)),
				EndSensitivity: live.EndSensitivity(strings.ToLower(cfg.EndSensitivity)), PrefixPaddingMs: cfg.PrefixPaddingMs,
				SilenceDurationMs: cfg.SilenceDurationMs, ActivityHandling: live.ActivityHandling(strings.ToLower(cfg.ActivityHandling)),
				TurnCoverage: live.TurnCoverage(strings.ToLower(cfg.TurnCoverage)),
			}}}
		stored, err := p.provision(ctx, store.VoiceProviderResource{Name: "speechkit-native-" + rand.Text(), ExpiresAt: expiresAt.Unix()}, storedConfig, assemblyai.CustomLLM{BaseURL: cfg.NativeLLMBaseURL, Model: cfg.NativeLLMModel, APIKey: callbackToken})
		if err != nil {
			_ = p.closeFor(ctx)
			return err
		}
		cfg.StoredAgentID = stored.ID
	}
	if err := p.validateConsent(ctx, cfg); err != nil {
		_ = p.closeFor(ctx)
		return err
	}
	if err := p.LiveProviderAdapter.Connect(ctx, cfg); err != nil {
		_ = p.closeFor(ctx)
		return err
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed || ctx.Err() != nil {
		_ = p.LiveProviderAdapter.Close()
		_ = p.closeFor(ctx)
		return live.ErrNotConnected
	}
	return nil
}

var errNativeConsentDenied = errors.New("voiceagent: current native voice consent is required")

func (p *registeredNativeProvider) validateConsent(ctx context.Context, cfg LiveConfigFrame) error {
	if ctx.Err() != nil {
		return errNativeConsentDenied
	}
	allowed := func(consent NativeVoiceConsent) bool {
		return consent.Verified && consent.CloudProcessing && consent.ExpiresAt > time.Now().Unix() &&
			(p.provider != "assemblyai" || (consent.RecordingAllowed && validRecordingStamp(consent.RecordingUpdatedAt)))
	}
	if !allowed(cfg.NativeConsent) || p.sessions.consentReader == nil {
		return errNativeConsentDenied
	}
	current, err := p.sessions.consentReader(ctx, cfg)
	if ctx.Err() != nil || err != nil || !allowed(current) || (p.provider == "assemblyai" && current.RecordingUpdatedAt != cfg.NativeConsent.RecordingUpdatedAt) {
		return errNativeConsentDenied
	}
	return nil
}

// One poll per admitted session fences idle media as well as authenticated
// callbacks. It is joined by the existing canceled provider lifetime.
func (p *registeredNativeProvider) watchConsent(ctx context.Context, cfg LiveConfigFrame, revoke context.CancelFunc) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if p.validateConsent(ctx, cfg) != nil {
				revoke() // Stops held initialization before bounded cleanup takes its lock.
				_ = p.closeFor(ctx)
				return
			}
		}
	}
}

// A late successful create must preserve a concurrent close's release marker.
// Keep provider I/O outside the mutex so teardown revokes authority immediately.
func (p *registeredNativeProvider) provision(ctx context.Context, resource store.VoiceProviderResource, cfg live.LiveConfig, llm assemblyai.CustomLLM) (assemblyai.StoredAgent, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return assemblyai.StoredAgent{}, live.ErrNotConnected
	}
	err := p.sessions.journal.SaveVoiceProviderResource(ctx, resource)
	if err == nil {
		p.resource = resource
	}
	binding := p.binding
	p.mu.Unlock()
	if err != nil {
		return assemblyai.StoredAgent{}, errors.New("voiceagent: cannot journal native provision intent")
	}
	if err := p.validateConsent(ctx, binding); err != nil {
		return assemblyai.StoredAgent{}, err
	}
	p.mu.Lock()
	if p.closed || ctx.Err() != nil {
		p.mu.Unlock()
		return assemblyai.StoredAgent{}, live.ErrNotConnected
	}
	p.createDispatched = true
	p.mu.Unlock()
	stored, err := p.sessions.agents.Create(ctx, resource.Name, cfg, llm)
	if err != nil {
		return assemblyai.StoredAgent{}, err
	}
	p.mu.Lock()
	p.resource.VendorID = stored.ID
	resource = p.resource // Released is monotonic even when Create ignored cancellation.
	closed := p.closed
	journalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	err = p.sessions.journal.SaveVoiceProviderResource(journalCtx, resource)
	cancel()
	p.mu.Unlock()
	if closed {
		_ = p.deleteResource(ctx, resource)
		return assemblyai.StoredAgent{}, live.ErrNotConnected
	}
	if err != nil {
		return assemblyai.StoredAgent{}, errors.New("voiceagent: cannot journal native provider identity")
	}
	return stored, nil
}

// Native turns are admitted only from final audio transcripts. Text injection
// and host reminders must not manufacture a privileged callback turn.
func (p *registeredNativeProvider) SendText(string) error {
	return errors.New("voiceagent: native registered sessions require audio input")
}

// The stored/custom LLM binding is immutable for this voice session. Persona
// changes must start a newly bound session instead of mutating vendor authority.
func (p *registeredNativeProvider) UpdateInstructions(context.Context, LiveConfigFrame) error {
	return errors.New("voiceagent: native registered instructions are session-bound")
}

func (p *registeredNativeProvider) Close() error {
	return p.closeFor(context.Background())
}

// Close has no context parameter in the provider port. Connect/provision paths
// retain their context values while bounded compensation survives revocation.
func (p *registeredNativeProvider) closeFor(ctx context.Context) error {
	p.closeOnce.Do(func() { p.closeErr = p.close(ctx) })
	return p.closeErr
}

func (p *registeredNativeProvider) close(parent context.Context) error {
	// Revoke the callback before provider cleanup, including unacknowledged
	// creates and failed WS readiness. Vendor outage never preserves authority.
	p.mu.Lock()
	p.closed = true
	if p.cancel != nil {
		p.cancel()
	}
	p.sessions.mu.Lock()
	delete(p.sessions.callbacks, p.sessionID)
	p.sessions.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
	defer cancel()
	p.resource.Released = true
	resource := p.resource
	var journalErr error
	if resource.Name != "" && p.sessions.journal != nil {
		if p.createDispatched {
			journalErr = p.sessions.journal.SaveVoiceProviderResource(ctx, resource)
		} else {
			// The live process can prove Create was never admitted. Restarted
			// or dispatched intents remain conservative, including lost replies.
			journalErr = p.sessions.journal.DeleteVoiceProviderResource(ctx, resource.Name)
		}
	}
	p.mu.Unlock()
	closeErr := p.LiveProviderAdapter.Close()
	if resource.VendorID == "" {
		return errors.Join(closeErr, journalErr)
	}
	deleteErr := p.deleteResource(ctx, resource)
	if journalErr != nil || deleteErr != nil {
		slog.Warn("voiceagent: native resource cleanup pending", "provider", p.provider)
	}
	return errors.Join(closeErr, journalErr, deleteErr)
}

func (p *registeredNativeProvider) deleteResource(parent context.Context, resource store.VoiceProviderResource) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 15*time.Second)
	defer cancel()
	if err := p.sessions.agents.Delete(ctx, resource.VendorID); err != nil {
		return err
	}
	return p.sessions.journal.DeleteVoiceProviderResource(ctx, resource.Name)
}

func (p *registeredNativeProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	message, err := p.LiveProviderAdapter.Receive(ctx)
	if message != nil && message.Interrupted {
		_ = p.CancelResponse()
	}
	if message != nil && message.InputTranscriptDone && p.turns != nil {
		eventID, _ := message.ProviderMetadata["item_id"].(string)
		p.turns.Observe(message.InputTranscript, eventID)
	}
	return message, err
}

// CancelResponse implements the existing tap-to-interrupt port. Vendor native
// barge-in and explicit client cancellation both stop canonical agent effects.
func (p *registeredNativeProvider) CancelResponse() error {
	p.mu.Lock()
	callback := p.callback
	p.mu.Unlock()
	if callback != nil {
		callback.CancelTurn()
	}
	return nil
}

func (p *registeredNativeProvider) ResponsePending() bool {
	p.mu.Lock()
	callback := p.callback
	p.mu.Unlock()
	return callback != nil && callback.ResponsePending()
}

func (p *registeredNativeProvider) SupportsLiveKitTransport() bool {
	return providerSupportsLiveKitTransport(p.LiveProviderAdapter)
}

// StartNativeCleanup resumes pending deletions from the existing durable store.
// It never creates or replays a provider mutation. Active records are left alone
// until close/release or their signed authorization deadline.
func (h *Handler) StartNativeCleanup(ctx context.Context) {
	if h.native.journal == nil || h.native.agents.APIKey == "" {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			h.native.cleanup(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *nativeVoiceSessions) cleanup(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	resources, err := s.journal.ListVoiceProviderResources(ctx)
	if err != nil {
		slog.Warn("voiceagent: native cleanup journal unavailable")
		return
	}
	var agents []assemblyai.StoredAgent
	listed := false
	for _, resource := range resources {
		if ctx.Err() != nil {
			return
		}
		if !resource.Released && resource.ExpiresAt > time.Now().Unix() {
			continue
		}
		id := resource.VendorID
		if id == "" {
			if !listed {
				agents, err = s.agents.List(ctx)
				if err != nil {
					slog.Warn("voiceagent: native cleanup reconciliation pending")
					return
				}
				listed = true
			}
			// Exact opaque correlation only: never touch any foreign agent.
			for _, candidate := range agents {
				if candidate.Name == resource.Name {
					if s.agents.Delete(ctx, candidate.ID) != nil {
						slog.Warn("voiceagent: native cleanup delete pending")
						return
					}
					id = candidate.ID
				}
			}
			// A missing acknowledgement plus an empty list is not proof that
			// a delayed create failed. Retain intent for the next reconciliation.
			if id == "" {
				continue
			}
		} else if s.agents.Delete(ctx, id) != nil {
			slog.Warn("voiceagent: native cleanup delete pending")
			continue
		}
		if s.journal.DeleteVoiceProviderResource(ctx, resource.Name) != nil {
			slog.Warn("voiceagent: native cleanup acknowledgement pending")
		}
	}
}
