// Package a2a adapts a registered A2A agent to SpeechKit's cascaded voice
// pipeline. SpeechKit retains STT/TTS custody; the remote endpoint owns agent
// semantics, memory, tools and authorization.
//
// Stability: Experimental — may change in any release.
package a2a

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

const maxErrorBody = 8 * 1024

// RequestContext identifies the exact registered-agent turn for a header
// provider that mints short-lived authorization or a signed delegation.
type RequestContext struct {
	TargetAgentID string
	SessionID     string
	RequestBody   []byte
}

// HeaderProvider supplies per-turn headers. It is deliberately generic: a
// self-hosted deployment may use a bearer key while a hosted connector can
// mint a capability lease and signed delegation without exposing either to
// SpeechKit clients.
type HeaderProvider func(context.Context, RequestContext) (http.Header, error)

// Config describes the registered A2A agent an [Agent] talks to. Endpoint,
// TargetAgentID and SessionID are required.
type Config struct {
	// Endpoint is the agent's A2A JSON-RPC URL. It must use HTTPS; plain
	// HTTP is accepted only for loopback hosts.
	Endpoint string
	// TargetAgentID names the registered agent; it is sent in the request
	// metadata and in every [RequestContext].
	TargetAgentID string
	// SessionID is the A2A contextId every turn is sent under, so the
	// remote agent can keep per-session memory.
	SessionID string
	// HTTPClient sends the turns; nil uses a client with a 60 s timeout.
	HTTPClient *http.Client
	// TurnTimeout bounds the complete request and streamed answer, including
	// custom HTTP clients. Zero uses 60 seconds. Turns are never replayed.
	TurnTimeout time.Duration
	// Headers optionally mints per-turn headers; nil adds none.
	Headers HeaderProvider
}

// Agent implements [cascaded.Agent] by forwarding each utterance to a
// registered A2A agent and returning the text parts of its answer. Build it
// with [New]; it holds no per-turn state and is safe for concurrent use.
type Agent struct {
	endpoint      string
	targetAgentID string
	sessionID     string
	client        *http.Client
	headers       HeaderProvider
	turnTimeout   time.Duration
}

// New validates config and returns an [Agent]. It fails when Endpoint is
// missing or not HTTPS (HTTP is allowed only on loopback), or when
// TargetAgentID or SessionID is blank.
func New(config Config) (*Agent, error) {
	endpoint, err := validateEndpoint(config.Endpoint)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.TargetAgentID) == "" {
		return nil, errors.New("speechkit a2a: target agent id is required")
	}
	if strings.TrimSpace(config.SessionID) == "" {
		return nil, errors.New("speechkit a2a: session id is required")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	// Delegated headers authorize one bound endpoint. A redirect must not
	// forward them to another endpoint, even with a host-injected HTTP client.
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	turnTimeout := config.TurnTimeout
	if turnTimeout <= 0 {
		turnTimeout = 60 * time.Second
	}
	return &Agent{
		endpoint:      endpoint,
		targetAgentID: strings.TrimSpace(config.TargetAgentID),
		sessionID:     strings.TrimSpace(config.SessionID),
		client:        &safeClient,
		headers:       config.Headers,
		turnTimeout:   turnTimeout,
	}, nil
}

// Run sends one user turn as a JSON-RPC "message/stream" request and returns
// the agent's text answer with Action "display". It fails closed: a blank
// utterance, a [HeaderProvider] error (reported before any network call), a
// non-2xx status, a JSON-RPC error object (also inside an SSE stream; a
// [*TurnError] carrying the agent's typed reason), or an answer without text
// parts is an error. Streamed answers concatenate the
// text of every result event.
func (a *Agent) Run(ctx context.Context, input cascaded.AgentInput) (cascaded.AgentOutput, error) {
	return a.Stream(ctx, input, nil)
}

// Stream sends the same authorized turn as Run and emits each answer delta as
// it arrives. Returning an error from emit aborts the request without replay.
func (a *Agent) Stream(ctx context.Context, input cascaded.AgentInput, emit func(string) error) (cascaded.AgentOutput, error) {
	return a.StreamTurn(ctx, input, "", emit)
}

// StreamTurn supplies the host's canonical native final-transcript identity.
// Empty keeps the historical generated request identity. The host must bind
// turnID to one observed final utterance and never replay uncertain execution.
func (a *Agent) StreamTurn(ctx context.Context, input cascaded.AgentInput, turnID string, emit func(string) error) (cascaded.AgentOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, a.turnTimeout)
	defer cancel()
	utterance := strings.TrimSpace(input.Utterance)
	if utterance == "" {
		return cascaded.AgentOutput{}, errors.New("speechkit a2a: utterance is required")
	}
	requestID := "speechkit-" + fmt.Sprint(time.Now().UnixNano())
	if turnID != "" {
		requestID = turnID
	}
	wire := map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"method":  "message/stream",
		"params": map[string]any{
			"message": map[string]any{
				"kind":      "message",
				"role":      "user",
				"messageId": requestID,
				"contextId": a.sessionID,
				"parts":     []map[string]any{{"kind": "text", "text": utterance}},
			},
			"metadata": map[string]any{
				"speechkit": map[string]any{
					"targetAgentId": a.targetAgentID,
					"sessionId":     a.sessionID,
					"locale":        input.Locale,
				},
			},
		},
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return cascaded.AgentOutput{}, fmt.Errorf("speechkit a2a: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint, bytes.NewReader(body))
	if err != nil {
		return cascaded.AgentOutput{}, fmt.Errorf("speechkit a2a: build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream, application/json")
	if a.headers != nil {
		headers, headerErr := a.headers(ctx, RequestContext{TargetAgentID: a.targetAgentID, SessionID: a.sessionID, RequestBody: append([]byte(nil), body...)})
		if headerErr != nil {
			code := "permission_denied"
			var coded cascaded.CodedError
			if errors.As(headerErr, &coded) {
				code = cascaded.SafeFailureCode(coded.Code())
			}
			return cascaded.AgentOutput{}, &TurnError{code: code}
		}
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}
	response, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return cascaded.AgentOutput{}, ctx.Err()
		}
		return cascaded.AgentOutput{}, &TurnError{code: "provider_unavailable"}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var detail any
		_ = json.NewDecoder(io.LimitReader(response.Body, maxErrorBody)).Decode(&detail)
		code := rpcErrorCode(detail)
		if code == "turn_failed" {
			switch response.StatusCode {
			case http.StatusUnauthorized:
				code = "auth_required"
			case http.StatusForbidden:
				code = "permission_denied"
			case http.StatusTooManyRequests:
				code = "rate_limited"
			default:
				code = "provider_failed"
			}
		}
		return cascaded.AgentOutput{}, &TurnError{code: code}
	}

	text, err := readAnswerStream(response, emit)
	if err != nil {
		if ctx.Err() != nil {
			return cascaded.AgentOutput{}, ctx.Err()
		}
		return cascaded.AgentOutput{}, err
	}
	if err := ctx.Err(); err != nil {
		return cascaded.AgentOutput{}, err
	}
	return cascaded.AgentOutput{Text: text, Action: "display"}, nil
}

func validateEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return "", errors.New("speechkit a2a: valid endpoint is required")
	}
	host := strings.ToLower(parsed.Hostname())
	local := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if parsed.Scheme != "https" && (parsed.Scheme != "http" || !local) {
		return "", errors.New("speechkit a2a: endpoint must use HTTPS (HTTP is allowed only on loopback)")
	}
	return parsed.String(), nil
}

func readAnswerStream(response *http.Response, emit func(string) error) (string, error) {
	if strings.Contains(strings.ToLower(response.Header.Get("content-type")), "text/event-stream") {
		return readSSEAnswerStream(response.Body, emit)
	}
	var envelope any
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return "", &TurnError{code: "provider_failed"}
	}
	if rpcError(envelope) {
		return "", &TurnError{code: rpcErrorCode(envelope)}
	}
	if text := strings.TrimSpace(answerText(envelope)); text != "" {
		if len(text) > 128<<10 {
			return "", &TurnError{code: "provider_failed"}
		}
		if emit != nil {
			if err := emit(text); err != nil {
				return "", err
			}
		}
		return text, nil
	}
	return "", errors.New("speechkit a2a: agent returned no text answer")
}

// readSSEAnswerStream joins the text of every streamed result event. Events carry
// token deltas, so whitespace at a delta's edge belongs to the answer and is
// trimmed only from the joined text.
func readSSEAnswerStream(reader io.Reader, emit func(string) error) (string, error) {
	// Bound the whole response, including metadata and ignored events, not
	// only individual lines. An extra byte distinguishes truncation from EOF.
	limited := &io.LimitedReader{R: reader, N: (4 << 20) + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	var answers strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var envelope any
		if json.Unmarshal([]byte(data), &envelope) == nil {
			if rpcError(envelope) {
				return "", &TurnError{code: rpcErrorCode(envelope)}
			}
			if text := answerText(envelope); text != "" {
				if answers.Len()+len(text) > 128<<10 {
					return "", &TurnError{code: "provider_failed"}
				}
				answers.WriteString(text)
				if emit != nil {
					if err := emit(text); err != nil {
						return "", err
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil || limited.N == 0 {
		return "", &TurnError{code: "provider_failed"}
	}
	answer := strings.TrimSpace(answers.String())
	if answer == "" {
		return "", errors.New("speechkit a2a: agent returned no text answer")
	}
	return answer, nil
}

// TurnError is a streamed turn the agent answered with a JSON-RPC error.
// It implements [cascaded.CodedError] with an allowlisted public outcome.
type TurnError struct {
	code string
}

// Code is the safe public outcome.
func (e *TurnError) Code() string { return e.code }

func (e *TurnError) Error() string {
	return cascaded.FailureMessage(e.code)
}

// rpcErrorCode reads the typed reason a JSON-RPC error carries in
// error.data.code; the numeric JSON-RPC code says nothing about why.
func rpcErrorCode(value any) string {
	root, _ := value.(map[string]any)
	errorValue, _ := root["error"].(map[string]any)
	data, _ := errorValue["data"].(map[string]any)
	code, _ := data["code"].(string)
	if code == "" {
		code, _ = errorValue["code"].(string)
	}
	return cascaded.SafeFailureCode(code)
}

var _ cascaded.CodedError = (*TurnError)(nil)

func rpcError(value any) bool {
	root, ok := value.(map[string]any)
	if !ok {
		return false
	}
	errorValue, exists := root["error"]
	return exists && errorValue != nil
}

func answerText(value any) string {
	root, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if rpcError, exists := root["error"]; exists && rpcError != nil {
		return ""
	}
	result, _ := root["result"].(map[string]any)
	if result == nil {
		result = root
	}
	if text := partsText(result["parts"]); text != "" {
		return text
	}
	if artifact, ok := result["artifact"].(map[string]any); ok {
		if text := partsText(artifact["parts"]); text != "" {
			return text
		}
	}
	if status, ok := result["status"].(map[string]any); ok {
		if message, ok := status["message"].(map[string]any); ok {
			return partsText(message["parts"])
		}
	}
	return ""
}

func partsText(value any) string {
	parts, ok := value.([]any)
	if !ok {
		return ""
	}
	var text strings.Builder
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok || part["kind"] != "text" {
			continue
		}
		if value, ok := part["text"].(string); ok {
			text.WriteString(value)
		}
	}
	return text.String()
}

var _ cascaded.Agent = (*Agent)(nil)
