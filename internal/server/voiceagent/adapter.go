//go:build linux

package voiceagent

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Adapter bridges a live WebSocket conversation to the Framework kernel's
// voiceagent session. One Adapter instance handles exactly one session,
// which matches the manager's concurrency model.
//
// The adapter owns two long-lived goroutines:
//
//	readPump:  WebSocket → kernel (control + audio frames from client)
//	writePump: kernel → WebSocket (audio + transcript + tool-call frames)
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

	writeMu sync.Mutex
	closed  atomicBool
	idle    *idleWatchdog
	flow    *SequenceRunner

	// replyActive tracks whether an agent reply is currently streaming from
	// the provider (set by writePump on downlink audio/transcript, cleared at
	// turn boundaries). A client `cancel` arms suppression only while a reply
	// is actually in flight — a cancel while idle stays a pure ack and must
	// never mute the NEXT reply.
	replyActive atomicBool
	// suppressDownlink drops the CURRENT reply's downlink audio after a
	// client `cancel`; cleared at the provider's turn boundary (Done or
	// Interrupted). Transcript frames keep flowing so the client can still
	// render text for the cancelled reply.
	suppressDownlink atomicBool

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
	defer a.closeSocket(websocket.StatusNormalClosure, "done")
	if a.OnClose != nil {
		defer a.OnClose()
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	start, err := a.waitForStart(ctx)
	if err != nil {
		a.sendError(ctx, "start_required", err.Error())
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
		provider, resolved, err := a.selectProvider(start.Provider)
		if err != nil {
			a.sendError(ctx, "provider_unavailable", err.Error())
			return
		}
		a.Provider = provider
		// Normalise so the persona resolver (which picks the API key + model
		// per provider) and the sequence runner see the resolved backend.
		start.Provider = resolved
	}
	transport, err := normalizeMediaTransport(start.MediaTransport)
	if err != nil {
		a.sendError(ctx, "invalid_media_transport", err.Error())
		return
	}
	a.mediaTransport = transport
	if transport == MediaTransportLiveKit {
		if !providerSupportsLiveKitTransport(a.Provider) {
			a.sendError(ctx, "media_transport_unsupported", "media_transport=livekit requires a native realtime PCM provider")
			return
		}
		if a.MediaBridge == nil {
			a.sendError(ctx, "media_transport_unavailable", "LiveKit media bridge is not configured")
			return
		}
	}
	cfg, err := a.resolvePersonaConfig(&start, personaFromPref)
	if err != nil {
		a.sendError(ctx, "persona_unresolved", err.Error())
		return
	}
	if a.Session != nil {
		binding := a.Session.VoiceAgentBinding
		cfg.AgentTargetID = binding.TargetAgentID
		cfg.AgentEndpoint = binding.Endpoint
		cfg.CapabilityLease = binding.Lease
		cfg.VoiceSessionID = a.Session.ID
		cfg.AISessionID = a.Session.AISessionID
		cfg.OwnerUserID = a.Session.Owner.UserID
		cfg.OwnerOrgID = a.Session.Owner.OrgID
		cfg.OwnerPlan = a.Session.Owner.Plan
		cfg.OboSubjectToken = a.Session.BridgeCredential
	}
	// Merge server-executed tool definitions from the tool bridge into the
	// provider config before Connect. Bounded and fail-open to tool-less:
	// a slow or failing bridge never blocks or kills the voice session.
	a.mergeBridgeTools(ctx, &cfg)
	if err := a.Provider.Connect(ctx, cfg); err != nil {
		a.sendError(ctx, "provider_connect_failed", err.Error())
		return
	}
	connectedAt := a.now()
	if a.OnUsage != nil {
		meteredProvider := normalizeProviderName(start.Provider)
		if meteredProvider == "" && a.Provider != nil {
			meteredProvider = normalizeProviderName(a.Provider.Name())
		}
		sessionID, aiSessionID := "", ""
		if a.Session != nil {
			sessionID = a.Session.ID
			aiSessionID = a.Session.AISessionID
		}
		defer func() {
			a.OnUsage(VoiceUsage{
				SessionID:   sessionID,
				AISessionID: aiSessionID,
				Provider:    meteredProvider,
				Duration:    a.now().Sub(connectedAt),
			})
		}()
	}
	defer func() {
		if err := a.Provider.Close(); err != nil {
			slog.Debug("voiceagent: provider close", "err", err)
		}
	}()
	if transport == MediaTransportLiveKit {
		bridge, err := a.MediaBridge.Start(ctx, MediaBridgeRequest{
			SessionID: a.Session.ID,
			Owner:     a.Session.Owner,
			Provider:  a.Provider,
		})
		if err != nil {
			a.sendError(ctx, "media_bridge_failed", err.Error())
			return
		}
		a.mediaBridge = bridge
		defer func() {
			if err := bridge.Close(); err != nil {
				slog.Debug("voiceagent: media bridge close", "err", err)
			}
		}()
	}

	// Report the backend and transport that actually serve this session on
	// the session_ready frame (the start.provider → voice-pref → default
	// precedence already ran above). Clients need this to distinguish e.g.
	// native Deepgram from the cascaded pipeline and to gate `cancel`
	// support on servers that ship it (kombify-SpeechKit-aajy).
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
	a.idle = newIdleWatchdog(a.IdleTimeout)
	defer a.idle.Stop()
	var maxDuration <-chan time.Time
	var maxTimer *time.Timer
	if a.MaxDuration > 0 {
		maxTimer = time.NewTimer(a.MaxDuration)
		maxDuration = maxTimer.C
		defer maxTimer.Stop()
	}

	done := make(chan struct{}, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); defer a.guardPump("read", done); a.readPump(ctx, done) }()
	go func() { defer wg.Done(); defer a.guardPump("write", done); a.writePump(ctx, done) }()

	select {
	case <-done:
		// One of the pumps returned: a client disconnect, provider EOF,
		// GoAway, or explicit MsgStop. The other pump exits naturally
		// when ctx cancels below.
	case <-a.idle.Fired():
		slog.Info("voiceagent: session idle timeout reached; closing",
			"session_id", a.Session.ID,
			"timeout", a.IdleTimeout,
		)
		a.sendJSON(ctx, SessionEndFrame{
			Type:             MsgSessionEnd,
			EventFrameFields: a.eventFrameFields(nil, EventSessionEnd),
			Reason:           "idle",
		})
	case <-maxDuration:
		slog.Info("voiceagent: session max duration reached; closing",
			"session_id", a.Session.ID,
			"max_duration", a.MaxDuration,
		)
		a.sendJSON(ctx, SessionEndFrame{
			Type:             MsgSessionEnd,
			EventFrameFields: a.eventFrameFields(nil, EventSessionEnd),
			Reason:           "max_duration",
		})
	}
	cancel()
	wg.Wait()
	// Wait for in-flight server-side tool executions before the deferred
	// provider Close runs; ctx is cancelled so they abort promptly.
	a.toolWG.Wait()
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
		"err", rec,
		"stack", string(debug.Stack()),
	)
	select {
	case done <- struct{}{}:
	default:
	}
}
