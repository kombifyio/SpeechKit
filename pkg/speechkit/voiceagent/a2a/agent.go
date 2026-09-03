// Package a2a adapts a registered A2A agent to SpeechKit's cascaded voice
// pipeline. SpeechKit retains STT/TTS custody; the remote endpoint owns agent
// semantics, memory, tools and authorization.
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

type Config struct {
	Endpoint      string
	TargetAgentID string
	SessionID     string
	HTTPClient    *http.Client
	Headers       HeaderProvider
}

type Agent struct {
	endpoint      string
	targetAgentID string
	sessionID     string
	client        *http.Client
	headers       HeaderProvider
}

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
	return &Agent{
		endpoint:      endpoint,
		targetAgentID: strings.TrimSpace(config.TargetAgentID),
		sessionID:     strings.TrimSpace(config.SessionID),
		client:        client,
		headers:       config.Headers,
	}, nil
}

func (a *Agent) Run(ctx context.Context, input cascaded.AgentInput) (cascaded.AgentOutput, error) {
	utterance := strings.TrimSpace(input.Utterance)
	if utterance == "" {
		return cascaded.AgentOutput{}, errors.New("speechkit a2a: utterance is required")
	}
	requestID := "speechkit-" + fmt.Sprint(time.Now().UnixNano())
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
			return cascaded.AgentOutput{}, fmt.Errorf("speechkit a2a: authorize turn: %w", headerErr)
		}
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}
	response, err := a.client.Do(req)
	if err != nil {
		return cascaded.AgentOutput{}, fmt.Errorf("speechkit a2a: send turn: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
		return cascaded.AgentOutput{}, fmt.Errorf("speechkit a2a: turn denied with HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(detail)))
	}

	text, err := readAnswer(response)
	if err != nil {
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
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && local) {
		return "", errors.New("speechkit a2a: endpoint must use HTTPS (HTTP is allowed only on loopback)")
	}
	return parsed.String(), nil
}

func readAnswer(response *http.Response) (string, error) {
	if strings.Contains(strings.ToLower(response.Header.Get("content-type")), "text/event-stream") {
		return readSSEAnswer(response.Body)
	}
	var envelope any
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return "", fmt.Errorf("speechkit a2a: decode response: %w", err)
	}
	if text := answerText(envelope); text != "" {
		return text, nil
	}
	return "", errors.New("speechkit a2a: agent returned no text answer")
}

func readSSEAnswer(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	var answers []string
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
				return "", errors.New("speechkit a2a: agent denied the streamed turn")
			}
			if text := answerText(envelope); text != "" {
				answers = append(answers, text)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("speechkit a2a: read stream: %w", err)
	}
	if len(answers) == 0 {
		return "", errors.New("speechkit a2a: agent returned no text answer")
	}
	return strings.Join(answers, ""), nil
}

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
	return strings.TrimSpace(text.String())
}

var _ cascaded.Agent = (*Agent)(nil)
