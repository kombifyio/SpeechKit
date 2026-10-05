// Package responses adapts one explicitly selected own endpoint to SpeechKit's
// existing cascaded/native conversation pipeline. The application owns account,
// consent, funding and current endpoint admission; this adapter never selects a
// model, an agent, a credential or a fallback.
//
// Stability: Experimental
package responses

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// MaxContextBytes bounds the encoded conversation context.
const MaxContextBytes = 128 << 10

// MaxContextMessages bounds the retained conversation length.
const MaxContextMessages = 128

// Message is one turn in the application-owned conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Connection binds the explicitly selected endpoint and model.
type Connection struct {
	Mode       string `json:"mode"`
	EndpointID string `json:"endpointId"`
	Model      string `json:"model"`
}

// Context is the application-owned plain direct chat retained for this voice
// lifetime. ConversationID is a client correlation value, never a grant.
type Context struct {
	Connection     Connection `json:"connection"`
	Messages       []Message  `json:"messages"`
	Surface        string     `json:"surface"`
	ConversationID string     `json:"conversationId"`
}

// Config is immutable for one voice lifetime. Messages are a bounded copy of
// the existing client-local direct chat, not a new durable conversation store.
type Config struct {
	URL        string
	Connection Connection
	Messages   []Message
	Surface    string
	Headers    func(context.Context, []byte) (http.Header, error)
	Client     *http.Client
}

// Agent streams responses for one immutable endpoint binding.
type Agent struct {
	config   Config
	client   http.Client
	mu       sync.Mutex
	context  []Message
	consumed map[string]struct{}
}

// ValidateContext checks the bounded conversation accepted by this adapter.
func ValidateContext(messages []Message) error {
	if len(messages) > MaxContextMessages {
		return errors.New("speechkit responses: context limit exceeded")
	}
	for i, message := range messages {
		if strings.TrimSpace(message.Content) == "" || (message.Role != "user" && message.Role != "assistant" && (message.Role != "system" || i != 0)) {
			return errors.New("speechkit responses: invalid context")
		}
	}
	encoded, err := json.Marshal(messages)
	if err != nil || len(encoded) > MaxContextBytes {
		return errors.New("speechkit responses: context limit exceeded")
	}
	return nil
}

// New constructs an adapter for an explicitly admitted endpoint binding.
func New(config Config) (*Agent, error) {
	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") ||
		(parsed.Scheme == "http" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1") ||
		config.Connection.Mode != "endpoint" || strings.TrimSpace(config.Connection.EndpointID) == "" || strings.TrimSpace(config.Connection.Model) == "" ||
		config.Headers == nil || strings.TrimSpace(config.Surface) == "" {
		return nil, errors.New("speechkit responses: complete endpoint binding required")
	}
	if err := ValidateContext(config.Messages); err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 90 * time.Second}
	if config.Client != nil {
		client = *config.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Agent{config: config, client: client, context: append([]Message(nil), config.Messages...), consumed: make(map[string]struct{})}, nil
}

// Run collects the answer for one cascaded conversation turn.
func (a *Agent) Run(ctx context.Context, input cascaded.AgentInput) (cascaded.AgentOutput, error) {
	return a.Stream(ctx, input, nil)
}

// Stream emits answer fragments for one cascaded conversation turn.
func (a *Agent) Stream(ctx context.Context, input cascaded.AgentInput, emit func(string) error) (cascaded.AgentOutput, error) {
	// The ordinary cascaded host has no native callback identity. A fresh
	// request is safe because this operation never retries an uncertain turn.
	return a.StreamTurn(ctx, input, "speechkit-direct-"+time.Now().Format("20060102T150405.000000000"), emit)
}

// StreamTurn consumes the observed turn identity before its HTTP effect.
func (a *Agent) StreamTurn(ctx context.Context, input cascaded.AgentInput, turnID string, emit func(string) error) (cascaded.AgentOutput, error) {
	if !a.mu.TryLock() {
		return cascaded.AgentOutput{}, errors.New("speechkit responses: turn already running")
	}
	defer a.mu.Unlock()
	if ctx.Err() != nil {
		return cascaded.AgentOutput{}, ctx.Err()
	}
	utterance := strings.TrimSpace(input.Utterance)
	if utterance == "" || turnID == "" || len(turnID) > 256 {
		return cascaded.AgentOutput{}, errors.New("speechkit responses: observed turn required")
	}
	if _, seen := a.consumed[turnID]; seen || len(a.consumed) >= 256 {
		return cascaded.AgentOutput{}, errors.New("speechkit responses: turn already consumed")
	}
	messages := append(append([]Message(nil), a.context...), Message{Role: "user", Content: utterance})
	if err := ValidateContext(messages); err != nil {
		return cascaded.AgentOutput{}, err
	}
	body, err := json.Marshal(struct {
		Stream         bool       `json:"stream"`
		IdempotencyKey string     `json:"idempotencyKey"`
		Surface        string     `json:"surface"`
		Connection     Connection `json:"connection"`
		Messages       []Message  `json:"messages"`
	}{true, turnID, a.config.Surface, a.config.Connection, messages})
	if err != nil {
		return cascaded.AgentOutput{}, err
	}
	headers, err := a.config.Headers(ctx, append([]byte(nil), body...))
	if err != nil {
		return cascaded.AgentOutput{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.URL, bytes.NewReader(body))
	if err != nil {
		return cascaded.AgentOutput{}, err
	}
	req.Header = headers.Clone()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Idempotency-Key", turnID)
	a.consumed[turnID] = struct{}{} // Retain uncertainty before the HTTP effect.
	response, err := a.client.Do(req)
	if err != nil {
		return cascaded.AgentOutput{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return cascaded.AgentOutput{}, &TurnError{reason: "permission_denied"}
	}
	if mediaType := strings.Split(response.Header.Get("Content-Type"), ";")[0]; strings.TrimSpace(mediaType) != "text/event-stream" {
		return cascaded.AgentOutput{}, errors.New("speechkit responses: expected answer stream")
	}
	var answer strings.Builder
	complete := false
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), MaxContextBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var frame struct {
			Type      string `json:"type"`
			Content   string `json:"content"`
			ErrorCode string `json:"error_code"`
			Code      string `json:"code"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &frame) != nil {
			err = errors.New("speechkit responses: invalid answer stream")
			break
		}
		switch frame.Type {
		case "text":
			if answer.Len()+len(frame.Content) > MaxContextBytes {
				err = errors.New("speechkit responses: answer limit exceeded")
				break
			}
			answer.WriteString(frame.Content)
			if emit != nil {
				err = emit(frame.Content)
			}
		case "error":
			err = &TurnError{reason: cascaded.SafeFailureCode(firstNonBlank(frame.ErrorCode, frame.Code))}
		case "done":
			complete = true
		}
		if err != nil || complete {
			break
		}
	}
	if err == nil {
		err = scanner.Err()
	}
	text := answer.String()
	// A stopped partial answer remains part of the same direct conversation,
	// matching its existing typed-chat semantics. Refusals never enter history.
	if text != "" && (complete || ctx.Err() != nil) {
		messages = append(messages, Message{Role: "assistant", Content: text})
		if contextErr := ValidateContext(messages); contextErr != nil {
			return cascaded.AgentOutput{}, contextErr
		}
		a.context = messages
	}
	if err != nil {
		return cascaded.AgentOutput{Text: text, Action: "display"}, err
	}
	if !complete || text == "" {
		return cascaded.AgentOutput{}, errors.New("speechkit responses: incomplete answer")
	}
	return cascaded.AgentOutput{Text: text, Action: "display"}, nil
}

// TurnError reports a denied turn without exposing upstream error details.
type TurnError struct{ reason string }

// Error returns the public denial description.
func (e *TurnError) Error() string { return "speechkit responses: turn denied" }

// Code returns the structured safe failure code.
func (e *TurnError) Code() string { return e.reason }
func firstNonBlank(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
