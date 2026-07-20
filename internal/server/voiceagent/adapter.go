//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
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

	writeMu sync.Mutex
	closed  atomicBool
	idle    *idleWatchdog
	flow    *SequenceRunner

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
	// Merge server-executed tool definitions from the tool bridge into the
	// provider config before Connect. Bounded and fail-open to tool-less:
	// a slow or failing bridge never blocks or kills the voice session.
	a.mergeBridgeTools(ctx, &cfg)
	if err := a.Provider.Connect(ctx, cfg); err != nil {
		a.sendError(ctx, "provider_connect_failed", err.Error())
		return
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

	a.sendJSON(ctx, StateFrame{
		Type:             MsgState,
		EventFrameFields: eventFrameFields(nil, EventSessionReady),
		State:            "listening",
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
			EventFrameFields: eventFrameFields(nil, EventSessionEnd),
			Reason:           "idle",
		})
	case <-maxDuration:
		slog.Info("voiceagent: session max duration reached; closing",
			"session_id", a.Session.ID,
			"max_duration", a.MaxDuration,
		)
		a.sendJSON(ctx, SessionEndFrame{
			Type:             MsgSessionEnd,
			EventFrameFields: eventFrameFields(nil, EventSessionEnd),
			Reason:           "max_duration",
		})
	}
	cancel()
	wg.Wait()
	// Wait for in-flight server-side tool executions before the deferred
	// provider Close runs; ctx is cancelled so they abort promptly.
	a.toolWG.Wait()
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

// applyVoicePrefDefaults fills the provider and persona a client omitted from
// the start frame with the user's edge-resolved voice preferences captured at
// session-mint time (middleware.VoicePrefsFromContext → ManagedSession).
// Explicit start-frame values always win. Preferences are best-effort per the
// voice-preferences contract: a preferred provider that is not configured on
// this server is skipped (the server default applies) instead of failing the
// session, and the returned personaFromPref flag lets the caller retry
// persona resolution without the preference when the preferred persona does
// not resolve.
func (a *Adapter) applyVoicePrefDefaults(start *StartFrame) (personaFromPref bool) {
	if a.Session == nil || start == nil {
		return false
	}
	prefs := a.Session.VoicePrefs
	// Provider default only matters when production provider selection runs
	// (a.Provider == nil); tests that pre-inject a provider keep their frame.
	if a.Provider == nil && strings.TrimSpace(start.Provider) == "" {
		if pref := normalizeProviderName(prefs.VAProvider); pref != "" {
			if _, ok := a.Providers[pref]; ok {
				start.Provider = pref
			} else {
				slog.Info("voiceagent: preferred provider not configured on this server; using default",
					"preferred_provider", pref,
					"session_id", a.Session.ID,
				)
			}
		}
	}
	if strings.TrimSpace(start.PersonaID) == "" {
		if pref := strings.TrimSpace(prefs.VAPersona); pref != "" {
			start.PersonaID = pref
			personaFromPref = true
		}
	}
	return personaFromPref
}

// resolvePersonaConfig resolves the persona/role config for the session.
// When the persona came from a user preference (not an explicit client
// value) and does not resolve, the adapter retries with the server-default
// persona instead of failing the session — preferences fall back, explicit
// requests still error.
func (a *Adapter) resolvePersonaConfig(start *StartFrame, personaFromPref bool) (LiveConfigFrame, error) {
	cfg, err := a.Persona.Resolve(*start)
	if err != nil && personaFromPref {
		slog.Warn("voiceagent: preferred persona did not resolve; falling back to default persona",
			"preferred_persona", start.PersonaID,
			"session_id", a.Session.ID,
			"err", err,
		)
		start.PersonaID = ""
		cfg, err = a.Persona.Resolve(*start)
	}
	return cfg, err
}

// selectProvider resolves the client-requested provider name (or the server
// default when empty) to a fresh provider instance. Returns the resolved name
// so the caller can normalise the StartFrame for downstream resolution.
func (a *Adapter) selectProvider(requested string) (LiveProviderAdapter, string, error) {
	name := normalizeProviderName(requested)
	if name == "" {
		name = normalizeProviderName(a.DefaultProvider)
	}
	if name == "" {
		return nil, "", errors.New("no voice agent provider requested and no server default configured")
	}
	factory, ok := a.Providers[name]
	if !ok || factory == nil {
		available := make([]string, 0, len(a.Providers))
		for k := range a.Providers {
			available = append(available, k)
		}
		sort.Strings(available)
		return nil, name, errors.New("voice agent provider " + strconv.Quote(name) +
			" is not available on this server; configured: " + strings.Join(available, ", "))
	}
	return factory.NewProvider(), name, nil
}

func normalizeProviderName(provider string) string {
	name := strings.ToLower(strings.TrimSpace(provider))
	name = strings.ReplaceAll(name, "_", "-")
	switch name {
	case "google", "gemini", "gemini-live", "google-live", "realtime.google.gemini-native-audio":
		return "gemini"
	case "deepgram", "deepgram-agent", "deepgram-live", "realtime.deepgram.voice-agent":
		return "deepgram"
	case "assemblyai", "assembly-ai", "assemblyai-agent", "assemblyai-live", "realtime.assemblyai.voice-agent":
		return "assemblyai"
	case "openai", "openai-realtime", "openai-live", "realtime.openai.gpt-realtime-2":
		return "openai"
	case "cascaded", "local-cascaded", "pipeline", "pipeline-fallback", "voice-agent-cascaded", "voice-agent-cascaded-pipeline":
		return "cascaded"
	default:
		return name
	}
}

func (a *Adapter) waitForStart(ctx context.Context) (StartFrame, error) {
	// Tight deadline so clients that never send `start` don't park a slot.
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	typ, data, err := a.Conn.Read(readCtx)
	if err != nil {
		return StartFrame{}, err
	}
	if typ != websocket.MessageText {
		return StartFrame{}, errors.New("first frame must be text 'start'")
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return StartFrame{}, err
	}
	if env.Type != MsgStart {
		return StartFrame{}, errors.New("first frame must be 'start'")
	}
	var start StartFrame
	if err := json.Unmarshal(data, &start); err != nil {
		return StartFrame{}, err
	}
	return start, nil
}

func (a *Adapter) readPump(ctx context.Context, done chan<- struct{}) {
	defer func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	for {
		typ, data, err := a.Conn.Read(ctx)
		if err != nil {
			return
		}
		a.idle.Reset()
		switch typ {
		case websocket.MessageBinary:
			if a.mediaTransport == MediaTransportLiveKit {
				a.sendError(ctx, "audio_transport_mismatch", "binary audio frames are disabled when media_transport=livekit")
				continue
			}
			if err := a.Provider.SendAudio(data); err != nil {
				slog.Warn("voiceagent: send audio failed", "err", err)
				a.sendError(ctx, "audio_upstream_failed", err.Error())
				return
			}
		case websocket.MessageText:
			var env envelope
			if err := json.Unmarshal(data, &env); err != nil {
				a.sendError(ctx, "invalid_frame", err.Error())
				continue
			}
			switch env.Type {
			case MsgAudioEnd:
				if err := a.Provider.SendAudioStreamEnd(); err != nil {
					slog.Warn("voiceagent: audio-end upstream failed", "err", err)
				}
			case MsgText:
				var tx TextFrame
				if err := json.Unmarshal(data, &tx); err == nil {
					if err := a.Provider.SendText(tx.Text); err != nil {
						slog.Warn("voiceagent: text upstream failed", "err", err)
					}
				}
			case MsgToolResponse:
				var tr ToolResponseFrame
				if err := json.Unmarshal(data, &tr); err != nil {
					a.sendError(ctx, "invalid_frame", err.Error())
					continue
				}
				responder, ok := a.Provider.(LiveToolResponder)
				if !ok {
					a.sendError(ctx, "tool_response_unsupported", "provider does not accept tool responses")
					continue
				}
				if err := responder.SendToolResponse(tr); err != nil {
					slog.Warn("voiceagent: tool response upstream failed", "err", err)
					a.sendError(ctx, "tool_response_failed", err.Error())
				}
			case MsgPing:
				a.sendJSON(ctx, PongFrame{Type: MsgPong})
			case MsgStop:
				a.sendJSON(ctx, SessionEndFrame{
					Type:             MsgSessionEnd,
					EventFrameFields: eventFrameFields(nil, EventSessionEnd),
					Reason:           "client",
				})
				return
			case MsgAdvanceStep:
				var advance AdvanceStepFrame
				if err := json.Unmarshal(data, &advance); err != nil {
					a.sendError(ctx, "invalid_frame", err.Error())
					continue
				}
				if advance.Reason == "" {
					advance.Reason = "client"
				}
				if err := a.advanceWorkflowStep(ctx, advance); err != nil {
					slog.Warn("voiceagent: advance_step failed", "err", err)
					a.sendError(ctx, "advance_step_failed", err.Error())
				}
			default:
				// Unknown frames are tolerated (forward-compat); we log
				// and ignore so a newer client doesn't break older servers.
				slog.Debug("voiceagent: unknown client frame", "type", env.Type)
			}
		}
	}
}

func (a *Adapter) writePump(ctx context.Context, done chan<- struct{}) {
	defer func() {
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	for {
		msg, err := a.Provider.Receive(ctx)
		if err != nil {
			if ctx.Err() == nil {
				a.sendError(ctx, "provider_receive_failed", err.Error())
			}
			return
		}
		if msg == nil {
			continue
		}
		// Any provider-emitted message counts as activity for the idle
		// watchdog so a long-running TTS reply doesn't get cut off.
		a.idle.Reset()
		if len(msg.Audio) > 0 {
			if a.mediaBridge != nil {
				if err := a.mediaBridge.SendAudio(msg.Audio); err != nil {
					slog.Warn("voiceagent: send media bridge audio failed", "err", err)
					a.sendError(ctx, "audio_downstream_failed", err.Error())
					return
				}
			} else {
				a.sendBinary(ctx, msg.Audio)
			}
		}
		if msg.InputTranscript != "" {
			eventType := EventInputPartial
			if msg.InputTranscriptDone {
				eventType = EventInputFinal
			}
			a.sendJSON(ctx, TranscriptFrame{
				Type:              MsgInputTranscript,
				EventFrameFields:  eventFrameFields(msg, eventType),
				Text:              msg.InputTranscript,
				Done:              msg.InputTranscriptDone,
				SpeakerLabel:      msg.InputSpeakerLabel,
				PersonID:          msg.InputPersonID,
				DisplayName:       msg.InputDisplayName,
				SpeakerConfidence: msg.InputSpeakerConfidence,
			})
			if msg.InputTranscriptDone {
				a.recordUserTurn(ctx)
			}
		}
		if msg.OutputTranscript != "" {
			a.sendJSON(ctx, TranscriptFrame{
				Type:             MsgOutputTranscript,
				EventFrameFields: eventFrameFields(msg, EventOutputText),
				Text:             msg.OutputTranscript,
				Done:             msg.OutputTranscriptDone,
			})
		}
		for _, call := range msg.ToolCalls {
			if _, claimed := a.bridgeTools[call.Name]; claimed {
				// Server-executed tool: the client still receives the
				// tool_call frame for UI transparency (tagged
				// execution=server) but must not answer it — the adapter
				// resolves it against the tool router asynchronously.
				a.dispatchBridgeToolCall(ctx, msg, call)
				continue
			}
			a.sendJSON(ctx, ToolCallFrame{
				Type:             MsgToolCall,
				EventFrameFields: eventFrameFields(msg, EventToolCall),
				ID:               call.ID,
				Name:             call.Name,
				Args:             call.Args,
			})
		}
		if msg.Interrupted {
			a.sendJSON(ctx, InterruptedFrame{
				Type:             MsgInterrupted,
				EventFrameFields: eventFrameFields(msg, EventInterrupted),
			})
		}
		if msg.GoAway {
			a.sendJSON(ctx, SessionEndFrame{
				Type:             MsgSessionEnd,
				EventFrameFields: eventFrameFields(msg, EventSessionEnd),
				Reason:           "go_away",
			})
			return
		}
		if eventType := standaloneEventType(msg); eventType != "" {
			a.sendJSON(ctx, EventFrame{
				Type:             MsgEvent,
				EventFrameFields: eventFrameFields(msg, eventType),
			})
		}
	}
}

// mergeBridgeTools asks the session tool router for definitions (bounded by
// bridgeDefinitionsTimeout) and merges them into cfg.Tools. Failure or an
// empty manifest degrades to tool-less and surfaces an informational event
// frame; the session always proceeds.
func (a *Adapter) mergeBridgeTools(ctx context.Context, cfg *LiveConfigFrame) {
	if a.ToolRouter == nil {
		return
	}
	defsCtx, cancel := context.WithTimeout(ctx, bridgeDefinitionsTimeout)
	defer cancel()
	defs := a.ToolRouter.Definitions(defsCtx, a.Session, *cfg)
	if len(defs) == 0 {
		fields := eventFrameFields(nil, EventToolBridgeUnavailable)
		fields.ProviderMetadata = map[string]any{"reason": "no_tool_definitions"}
		a.sendJSON(ctx, EventFrame{Type: MsgEvent, EventFrameFields: fields})
		return
	}
	a.bridgeTools = make(map[string]ToolDefinitionFrame, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		def.Name = name
		a.bridgeTools[name] = def
		cfg.Tools = append(cfg.Tools, def)
	}
}

// dispatchBridgeToolCall handles one provider tool call claimed by the tool
// router: emit the transparency tool_call frame, execute asynchronously under
// the per-session concurrency bound, feed the result back to the provider via
// SendToolResponse, and acknowledge to the client with a tool_result_ack
// event frame.
func (a *Adapter) dispatchBridgeToolCall(ctx context.Context, msg *LiveMessage, call ToolCall) {
	fields := eventFrameFields(msg, EventToolCall)
	metadata := make(map[string]any, len(fields.ProviderMetadata)+1)
	for key, value := range fields.ProviderMetadata {
		metadata[key] = value
	}
	metadata["execution"] = "server"
	fields.ProviderMetadata = metadata
	a.sendJSON(ctx, ToolCallFrame{
		Type:             MsgToolCall,
		EventFrameFields: fields,
		ID:               call.ID,
		Name:             call.Name,
		Args:             call.Args,
	})

	select {
	case a.toolSem <- struct{}{}:
	default:
		// Concurrency bound reached: answer immediately with a structured
		// error so the model recovers verbally instead of stalling the turn.
		a.sendBridgeToolResponse(ctx, call, map[string]any{
			"error": map[string]any{
				"code":    "tool_concurrency_exceeded",
				"message": "too many concurrent server-side tool calls in this session; try again",
			},
		}, "error")
		return
	}
	a.toolWG.Add(1)
	go func() {
		defer a.toolWG.Done()
		defer func() { <-a.toolSem }()
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("voiceagent: bridge tool execution panic recovered",
					"session_id", a.Session.ID,
					"tool", call.Name,
					"err", rec,
					"stack", string(debug.Stack()),
				)
			}
		}()
		response, handled := a.ToolRouter.Execute(ctx, a.Session, call)
		status := "ok"
		if !handled || response == nil {
			status = "error"
			response = map[string]any{
				"error": map[string]any{
					"code":    "tool_bridge_unavailable",
					"message": "server-side tool execution failed",
				},
			}
		} else if _, hasError := response["error"]; hasError {
			status = "error"
		}
		a.sendBridgeToolResponse(ctx, call, response, status)
	}()
}

// sendBridgeToolResponse feeds a server-side tool result back to the provider
// and emits the client-facing tool_result_ack event frame carrying id/status.
func (a *Adapter) sendBridgeToolResponse(ctx context.Context, call ToolCall, response map[string]any, status string) {
	responder, ok := a.Provider.(LiveToolResponder)
	if !ok {
		slog.Warn("voiceagent: provider does not accept tool responses; dropping bridge result",
			"session_id", a.Session.ID, "tool", call.Name)
		status = "error"
	} else if err := responder.SendToolResponse(ToolResponseFrame{
		Type:     MsgToolResponse,
		ID:       call.ID,
		Name:     call.Name,
		Response: response,
	}); err != nil {
		slog.Warn("voiceagent: bridge tool response upstream failed",
			"session_id", a.Session.ID, "tool", call.Name, "err", err)
		status = "error"
	}
	fields := eventFrameFields(nil, EventToolResultAck)
	fields.ProviderMetadata = map[string]any{
		"id":        call.ID,
		"name":      call.Name,
		"status":    status,
		"execution": "server",
	}
	a.sendJSON(ctx, EventFrame{Type: MsgEvent, EventFrameFields: fields})
}

func standaloneEventType(msg *LiveMessage) string {
	if msg == nil {
		return ""
	}
	if msg.InputTranscript != "" ||
		msg.OutputTranscript != "" ||
		len(msg.ToolCalls) > 0 ||
		msg.Interrupted ||
		msg.GoAway {
		return ""
	}
	for _, eventType := range inferServerEventTypes(msg) {
		if len(msg.Audio) > 0 && eventType == EventOutputAudio {
			continue
		}
		return eventType
	}
	return ""
}

func eventFrameFields(msg *LiveMessage, primary string) EventFrameFields {
	types := appendEventTypeString(nil, primary)
	for _, eventType := range inferServerEventTypes(msg) {
		types = appendEventTypeString(types, eventType)
	}
	eventType := strings.TrimSpace(primary)
	if eventType == "" && len(types) > 0 {
		eventType = types[0]
	}
	return EventFrameFields{
		EventType:        eventType,
		EventTypes:       types,
		ProviderMetadata: copyProviderMetadata(msg),
	}
}

func inferServerEventTypes(msg *LiveMessage) []string {
	if msg == nil {
		return nil
	}
	var out []string
	out = appendEventTypeString(out, msg.EventType)
	for _, eventType := range msg.EventTypes {
		out = appendEventTypeString(out, eventType)
	}
	if len(out) > 0 {
		return out
	}
	if msg.GoAway {
		out = appendEventTypeString(out, EventSessionEnd)
	}
	if msg.SessionResumable {
		out = appendEventTypeString(out, EventSessionResumable)
	}
	if msg.Interrupted {
		out = appendEventTypeString(out, EventInterrupted)
	}
	if msg.InputTranscript != "" {
		if msg.InputTranscriptDone {
			out = appendEventTypeString(out, EventInputFinal)
		} else {
			out = appendEventTypeString(out, EventInputPartial)
		}
	}
	if len(msg.Audio) > 0 {
		out = appendEventTypeString(out, EventOutputAudio)
	}
	if msg.OutputTranscript != "" {
		out = appendEventTypeString(out, EventOutputText)
	}
	if len(msg.ToolCalls) > 0 {
		out = appendEventTypeString(out, EventToolCall)
	}
	if msg.Done {
		out = appendEventTypeString(out, EventTurnEnd)
	}
	return out
}

func appendEventTypeString(base []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return base
	}
	for _, existing := range base {
		if existing == value {
			return base
		}
	}
	return append(base, value)
}

func copyProviderMetadata(msg *LiveMessage) map[string]any {
	if msg == nil || len(msg.ProviderMetadata) == 0 {
		return nil
	}
	out := make(map[string]any, len(msg.ProviderMetadata))
	for key, value := range msg.ProviderMetadata {
		out[key] = value
	}
	return out
}

func (a *Adapter) recordUserTurn(ctx context.Context) {
	if a.flow == nil || !a.flow.RecordUserTurn() {
		return
	}
	if err := a.advanceWorkflowStep(ctx, AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "max_turns"}); err != nil {
		slog.Warn("voiceagent: max-turn workflow advance failed", "err", err)
		a.sendError(ctx, "advance_step_failed", err.Error())
	}
}

func (a *Adapter) advanceWorkflowStep(ctx context.Context, frame AdvanceStepFrame) error {
	if a.flow == nil || !a.flow.Active() {
		return nil
	}
	transition, err := a.flow.Advance(ctx, frame)
	if err != nil {
		return err
	}
	if transition.Completed != nil {
		a.sendJSON(ctx, *transition.Completed)
	}
	if transition.SequenceCompleted {
		current := a.flow.Current()
		a.sendJSON(ctx, sequenceCompletedFrame(current, frame.Reason))
		return nil
	}
	if transition.Entered != nil {
		if err := a.applyInstructionUpdate(ctx, transition.NextConfig); err != nil {
			slog.Warn("voiceagent: workflow instruction update failed", "err", err)
			a.sendError(ctx, "instruction_update_failed", err.Error())
		}
		a.sendJSON(ctx, *transition.Entered)
	}
	return nil
}

func (a *Adapter) applyInstructionUpdate(ctx context.Context, cfg LiveConfigFrame) error {
	if updater, ok := a.Provider.(LiveInstructionUpdater); ok {
		return updater.UpdateInstructions(ctx, cfg)
	}
	text := RenderHostInstructionUpdate(cfg)
	if text == "" {
		return nil
	}
	return a.Provider.SendText(text)
}

func RenderHostInstructionUpdate(cfg LiveConfigFrame) string {
	var b strings.Builder
	if strings.TrimSpace(cfg.StepID) == "" && strings.TrimSpace(cfg.SystemPrompt) == "" {
		return ""
	}
	b.WriteString("Host instruction update: the active Voice Agent workflow step has changed.")
	if cfg.SequenceID != "" || cfg.StepID != "" {
		b.WriteString("\nSequence: ")
		b.WriteString(cfg.SequenceID)
		b.WriteString("\nStep: ")
		b.WriteString(cfg.StepID)
	}
	if strings.TrimSpace(cfg.StepInstruction) != "" {
		b.WriteString("\nStep instruction:\n")
		b.WriteString(strings.TrimSpace(cfg.StepInstruction))
	}
	if strings.TrimSpace(cfg.StepExitCriteria) != "" {
		b.WriteString("\nExit criteria:\n")
		b.WriteString(strings.TrimSpace(cfg.StepExitCriteria))
	}
	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		b.WriteString("\nComposed behavior prompt:\n")
		b.WriteString(strings.TrimSpace(cfg.SystemPrompt))
	}
	return b.String()
}

// ── helpers ─────────────────────────────────────────────────────────────────

func (a *Adapter) sendJSON(ctx context.Context, v any) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Warn("voiceagent: marshal frame", "err", err)
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.Conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		slog.Debug("voiceagent: write text failed", "err", err)
	}
}

func (a *Adapter) sendBinary(ctx context.Context, data []byte) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := a.Conn.Write(writeCtx, websocket.MessageBinary, data); err != nil {
		slog.Debug("voiceagent: write binary failed", "err", err)
	}
}

func (a *Adapter) sendError(ctx context.Context, code, message string) {
	a.sendJSON(ctx, ErrorFrame{Type: MsgError, Code: code, Message: message})
}

func (a *Adapter) closeSocket(status websocket.StatusCode, reason string) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed.get() {
		return
	}
	a.closed.set(true)
	_ = a.Conn.Close(status, reason)
}

// atomicBool is a tiny wrapper; avoids pulling sync/atomic aliases into every
// test file.
type atomicBool struct {
	mu  sync.Mutex
	val bool
}

func (a *atomicBool) get() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.val
}
func (a *atomicBool) set(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.val = v
}
