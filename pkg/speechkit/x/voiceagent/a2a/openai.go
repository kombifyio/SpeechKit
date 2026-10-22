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

// ErrTurnRejected is returned by [OpenAIHandler.Answer] when the turn is not
// admitted: expired or revoked authority, a turn already in flight, failed
// revalidation, or an utterance that does not bind to a host-observed final.
var ErrTurnRejected = errors.New("speechkit a2a: native turn rejected")

// begin admits one turn under the handler's authority: deadline, single-flight
// admission, session revocation and current-authority revalidation. A non-zero
// status names the rejection; release must run when the turn ends.
func (h *OpenAIHandler) begin(parent context.Context) (context.Context, func(), int) {
	if !time.Now().Before(h.ExpiresAt) || h.Context.Err() != nil {
		return nil, nil, http.StatusUnauthorized
	}
	ctx, cancel := context.WithDeadline(parent, h.ExpiresAt)
	h.turnMu.Lock()
	select {
	case h.admit <- struct{}{}:
		h.turnCancel = cancel
	default:
		h.turnMu.Unlock()
		cancel()
		return nil, nil, http.StatusConflict
	}
	h.turnMu.Unlock()
	// The owning voice lifetime supplements the caller's context.
	stop := context.AfterFunc(h.Context, cancel) //nolint:contextcheck // Session revocation must also cancel this turn.
	release := func() {
		stop()
		cancel()
		h.turnMu.Lock()
		h.turnCancel = nil
		h.turnMu.Unlock()
		<-h.admit
	}
	if h.Revalidate != nil {
		if err := h.Revalidate(ctx); err != nil {
			release()
			return nil, nil, http.StatusForbidden
		}
	}
	return ctx, release, 0
}

// bind resolves utterance against a host-observed native final and consumes
// its identity before execution, including uncertain failures, so vendor
// history or a retried event can never create or replay a turn.
func (h *OpenAIHandler) bind(ctx context.Context, utterance string) (string, int) {
	if ctx.Err() != nil {
		return "", http.StatusConflict
	}
	if h.BindTurn == nil {
		return "", http.StatusServiceUnavailable
	}
	turnID, err := h.BindTurn(ctx, utterance)
	if err != nil || turnID == "" {
		return "", http.StatusConflict
	}
	digest := sha256.Sum256([]byte(turnID))
	if _, replay := h.seen[digest]; replay || len(h.seen) >= 256 {
		return "", http.StatusConflict
	}
	h.seen[digest] = struct{}{}
	return turnID, 0
}

// stream runs the bound agent and emits the trusted disclosure once before
// the first answer delta.
func (h *OpenAIHandler) stream(ctx context.Context, utterance, turnID string, emit func(string) error) (cascaded.AgentOutput, error) {
	// The disclosure is host-trusted text and needs nothing from the agent, so it
	// is spoken now, while the agent works, instead of after its first delta.
	if !h.disclosed && h.FirstResponsePrefix != "" {
		if err := emit(h.FirstResponsePrefix); err != nil {
			return cascaded.AgentOutput{}, err
		}
		h.disclosed = true
	}
	return h.Agent.StreamTurn(ctx, cascaded.AgentInput{Utterance: utterance, Locale: h.Locale}, turnID, emit)
}

// Answer runs one turn in-process for native providers whose tool call or
// delegation event (instead of an HTTP chat-completions callback) asks the
// bound agent. It applies the same admission, revalidation, final-turn
// binding, replay protection and disclosure as ServeHTTP and returns the
// agent's complete answer. A failed agent turn is returned as an error and
// never as speakable text.
func (h *OpenAIHandler) Answer(ctx context.Context, utterance string) (string, error) {
	utterance = strings.TrimSpace(utterance)
	if utterance == "" {
		return "", ErrTurnRejected
	}
	turnCtx, release, status := h.begin(ctx)
	if status != 0 {
		return "", fmt.Errorf("%w: status %d", ErrTurnRejected, status)
	}
	defer release()
	turnID, status := h.bind(turnCtx, utterance)
	if status != 0 {
		return "", fmt.Errorf("%w: status %d", ErrTurnRejected, status)
	}
	// Nothing reaches the vendor until the turn succeeds, so the disclosure
	// is prepended (and marked spoken) only to an answer that is delivered.
	var answer strings.Builder
	if _, err := h.Agent.StreamTurn(turnCtx, cascaded.AgentInput{Utterance: utterance, Locale: h.Locale}, turnID, func(delta string) error {
		answer.WriteString(delta)
		return nil
	}); err != nil {
		return "", err
	}
	text := answer.String()
	if !h.disclosed && h.FirstResponsePrefix != "" {
		text = h.FirstResponsePrefix + text
		h.disclosed = true
	}
	return text, nil
}

func (h *OpenAIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	authorization := r.Header.Get("Authorization")
	credential := strings.TrimPrefix(authorization, "Bearer ")
	if !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare([]byte(credential), []byte(h.Token)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	ctx, release, status := h.begin(r.Context())
	if status != 0 {
		w.WriteHeader(status)
		return
	}
	stopBody := context.AfterFunc(ctx, func() { _ = r.Body.Close() })
	defer func() { stopBody(); release() }()
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
	turnID, status := h.bind(ctx, utterance)
	if status != 0 {
		w.WriteHeader(status)
		return
	}
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
	_, err := h.stream(ctx, utterance, turnID, func(delta string) error {
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
