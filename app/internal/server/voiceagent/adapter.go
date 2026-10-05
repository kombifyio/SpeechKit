//go:build linux

package voiceagent

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Adapter bridges a live WebSocket conversation to the Framework kernel's
// voiceagent session. One Adapter instance handles exactly one session,
// which matches the manager's concurrency model.
//
// The adapter owns three joined goroutines:
//
//	readPump:  WebSocket → kernel (control + audio frames from client)
//	providerReadPump: kernel → bounded provider-event handoff
//	writePump: handoff → paced WebSocket (audio + transcript + tool-call frames)
//
// When either pump errors or the provider reports session_end, the adapter
// closes the socket, calls OnClose, and returns from Run.
type Adapter struct {
	Session *ManagedSession
	Conn    *websocket.Conn
	// Provider, when set, is used directly (tests pre-inject a fake). In
	// production it is nil and the provider is chosen per session from
	// Providers[StartFrame.Provider || DefaultProvider] after the start frame
	// is read — this is what makes the backend switchable per session.
	Provider        LiveProviderAdapter
	Providers       map[string]ProviderFactory
	DefaultProvider string
	Persona         PersonaResolver
	native          *nativeVoiceSessions
	// MediaBridge starts an optional LiveKit media bridge for sessions that
	// keep this WebSocket as control transport and move audio through LiveKit.
	MediaBridge MediaBridgeFactory
	// IdleTimeout terminates a session whose readPump and writePump have
	// both been silent for the duration. Zero disables the server-side
	// idle watchdog. Defaults to 15 minutes when set by the WS handler.
	IdleTimeout time.Duration
	// MaxDuration terminates a session after this wall-clock duration even
	// when it remains active. Zero disables the hard cap.
	MaxDuration time.Duration
	// ConnectTimeout bounds provider and media readiness. Zero uses 15 seconds.
	ConnectTimeout time.Duration
	// ToolRouter, when non-nil, supplies server-executed tools for this
	// session. Tool names it claims (via Definitions) are executed
	// server-side; all other provider tool calls keep the existing client
	// pass-through wire contract.
	ToolRouter SessionToolRouter
	// OnClose runs after both pumps have returned. Typically removes the
	// session from the manager.
	OnClose func()
	// OnUsage receives the provider-connected duration exactly once. It is
	// registered only after Connect succeeds, so pending tickets and failed
	// provider handshakes are never billed.
	OnUsage func(VoiceUsage)
	Clock   func() time.Time

	writeMu          sync.Mutex
	providerClose    sync.Once
	mediaClose       sync.Once
	closed           atomicBool
	terminal         bool               // guarded by writeMu; a terminal frame seals all downlink
	failureEndReason string             // guarded by writeMu; fatal error determines subsequent terminal reason
	stopAdmission    context.CancelFunc // set before pumps start; fatal writes cancel upstream first
	idle             *idleWatchdog
	flow             *SequenceRunner

	// replyActive tracks whether an agent reply is currently streaming from
	// the provider (set by writePump on downlink audio/transcript, cleared at
	// turn boundaries). A client `cancel` arms suppression only while a reply
	// is actually in flight — a cancel while idle stays a pure ack and must
	// never mute the NEXT reply.
	replyActive atomicBool
	// suppressDownlink drops the CURRENT reply's downlink audio after a
	// client `cancel`; cleared at Done, or at the existing quiet cadence for
	// continuous-duplex providers. Transcript frames keep flowing so the client can still
	// render text for the cancelled reply.
	suppressDownlink atomicBool
	// Continuous-duplex output has no Done event. A cadence gap settles only
	// local cancellation state; it never invents a provider turn boundary.
	continuousDuplex bool      // selected once before pumps start
	replyActivityAt  time.Time // guarded by writeMu; includes paced delivery
	// Provider receipt and playback advance independently. Epochs keep a
	// queued, cancelled tail muted even after its ordered Done is delivered.
	receiveEpoch uint64 // guarded by writeMu; initialized before pumps start
	outputEpoch  uint64 // guarded by writeMu; currently relayed reply
	mutedThrough uint64 // guarded by writeMu; cancelled reply epochs

	// bridgeTools is the set of tool names claimed by ToolRouter for this
	// session, keyed by name. Written once before the pumps start; read-only
	// afterwards, so no lock is needed.
	bridgeTools map[string]ToolDefinitionFrame
	// toolSem bounds concurrent server-side tool executions; toolWG lets Run
	// wait for in-flight executions before tearing the provider down.
	toolSem chan struct{}
	toolWG  sync.WaitGroup

	mediaTransport string
	mediaBridge    MediaBridge
}

// Run blocks until the session ends. The first frame from the client MUST be
// a StartFrame; if it isn't, the adapter closes with an error.
func (a *Adapter) Run(parent context.Context) {
	defer a.closeSocket()
	if a.OnClose != nil {
		defer a.OnClose()
	}

	ctx, cancel := context.WithCancel(parent)
	a.stopAdmission = cancel
	defer cancel()
	endReason := "error"
	var connectedAt time.Time
	// Reserved sessions settle through a trusted final receipt even when
	// provider admission fails. Local sessions meter only successful connects.
	defer func() {
		if a.OnUsage == nil || a.Session == nil || (connectedAt.IsZero() && a.Session.VoiceBudget.ReservationID == "") {
			return
		}
		endedAt := a.now()
		if expiry := a.Session.VoiceBudget.ExpiresAt; expiry > 0 && endedAt.After(time.Unix(expiry, 0)) {
			endedAt = time.Unix(expiry, 0)
		}
		duration := time.Duration(0)
		if !connectedAt.IsZero() && endedAt.After(connectedAt) {
			duration = endedAt.Sub(connectedAt)
		}
		provider := ""
		if a.Provider != nil {
			provider = normalizeProviderName(a.Provider.Name())
		}
		a.OnUsage(VoiceUsage{SessionID: a.Session.ID, AISessionID: a.Session.AISessionID, Provider: provider,
			Duration: duration, OwnerUserID: a.Session.Owner.UserID, OwnerOrgID: a.Session.Owner.OrgID,
			ReservationID: a.Session.VoiceBudget.ReservationID})
	}()
	defer func() {
		a.sendSessionEnd(context.WithoutCancel(parent), endReason)
	}()
	var budgetExpiry time.Time
	if a.Session != nil && a.Session.VoiceBudget.ReservationID != "" {
		budgetExpiry = time.Unix(a.Session.VoiceBudget.ExpiresAt, 0)
		if !time.Now().Before(budgetExpiry) {
			a.sendFatalError(parent, "voice_budget_exhausted", "Reserve new Voice quota before starting voice media.")
			endReason = "voice_budget_exhausted"
			return
		}
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadlineCause(ctx, budgetExpiry, errVoiceBudgetExpired)
		defer deadlineCancel()
	}
	budgetEnded := func() bool { return !budgetExpiry.IsZero() && !time.Now().Before(budgetExpiry) }
	var authorizationExpiry time.Time
	if a.Session != nil && (a.Session.VoiceAgentBinding.TargetAgentID != "" || a.Session.VoiceAgentBinding.DirectEndpoint != nil) {
		var err error
		authorizationExpiry, err = voiceAuthorizationExpiry(LiveConfigFrame{CapabilityLease: a.Session.VoiceAgentBinding.Lease, OboSubjectToken: a.Session.BridgeCredential,
			CredentialExpiresAt: a.Session.VoiceAgentBinding.CredentialExpiresAt, DirectEndpoint: a.Session.VoiceAgentBinding.DirectEndpoint,
			EndpointBinding: a.Session.VoiceAgentBinding.EndpointBinding, EndpointSignature: a.Session.VoiceAgentBinding.EndpointSignature})
		if err != nil || !time.Now().Before(authorizationExpiry) {
			a.sendFatalError(parent, "auth_expired", "Start a new authorized voice session.")
			endReason = "authorization_expired"
			return
		}
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadlineCause(ctx, authorizationExpiry, errAuthorizationExpired)
		defer deadlineCancel()
	}
	authorizationEnded := func() bool {
		return !authorizationExpiry.IsZero() &&
			(errors.Is(context.Cause(ctx), errAuthorizationExpired) || !time.Now().Before(authorizationExpiry))
	}

	start, err := a.waitForStart(ctx)
	if err != nil || ctx.Err() != nil || authorizationEnded() {
		if budgetEnded() {
			endReason = "voice_budget_exhausted"
			a.sendFatalError(parent, "voice_budget_exhausted", "Reserve new Voice quota before starting voice media.")
		} else if authorizationEnded() {
			endReason = "authorization_expired"
			a.sendFatalError(parent, "auth_expired", "Start a new authorized voice session.")
		} else if ctx.Err() != nil {
			endReason = "shutdown"
		} else {
			a.sendFatalError(parent, "start_required", "Send a valid start frame before starting voice media.")
		}
		return
	}
	// Fill provider/persona fields the client omitted from the user's
	// edge-resolved voice preferences captured at session mint. Explicit
	// start-frame values always win; unsatisfiable preferences fall back to
	// the server defaults instead of failing the session.
	personaFromPref := a.applyVoicePrefDefaults(&start)
	// Select the realtime backend for this session. Tests pre-set a.Provider;
	// production picks from the factory map by the client-requested provider
	// (falling back to the server default) so backends are switchable per
	// session without a redeploy.
	if a.Provider == nil {
		requested := start.Provider
		mediaProvider := normalizeProviderName(start.MediaProvider)
		if mediaProvider != "" {
			if a.Session == nil || (a.Session.VoiceAgentBinding.TargetAgentID == "" && a.Session.VoiceAgentBinding.DirectEndpoint == nil) || a.Session.VoiceBudget.ReservationID == "" || (mediaProvider != "assemblyai" && mediaProvider != "deepgram") || a.native == nil {
				a.sendFatalError(ctx, "native_voice_binding_required", "Reserve Voice quota and bind a registered agent before selecting native voice media.")
				return
			}
			requested = mediaProvider
		}
		if a.Session != nil && a.Session.VoiceAgentBinding.DirectEndpoint != nil && mediaProvider == "" {
			a.sendFatalError(ctx, "native_voice_binding_required", "Own endpoint Voice requires the selected native speech provider.")
			return
		}
		provider, resolved, err := a.selectProvider(requested)
		if err != nil {
			a.sendFatalError(ctx, "provider_unavailable", "The selected voice provider is unavailable.")
			return
		}
		a.Provider = provider
		if mediaProvider != "" {
			a.Provider = &registeredNativeProvider{LiveProviderAdapter: provider, sessions: a.native, provider: mediaProvider}
		}
		// Normalise so the persona resolver (which picks the API key + model
		// per provider) and the sequence runner see the resolved backend.
		start.Provider = resolved
	}
	defer a.closeProvider()
	if mode := live.TranscriptionMode(start.TranscriptionMode); !mode.Valid() || (mode != "" && normalizeProviderName(start.Provider) != "assemblyai") {
		a.sendFatalError(ctx, "transcription_mode_unsupported", "The selected speech recognition profile requires AssemblyAI native Voice.")
		return
	}
	transport, err := normalizeMediaTransport(start.MediaTransport)
	if err != nil {
		a.sendFatalError(ctx, "invalid_media_transport", err.Error())
		return
	}
	a.mediaTransport = transport
	if transport == MediaTransportLiveKit {
		if !providerSupportsLiveKitTransport(a.Provider) {
			a.sendFatalError(ctx, "media_transport_unsupported", "media_transport=livekit requires a native realtime PCM provider")
			return
		}
		if a.MediaBridge == nil {
			a.sendFatalError(ctx, "media_transport_unavailable", "LiveKit media bridge is not configured")
			return
		}
	}
	cfg, err := a.resolvePersonaConfig(&start, personaFromPref)
	if err != nil {
		a.sendFatalError(ctx, "persona_unresolved", "The voice persona could not be resolved.")
		return
	}
	cfg.TranscriptionMode = start.TranscriptionMode
	if a.Session != nil {
		binding := a.Session.VoiceAgentBinding
		cfg.AgentTargetID = binding.TargetAgentID
		cfg.AgentEndpoint = binding.Endpoint
		cfg.AgentInstanceAuth = binding.InstanceAuth
		cfg.CapabilityLease = binding.Lease
		cfg.CredentialExpiresAt = binding.CredentialExpiresAt
		cfg.NativeConsent = NativeVoiceConsent{Verified: binding.ConsentVerified, CloudProcessing: binding.CloudProcessing, RecordingAllowed: binding.VoiceAgentRecording, RecordingUpdatedAt: binding.VoiceAgentRecordingUpdatedAt, ExpiresAt: binding.CredentialExpiresAt}
		cfg.VoiceSessionID = a.Session.ID
		cfg.AISessionID = a.Session.AISessionID
		cfg.OwnerUserID = a.Session.Owner.UserID
		cfg.OwnerOrgID = a.Session.Owner.OrgID
		cfg.OwnerPlan = a.Session.Owner.Plan
		cfg.OboSubjectToken = a.Session.BridgeCredential
		cfg.DirectEndpoint = binding.DirectEndpoint
		cfg.EndpointBinding, cfg.EndpointSignature = binding.EndpointBinding, binding.EndpointSignature
		if cfg.DirectEndpoint != nil {
			cfg.SystemPrompt, cfg.RefinementPrompt = "", ""
		}
	}
	// Merge server-executed tool definitions from the tool bridge into the
	// provider config before Connect. Bounded and fail-open to tool-less:
	// a slow or failing bridge never blocks or kills the voice session.
	if cfg.DirectEndpoint == nil {
		a.mergeBridgeTools(ctx, &cfg)
	}
	connectTimeout := a.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 15 * time.Second
	}
	// Cancel the session only if readiness stalls. Stopping this timer after
	// success preserves the context used by provider background work.
	connectTimer := time.AfterFunc(connectTimeout, cancel)
	defer connectTimer.Stop()
	if ctx.Err() != nil {
		if budgetEnded() {
			endReason = "voice_budget_exhausted"
			a.sendFatalError(parent, "voice_budget_exhausted", "Reserve new Voice quota before starting voice media.")
		} else if authorizationEnded() {
			endReason = "authorization_expired"
			a.sendFatalError(parent, "auth_expired", "Start a new authorized voice session.")
		}
		return
	}
	if err := a.Provider.Connect(ctx, cfg); err != nil {
		cancel()
		a.closeProvider()
		if budgetEnded() {
			endReason = "voice_budget_exhausted"
			a.sendFatalError(parent, "voice_budget_exhausted", "Reserve new Voice quota before starting voice media.")
		} else if authorizationEnded() {
			endReason = "authorization_expired"
			a.sendFatalError(parent, "auth_expired", "Start a new authorized voice session.")
		} else if errors.Is(err, errNativeConsentDenied) {
			a.sendFatalError(parent, "voice_consent_required", "Review cloud voice and recording consent in voice settings before starting a new session.")
		} else {
			a.sendFatalError(parent, "provider_connect_failed", "The voice provider could not connect.")
		}
		return
	}
	connectedAt = a.now()
	if transport == MediaTransportLiveKit {
		bridge, err := a.MediaBridge.Start(ctx, MediaBridgeRequest{
			SessionID: a.Session.ID,
			Owner:     a.Session.Owner,
			Provider:  a.Provider,
		})
		if err != nil {
			a.sendFatalError(parent, "media_bridge_failed", "The voice media transport could not connect.")
			return
		}
		a.mediaBridge = bridge
		defer a.closeMediaBridge()
	}
	connectTimer.Stop()
	if ctx.Err() != nil {
		if budgetEnded() {
			endReason = "voice_budget_exhausted"
		} else if authorizationEnded() {
			endReason = "authorization_expired"
		}
		return
	}

	// Report the backend and transport that actually serve this session on
	// the session_ready frame (the start.provider → voice-pref → default
	// precedence already ran above). Clients need this to distinguish e.g.
	// native Deepgram from the cascaded pipeline and to gate `cancel`
	// support on servers that ship it.
	providerName := normalizeProviderName(start.Provider)
	if providerName == "" && a.Provider != nil {
		// Pre-injected provider (tests) with no client-requested name.
		providerName = normalizeProviderName(a.Provider.Name())
	}
	a.sendJSON(ctx, StateFrame{
		Type:             MsgState,
		EventFrameFields: a.eventFrameFields(nil, EventSessionReady),
		State:            "listening",
		Provider:         providerName,
		MediaTransport:   transport,
	})
	if stepResolver, ok := a.Persona.(StepResolver); ok {
		a.flow = NewSequenceRunner(start, cfg, stepResolver)
	} else {
		a.flow = NewSequenceRunner(start, cfg, nil)
	}
	if entered := a.flow.InitialEnteredFrame(); entered != nil {
		a.sendJSON(ctx, *entered)
	}

	a.toolSem = make(chan struct{}, maxConcurrentBridgeToolCalls)
	if duplex, ok := a.Provider.(live.ContinuousDuplexProvider); ok {
		a.continuousDuplex = duplex.ContinuousDuplex()
	}
	a.idle = newIdleWatchdog(a.IdleTimeout)
	defer a.idle.Stop()
	var maxDuration <-chan time.Time
	var maxTimer *time.Timer
	if a.MaxDuration > 0 {
		maxTimer = time.NewTimer(a.MaxDuration)
		maxDuration = maxTimer.C
		defer maxTimer.Stop()
	}

	a.receiveEpoch = 1
	results := make(chan providerReceiveResult, 1)
	done := make(chan struct{}, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); defer a.guardPump("read", done); a.readPump(ctx, done) }()
	go func() { defer wg.Done(); defer a.guardPump("provider", done); a.providerReadPump(ctx, results) }()
	go func() { defer wg.Done(); defer a.guardPump("write", done); a.writePump(ctx, done, results) }()

	select {
	case <-done:
		// One of the pumps returned: a client disconnect, provider EOF,
		// GoAway, or explicit MsgStop. The other pumps exit naturally
		// when ctx cancels below.
		if budgetEnded() {
			endReason = "voice_budget_exhausted"
		} else if authorizationEnded() {
			endReason = "authorization_expired"
		} else if parent.Err() != nil {
			endReason = "shutdown"
		}
	case <-ctx.Done():
		if budgetEnded() {
			endReason = "voice_budget_exhausted"
		} else if authorizationEnded() {
			endReason = "authorization_expired"
		} else {
			endReason = "shutdown"
		}
	case <-a.idle.Fired():
		slog.Info("voiceagent: session idle timeout reached; closing",
			"session_id", a.Session.ID,
			"timeout", a.IdleTimeout,
		)
		endReason = "idle"
	case <-maxDuration:
		slog.Info("voiceagent: session max duration reached; closing",
			"session_id", a.Session.ID,
			"max_duration", a.MaxDuration,
		)
		endReason = "max_duration"
	}
	cancel()
	a.sendSessionEnd(context.WithoutCancel(parent), endReason)
	// Close before joining pumps: providers whose send API has no context
	// parameter can otherwise stay blocked on network writes after cancellation.
	a.closeProvider()
	a.closeMediaBridge()
	a.closeSocket()
	wg.Wait()
	// Wait for in-flight server-side tool executions before the deferred
	// provider Close runs; ctx is cancelled so they abort promptly.
	a.toolWG.Wait()
}

func (a *Adapter) closeProvider() {
	if a.Provider != nil {
		a.providerClose.Do(func() { _ = a.Provider.Close() })
	}
}

func (a *Adapter) closeMediaBridge() {
	if a.mediaBridge != nil {
		a.mediaClose.Do(func() { _ = a.mediaBridge.Close() })
	}
}

func (a *Adapter) now() time.Time {
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}

// guardPump converts a panic in a spawned session pump into a clean session
// teardown. The read/write pumps run in goroutines that the HTTP Recover
// middleware cannot reach, so an unrecovered panic here would crash the whole
// server process instead of ending a single session. Signalling done mirrors
// the pumps' normal exit path so the session shuts down gracefully.
func (a *Adapter) guardPump(name string, done chan<- struct{}) {
	rec := recover()
	if rec == nil {
		return
	}
	slog.Error("voiceagent: session pump panic recovered",
		"session_id", a.Session.ID,
		"pump", name,
		"stack", string(debug.Stack()),
	)
	select {
	case done <- struct{}{}:
	default:
	}
}
