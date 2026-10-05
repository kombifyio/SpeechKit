package a2a

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// OpenAIHandler adapts a native voice provider's streamed chat-completions
// callback to one fixed registered Agent. Token is a session-scoped callback
// credential, never an owner login or provider key. The host owns its expiry
// and removes this handler on voice teardown. Vendor prompts, model selection,
// history and tools cannot change the bound agent or its durable session.
type OpenAIHandler struct {
	Agent     TurnStreamer
	Model     string
	Token     string `json:"-"`
	ExpiresAt time.Time
	Locale    string
	// Context is the owning voice session. Its cancellation stops callbacks.
	Context context.Context
	// BindTurn resolves a callback against a host-observed native final
	// transcript. When set, vendor history cannot create a new turn identity.
	BindTurn func(context.Context, string) (string, error)
	// Revalidate checks current host authority on every authenticated callback,
	// before parsing, correlation or replay admission. Denial must revoke media.
	Revalidate func(context.Context) error
	// FirstResponsePrefix is trusted host disclosure, spoken once before the
	// first successful answer starts. It is never supplied by the vendor.
	FirstResponsePrefix string
	disclosed           bool
	// admit serializes turns without allowing an unbounded callback queue.
	admit      chan struct{}
	seen       map[[32]byte]struct{}
	turnMu     sync.Mutex
	turnCancel context.CancelFunc
}

// TurnStreamer is the existing host-observed native turn boundary. An endpoint
// adapter can reuse the callback lifecycle without inventing an agent identity.
type TurnStreamer interface {
	StreamTurn(context.Context, cascaded.AgentInput, string, func(string) error) (cascaded.AgentOutput, error)
}

// CancelTurn stops the current registered-agent request without revoking the
// voice session. Its admitted identity remains consumed, including uncertainty.
func (h *OpenAIHandler) CancelTurn() {
	h.turnMu.Lock()
	cancel := h.turnCancel
	h.turnMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ResponsePending includes admission/correlation before the first answer delta.
func (h *OpenAIHandler) ResponsePending() bool {
	h.turnMu.Lock()
	defer h.turnMu.Unlock()
	return h.turnCancel != nil
}

// NewOpenAIHandler requires a bound agent, an opaque credential and a future
// expiry. The caller must generate at least 32 random bytes for token.
func NewOpenAIHandler(ctx context.Context, agent TurnStreamer, token string, expiresAt time.Time, locale string) (*OpenAIHandler, error) {
	if ctx == nil || agent == nil || (reflect.ValueOf(agent).Kind() == reflect.Pointer && reflect.ValueOf(agent).IsNil()) || len(token) < 32 || !time.Now().Before(expiresAt) {
		return nil, errors.New("speechkit a2a: complete callback binding is required")
	}
	model := ""
	if registered, ok := agent.(*Agent); ok {
		model = registered.targetAgentID
	}
	return &OpenAIHandler{Agent: agent, Model: model, Token: token, ExpiresAt: expiresAt, Locale: locale, Context: ctx, admit: make(chan struct{}, 1), seen: make(map[[32]byte]struct{})}, nil
}

func (h *OpenAIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	authorization := r.Header.Get("Authorization")
	credential := strings.TrimPrefix(authorization, "Bearer ")
	if !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare([]byte(credential), []byte(h.Token)) != 1 || !time.Now().Before(h.ExpiresAt) || h.Context.Err() != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), h.ExpiresAt)
	h.turnMu.Lock()
	select {
	case h.admit <- struct{}{}:
		h.turnCancel = cancel
		defer func() { <-h.admit }()
	default:
		h.turnMu.Unlock()
		cancel()
		w.WriteHeader(http.StatusConflict)
		return
	}
	h.turnMu.Unlock()
	defer func() { h.turnMu.Lock(); h.turnCancel = nil; h.turnMu.Unlock() }()
	// The owning voice lifetime supplements the derived HTTP request context.
	stop := context.AfterFunc(h.Context, cancel) //nolint:contextcheck // Session revocation must also cancel this request.
	stopBody := context.AfterFunc(ctx, func() { _ = r.Body.Close() })
	defer func() { stop(); stopBody(); cancel() }()
	if h.Revalidate != nil {
		if err := h.Revalidate(ctx); err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}
	var input struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	if decoder.Decode(&input) != nil || !input.Stream {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	utterance := ""
	for i := len(input.Messages) - 1; i >= 0; i-- {
		if input.Messages[i].Role == "user" {
			utterance = strings.TrimSpace(input.Messages[i].Content)
			break
		}
	}
	if utterance == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if ctx.Err() != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}
	if h.BindTurn == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	turnID, err := h.BindTurn(ctx, utterance)
	if err != nil || turnID == "" {
		w.WriteHeader(http.StatusConflict)
		return
	}
	// Retain the canonical native identity before execution, including uncertain
	// failures. Vendor-controlled history cannot create a fresh identity.
	digest := sha256.Sum256([]byte(turnID))
	if _, replay := h.seen[digest]; replay || len(h.seen) >= 256 {
		w.WriteHeader(http.StatusConflict)
		return
	}
	h.seen[digest] = struct{}{}
	controller := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	started := false
	write := func(delta map[string]string, finish any) error {
		chunk := map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": h.Model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}}
		body, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
			return err
		}
		started = true
		return controller.Flush()
	}
	_, err = h.Agent.StreamTurn(ctx, cascaded.AgentInput{Utterance: utterance, Locale: h.Locale}, turnID, func(delta string) error {
		if !h.disclosed && h.FirstResponsePrefix != "" {
			if err := write(map[string]string{"content": h.FirstResponsePrefix}, nil); err != nil {
				return err
			}
			h.disclosed = true
		}
		return write(map[string]string{"content": delta}, nil)
	})
	if err != nil {
		// Never turn a failed/approval-blocked agent operation into spoken text.
		code := "agent_turn_failed"
		var coded cascaded.CodedError
		if errors.As(err, &coded) {
			code = cascaded.SafeFailureCode(coded.Code())
		}
		failure, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code}})
		if !started {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write(failure)
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", failure)
		_ = controller.Flush()
		return
	}
	if write(map[string]string{}, "stop") != nil {
		return
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	_ = controller.Flush()
}
