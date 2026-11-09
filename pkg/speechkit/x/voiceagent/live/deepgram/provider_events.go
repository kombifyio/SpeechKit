package deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Receive translates the next server frame into a live.LiveMessage. Binary frames
// are agent audio; text frames are JSON control events. Events that don't map
// to live.LiveMessage fields are swallowed and the loop fetches the next frame.
func (p *Provider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	conn := p.snapshotConn()
	if conn == nil {
		return nil, fmt.Errorf("deepgram agent: %w", live.ErrNotConnected)
	}
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			if websocket.CloseStatus(err) != -1 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("deepgram agent: ws read: %w", err)
		}
		if typ == websocket.MessageBinary {
			if len(data) == 0 {
				continue
			}
			return live.NormalizeMessageEvents(&live.LiveMessage{
				EventType: live.LiveEventOutputAudio,
				Audio:     data,
			}, "binary_audio"), nil
		}
		msg, swallow, err := p.parseEvent(data)
		if err != nil {
			return nil, err
		}
		if swallow {
			continue
		}
		return msg, nil
	}
}

// parseEvent translates one Deepgram Voice Agent server event into the kernel's
// live.LiveMessage shape. Returns (nil, true, nil) when the event should be swallowed.
func (p *Provider) parseEvent(data []byte) (*live.LiveMessage, bool, error) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, fmt.Errorf("deepgram agent: decode event: %w", err)
	}
	switch env.Type {
	case "SettingsApplied":
		p.turnMu.Lock()
		p.ready = true
		p.turnMu.Unlock()
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventSessionReady}, env.Type), false, nil
	case "Welcome", "AgentThinking", "AgentStartedSpeaking",
		"PromptUpdated", "SpeakUpdated", "History":
		return nil, true, nil
	case "ThinkUpdated":
		p.turnMu.Lock()
		if p.thinkUpdatePending && p.fallbackReask && !p.fallbackAmbiguous {
			p.fallbackAccepted = true
		}
		p.thinkUpdatePending = false
		p.turnMu.Unlock()
		return nil, true, nil
	case "InjectionRefused":
		return p.injectionRefused()
	case "Warning":
		var ev struct {
			Description string `json:"description"`
		}
		_ = json.Unmarshal(data, &ev)
		p.log().Warn("deepgram agent: server warning", "description", ev.Description)
		return nil, true, nil
	case "UserStartedSpeaking":
		p.invalidateFallbackEvidence()
		// Barge-in: the kernel state machine uses this for interruption.
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInterrupted, Interrupted: true}, env.Type), false, nil
	case "StartOfTurn", "TurnResumed":
		p.invalidateFallbackEvidence()
		// Flux turn events signal fresh user activity. Surface them as
		// interruptions so host UIs can stop playback across both Deepgram Voice
		// Agent and lower-level Flux transports without provider-specific code.
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventInterrupted, Interrupted: true}, env.Type), false, nil
	case "EagerEndOfTurn", "EndOfTurn":
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventTurnEnd}, env.Type), false, nil
	case "ConversationText":
		var ev struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		_ = json.Unmarshal(data, &ev)
		echo, recovered := p.noteTurnText(strings.EqualFold(ev.Role, "user"), ev.Content)
		if echo {
			// The echo of a re-asked turn: clients already show the utterance.
			return nil, true, nil
		}
		if strings.EqualFold(ev.Role, "user") {
			return live.NormalizeMessageEvents(&live.LiveMessage{
				EventType:           live.LiveEventInputFinal,
				InputTranscript:     ev.Content,
				InputTranscriptDone: true,
			}, env.Type), false, nil
		}
		out := &live.LiveMessage{
			EventType:            live.LiveEventOutputText,
			Text:                 ev.Content,
			OutputTranscript:     ev.Content,
			OutputTranscriptDone: true,
		}
		if recovered && strings.EqualFold(ev.Role, "assistant") {
			out.ProviderMetadata = map[string]any{"think_fallback_recovered": true}
		}
		return live.NormalizeMessageEvents(out, env.Type), false, nil
	case "AgentAudioDone":
		// End of the agent's spoken turn.
		return live.NormalizeMessageEvents(&live.LiveMessage{EventType: live.LiveEventTurnEnd, Done: true}, env.Type), false, nil
	case "FunctionCallRequest":
		var ev struct {
			FunctionCallID string          `json:"function_call_id"`
			FunctionName   string          `json:"function_name"`
			Input          json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil, false, fmt.Errorf("deepgram agent: decode function call: %w", err)
		}
		// A tool of this turn may have an effect: the turn is never re-asked.
		p.turnMu.Lock()
		p.turnEffectful = true
		p.turnMu.Unlock()
		args := map[string]any{}
		if len(ev.Input) > 0 {
			if err := json.Unmarshal(ev.Input, &args); err != nil {
				p.log().Warn("deepgram agent: function call input not an object", "name", ev.FunctionName)
			}
		}
		return live.NormalizeMessageEvents(&live.LiveMessage{
			EventType: live.LiveEventToolCall,
			ToolCalls: []live.ToolCall{{
				ID:   ev.FunctionCallID,
				Name: ev.FunctionName,
				Args: args,
			}},
		}, env.Type), false, nil
	case "Error":
		var ev struct {
			Code        string `json:"code"`
			Description string `json:"description"`
			Message     string `json:"message"`
		}
		_ = json.Unmarshal(data, &ev)
		return p.recoverTurn(ev.Code, dgFirst(ev.Description, ev.Message, "unknown"))
	default:
		// Forward-compatible swallow for observability-only events.
		return nil, true, nil
	}
}

// invalidateFallbackEvidence prevents a later answer from being attributed to
// a recovery after fresh user activity. Pending provider updates stay pending.
func (p *Provider) invalidateFallbackEvidence() {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	p.fallbackReask = false
	p.fallbackAccepted = false
	p.fallbackEchoSeen = false
}

// noteTurnText tracks the user turn that still waits for its answer: a final
// user transcript opens it, the agent's reply text closes it. It reports true
// for the echo of a re-asked turn, which the caller swallows so a client never
// shows the utterance twice. recovered is true only for an answer following
// an acknowledged fallback and the echo of the re-asked turn.
func (p *Provider) noteTurnText(user bool, content string) (echo, recovered bool) {
	p.turnMu.Lock()
	defer p.turnMu.Unlock()
	text := strings.TrimSpace(content)
	if user {
		if p.reaskEcho != "" && text == p.reaskEcho {
			p.reaskEcho = ""
			if p.fallbackReask && p.fallbackAccepted && !p.thinkUpdatePending {
				p.fallbackEchoSeen = true
			}
			return true, false
		}
		if text != "" {
			p.pendingUtterance = text
			p.turnAttempts = 0
			p.turnEffectful = false
			p.fallbackReask = false
			p.fallbackAccepted = false
			p.fallbackEchoSeen = false
		}
		return false, false
	}
	recovered = text != "" && p.fallbackReask && p.fallbackAccepted && p.fallbackEchoSeen
	p.clearTurnLocked()
	return false, recovered
}

// clearTurnLocked forgets the pending turn and its recovery. Caller holds turnMu.
func (p *Provider) clearTurnLocked() {
	p.pendingUtterance = ""
	p.turnAttempts = 0
	p.turnEffectful = false
	p.reaskEcho = ""
	p.reasking = false
	p.fallbackReask = false
	p.fallbackAccepted = false
	p.fallbackEchoSeen = false
}

// thinkTurnError is a recoverable turn failure with a public wire code.
type thinkTurnError struct{ code string }

func (e thinkTurnError) Error() string { return "deepgram agent: turn failed" }
func (e thinkTurnError) Code() string  { return e.code }

// deepgramErrorKind sorts a Deepgram Voice Agent Error event.
type deepgramErrorKind int

const (
	// deepgramErrorOther fails the turn without a re-ask.
	deepgramErrorOther deepgramErrorKind = iota
	// deepgramErrorThink is a failure of the think (LLM) leg: the only kind
	// that is re-asked and moved along the think chain.
	deepgramErrorThink
	// deepgramErrorClient is a refused client message (bad settings, an
	// UpdateThink or injection Deepgram rejected): no re-ask.
	deepgramErrorClient
	// deepgramErrorAccount is an account, key or credit failure: the session
	// ends, as no turn can succeed on it.
	deepgramErrorAccount
)

// classifyDeepgramError reads the Error event's code and description.
// Deepgram documents no stable code list for the Voice Agent, so the match is
// on words of both fields, most severe first:
//   - account: auth, unauthorized, forbidden, credit, balance, insufficient,
//     payment, billing, api key
//   - client: unparsable, client_message, invalid_message, settings, invalid
//   - think: think, llm, language model, model, completion, open_ai, openai,
//     anthropic, provider, upstream, timeout, timed out, rate limit,
//     overloaded, unavailable
//
// Anything else is "other": the turn fails without a re-ask.
func classifyDeepgramError(code, description string) deepgramErrorKind {
	text := strings.ToLower(code + " " + description)
	has := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(text, word) {
				return true
			}
		}
		return false
	}
	switch {
	case has("auth", "forbidden", "credit", "balance", "insufficient", "payment", "billing", "api key", "api_key"):
		return deepgramErrorAccount
	case has("unparsable", "client_message", "invalid_message", "settings", "invalid"):
		return deepgramErrorClient
	case has("think", "llm", "language model", "model", "completion", "open_ai", "openai", "anthropic",
		"provider", "upstream", "timeout", "timed out", "rate limit", "rate_limit", "overloaded", "unavailable"):
		return deepgramErrorThink
	default:
		return deepgramErrorOther
	}
}

// recoverTurn answers a Deepgram Error event. Before SettingsApplied it is a
// session failure (the settings were refused), and so is an account or credit
// failure at any time. A think-provider failure of a turn that ran no tool
// is re-asked on the same think model, then on the next model of the think
// chain (UpdateThink); only an exhausted chain reports a recoverable outage
// (AI-HARNESS-CONCEPT-STANDARD §8, answer paths degrade). Any other error,
// a refused UpdateThink and every error after a tool call fail the turn
// without a re-ask: a tool may have had an effect.
func (p *Provider) recoverTurn(code, description string) (*live.LiveMessage, bool, error) {
	kind := classifyDeepgramError(code, description)
	p.turnMu.Lock()
	if !p.ready || kind == deepgramErrorAccount {
		p.turnMu.Unlock()
		return nil, false, fmt.Errorf("deepgram agent: server error %s: %s", code, description)
	}
	refusedUpdate := p.thinkUpdatePending
	if refusedUpdate {
		p.fallbackAmbiguous = true
	}
	p.thinkUpdatePending = false
	utterance := p.pendingUtterance
	candidates := p.thinkCandidates()
	switchThink := false
	retry := utterance != "" && kind == deepgramErrorThink && !p.turnEffectful && !refusedUpdate
	switch {
	case !retry:
	case p.turnAttempts < deepgramThinkRetries:
		p.turnAttempts++
	case p.thinkIndex+1 < len(candidates):
		p.thinkIndex++
		// A fallback model answers once; a failure there moves on again.
		p.turnAttempts = deepgramThinkRetries
		switchThink = true
	default:
		retry = false
	}
	index := p.thinkIndex
	attempt := p.turnAttempts
	effectful := p.turnEffectful
	if retry {
		p.reasking = true
		p.reaskEcho = utterance
		p.fallbackEchoSeen = false
		p.thinkUpdatePending = switchThink
		if switchThink {
			p.fallbackReask = true
			p.fallbackAccepted = false
			p.fallbackEchoSeen = false
		}
	} else {
		p.clearTurnLocked()
	}
	p.turnMu.Unlock()

	selected := candidates[min(index, len(candidates)-1)]
	if retry {
		err := p.reaskTurn(utterance, switchThink, index)
		if err == nil {
			p.log().Warn("deepgram agent: think failed; turn re-asked",
				"code", code, "description", description, "attempt", attempt,
				"think_provider", selected.Provider, "think_model", selected.Model, "think_fallback", switchThink)
			return nil, true, nil
		}
		p.log().Warn("deepgram agent: think recovery could not be sent", "code", code, "error", err)
		p.turnMu.Lock()
		p.clearTurnLocked()
		if p.thinkUpdatePending {
			p.fallbackAmbiguous = true
		}
		p.thinkUpdatePending = false
		p.turnMu.Unlock()
	}
	p.log().Warn("deepgram agent: turn failed without a re-ask",
		"code", code, "description", description, "kind", kind, "after_tool", effectful,
		"refused_update", refusedUpdate, "think_provider", selected.Provider, "think_model", selected.Model)
	return p.turnFailed(), false, nil
}

// injectionRefused answers Deepgram's refusal of an injected message. During
// a recovery the re-asked turn cannot be answered: it fails, non-fatally.
func (p *Provider) injectionRefused() (*live.LiveMessage, bool, error) {
	p.turnMu.Lock()
	reasking := p.reasking
	if reasking {
		p.clearTurnLocked()
		if p.thinkUpdatePending {
			p.fallbackAmbiguous = true
		}
		p.thinkUpdatePending = false
	}
	p.turnMu.Unlock()
	if !reasking {
		return nil, true, nil
	}
	p.log().Warn("deepgram agent: re-asked turn refused by the agent")
	return p.turnFailed(), false, nil
}

// turnFailed is the recoverable outage of a turn: an in-band, non-fatal error.
func (p *Provider) turnFailed() *live.LiveMessage {
	return live.NormalizeMessageEvents(&live.LiveMessage{
		EventType: live.LiveEventTurnFailed,
		Done:      true,
		TurnError: thinkTurnError{code: "provider_failed"},
	}, "Error")
}

// reaskTurn moves the think leg to candidate index when switchThink is set,
// then injects the unanswered user turn again so the agent answers it. Each
// write is bounded like the Settings write.
func (p *Provider) reaskTurn(utterance string, switchThink bool, index int) error {
	conn := p.snapshotConn()
	if conn == nil {
		return live.ErrNotConnected
	}
	write := func(v any) error {
		body, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("deepgram agent: marshal frame: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), deepgramAgentWriteTimeout)
		defer cancel()
		return conn.Write(ctx, websocket.MessageText, body)
	}
	if switchThink {
		p.mu.RLock()
		cfg := live.LiveConfig{}
		if p.lastConfig != nil {
			cfg = *p.lastConfig
		}
		p.mu.RUnlock()
		if err := write(map[string]any{"type": "UpdateThink", "think": p.buildThink(cfg, index)}); err != nil {
			return err
		}
	}
	return write(map[string]any{"type": "InjectUserMessage", "content": utterance})
}
