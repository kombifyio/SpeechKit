package agentkit

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// AgentSession wraps a live.Session with a tool registry, lifecycle
// hooks, and a session-scoped memory store. It is the primary entry point
// for callers building voice agents on top of SpeechKit.
//
// Construction order:
//
//  1. Build a LiveProvider (e.g. live_gemini.NewLiveProvider(...))
//  2. Define your Callbacks (OnAudio for playback, OnError, ...)
//  3. Build a ToolRegistry and Register each Tool
//  4. Build LifecycleHooks (optional)
//  5. NewAgentSession(provider, callbacks, registry, hooks, memory)
//  6. Start(ctx, cfg, idleCfg)
//
// AgentSession is safe for concurrent calls to SendAudio / SendText /
// EndAudioStream and the underlying live.Session enforces single-
// session activation.
type AgentSession struct {
	session   *live.Session
	registry  *ToolRegistry
	hooks     LifecycleHooks
	memory    Memory
	sessionID string

	ctxMu  sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc

	bufMu     sync.Mutex
	inputBuf  strings.Builder
	outputBuf strings.Builder
}

// NewAgentSession constructs an AgentSession.
//
// userCallbacks may carry the consumer's own Callbacks (e.g. OnAudio for
// playback). agentkit wraps OnToolCall, OnInputTranscript, OnOutputTranscript
// and OnSessionEnd to drive tool dispatch and lifecycle hooks; the caller's
// originals are still invoked.
//
// If registry is nil, an empty registry is used. If memory is nil, the
// default in-memory store is used.
func NewAgentSession(
	provider LiveProvider,
	userCallbacks Callbacks,
	registry *ToolRegistry,
	hooks LifecycleHooks,
	memory Memory,
) *AgentSession {
	if registry == nil {
		registry = NewRegistry()
	}
	if memory == nil {
		memory = NewInMemory()
	}
	a := &AgentSession{
		registry:  registry,
		hooks:     hooks,
		memory:    memory,
		sessionID: newSessionID(),
	}
	wrapped := wrapCallbacks(a, userCallbacks)
	a.session = live.NewSession(provider, wrapped)
	return a
}

// Start activates the underlying voice-agent session. It injects the
// registry's tool definitions into cfg.Tools (preserving any tools the
// caller already supplied) before forwarding to live.Session.Start.
//
// OnSessionStart fires after a successful Start. If Start returns an error,
// no hook fires.
func (a *AgentSession) Start(ctx context.Context, cfg LiveConfig, idleCfg IdleConfig) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	a.ctxMu.Lock()
	a.ctx = sessionCtx
	a.cancel = cancel
	a.ctxMu.Unlock()

	cfg.Tools = append(cfg.Tools, a.registry.Definitions()...)

	if err := a.session.Start(ctx, cfg, idleCfg); err != nil {
		cancel()
		a.ctxMu.Lock()
		a.ctx = nil
		a.cancel = nil
		a.ctxMu.Unlock()
		return err
	}

	if a.hooks.OnSessionStart != nil {
		a.hooks.OnSessionStart(sessionCtx, a.sc())
	}
	return nil
}

// Stop deactivates the underlying session. Any pending tool dispatches
// receive their context cancellation through the session ctx.
func (a *AgentSession) Stop() {
	a.session.Stop()
	a.ctxMu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.cancel = nil
	a.ctxMu.Unlock()
}

// SendAudio forwards a PCM audio chunk to the realtime model.
func (a *AgentSession) SendAudio(chunk []byte) error { return a.session.SendAudio(chunk) }

// SendText injects a user text turn into the live session.
func (a *AgentSession) SendText(text string) error { return a.session.SendText(text) }

// EndAudioStream signals the end of the current microphone stream.
func (a *AgentSession) EndAudioStream() error { return a.session.EndAudioStream() }

// SessionID returns the unique id assigned to this session.
func (a *AgentSession) SessionID() string { return a.sessionID }

// Registry returns the ToolRegistry bound to this session.
func (a *AgentSession) Registry() *ToolRegistry { return a.registry }

// Memory returns the Memory store bound to this session.
func (a *AgentSession) Memory() Memory { return a.memory }

// State returns the underlying session state.
func (a *AgentSession) State() live.State { return a.session.CurrentState() }

func (a *AgentSession) sc() SessionContext {
	return SessionContext{
		SessionID: a.sessionID,
		Memory:    a.memory,
		Registry:  a.registry,
	}
}

func (a *AgentSession) currentCtx() context.Context {
	a.ctxMu.RLock()
	defer a.ctxMu.RUnlock()
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *AgentSession) dispatchTool(call ToolCall) {
	startDispatch := a.prepareToolDispatch(call)
	startDispatch()
}

// prepareToolDispatch performs the synchronous event-arrival policy phase and
// returns the asynchronous dispatch phase without starting it. Callback
// wrapping uses this split to preserve the caller's raw OnToolCall observer
// ordering while still authorizing before any observer can delay dispatch.
func (a *AgentSession) prepareToolDispatch(call ToolCall) func() {
	ctx := a.currentCtx()
	sc := a.sc()
	tool, ok := a.registry.Lookup(call.Name)
	if !ok {
		return func() {
			go func() {
				if a.hooks.OnToolCall != nil {
					a.hooks.OnToolCall(ctx, sc, call)
				}
				_ = a.session.SendToolResponse(ToolResponse{
					ID:       call.ID,
					Name:     call.Name,
					Response: map[string]any{"error": fmt.Sprintf("unknown tool: %q", call.Name)},
				})
			}()
		}
	}

	// Authorization deliberately runs in the receive callback before any
	// dispatch goroutine is spawned. Hosts can therefore bind policy to the
	// exact event-arrival state (for example, whether local capture is closed)
	// without a scheduler TOCTOU between arrival and Invoke.
	if a.hooks.AuthorizeToolCall != nil {
		args, err := a.hooks.AuthorizeToolCall(ctx, sc, call)
		if err != nil {
			return func() {
				go func() {
					_ = a.session.SendToolResponse(ToolResponse{
						ID:       call.ID,
						Name:     call.Name,
						Response: map[string]any{"error": "tool call denied by host policy"},
					})
				}()
			}
		}
		call.Args = cloneToolArgs(args)
	}

	return func() {
		go func() {
			if a.hooks.OnToolCall != nil {
				observed := call
				observed.Args = cloneToolArgs(call.Args)
				a.hooks.OnToolCall(ctx, sc, observed)
			}

			result, err := tool.Invoke(ctx, cloneToolArgs(call.Args))
			if err != nil {
				result = map[string]any{"error": err.Error()}
			}
			if result == nil {
				result = map[string]any{}
			}
			response := ToolResponse{
				ID:       call.ID,
				Name:     call.Name,
				Response: result,
			}
			if a.hooks.OnToolResult != nil {
				a.hooks.OnToolResult(ctx, sc, call, response)
			}
			_ = a.session.SendToolResponse(response)
		}()
	}
}

func cloneToolArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for key, value := range args {
		out[key] = value
	}
	return out
}

func (a *AgentSession) handleInputTranscript(text string, done bool) {
	if a.hooks.OnUserMessage == nil {
		return
	}
	a.bufMu.Lock()
	a.inputBuf.WriteString(text)
	if !done {
		a.bufMu.Unlock()
		return
	}
	full := a.inputBuf.String()
	a.inputBuf.Reset()
	a.bufMu.Unlock()
	a.hooks.OnUserMessage(a.currentCtx(), a.sc(), full)
}

func (a *AgentSession) handleOutputTranscript(text string, done bool) {
	if a.hooks.OnAgentMessage == nil {
		return
	}
	a.bufMu.Lock()
	a.outputBuf.WriteString(text)
	if !done {
		a.bufMu.Unlock()
		return
	}
	full := a.outputBuf.String()
	a.outputBuf.Reset()
	a.bufMu.Unlock()
	a.hooks.OnAgentMessage(a.currentCtx(), a.sc(), full)
}

func wrapCallbacks(a *AgentSession, user Callbacks) Callbacks {
	out := user

	origToolCall := user.OnToolCall
	out.OnToolCall = func(call ToolCall) {
		startDispatch := a.prepareToolDispatch(call)
		if origToolCall != nil {
			origToolCall(call)
		}
		startDispatch()
	}

	origInput := user.OnInputTranscript
	out.OnInputTranscript = func(text string, done bool) {
		if origInput != nil {
			origInput(text, done)
		}
		a.handleInputTranscript(text, done)
	}

	origOutput := user.OnOutputTranscript
	out.OnOutputTranscript = func(text string, done bool) {
		if origOutput != nil {
			origOutput(text, done)
		}
		a.handleOutputTranscript(text, done)
	}

	origEnd := user.OnSessionEnd
	out.OnSessionEnd = func() {
		if origEnd != nil {
			origEnd()
		}
		if a.hooks.OnSessionEnd != nil {
			a.hooks.OnSessionEnd(a.currentCtx(), a.sc())
		}
	}

	return out
}

func newSessionID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
