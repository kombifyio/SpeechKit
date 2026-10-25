// Package toolbridge implements a generic HTTP client for the SpeechKit
// voice-agent tool bridge wire contract "speechkit.toolbridge.v1"
// (docs/server/toolbridge.v1.md).
//
// The bridge is deployment-neutral: it knows nothing about who operates the
// remote endpoint. All tenant semantics (tool curation, authorization,
// entitlements, approval) live behind the two configured URLs; SpeechKit only
// forwards the opaque per-session credential the fronting proxy supplied at
// session creation as an Authorization bearer.
//
// Fail-closed behavior:
//   - no credential            -> no manifest, session runs tool-less
//   - manifest fetch failure   -> session runs tool-less
//   - invoke failure / denial  -> structured error payload ({error,
//     user_guidance}) the model can verbalize; the session never dies
//   - hard per-session call cap (default 20)
//
// The package is intentionally platform-neutral (no build tag) so its unit
// tests run on every OS; the linux-only server wiring adapts it to the
// voiceagent.SessionToolRouter interface.
package toolbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// WireVersion is the toolbridge wire-contract version this client speaks.
const WireVersion = "speechkit.toolbridge.v1"

const (
	// DefaultTimeout bounds one invoke round-trip.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxCallsPerSession is the hard per-session invoke cap.
	DefaultMaxCallsPerSession = 20
	// manifestCacheTTL bounds how long a manifest is reused per credential.
	manifestCacheTTL = 5 * time.Minute
	// maxResponseBytes caps how much of a bridge response body is read.
	maxResponseBytes = 1 << 20 // 1 MiB
	// sessionStateTTL is how long an idle per-session call counter survives
	// before lazy purging; retain budgets beyond the four-hour hosted session
	// ceiling, including setup and shutdown time.
	sessionStateTTL = 6 * time.Hour
)

// Options configures a Bridge.
type Options struct {
	// ManifestURL is the GET endpoint returning the tool manifest. Required.
	ManifestURL string
	// InvokeURL is the POST endpoint executing one tool call. Required.
	InvokeURL string
	// Timeout bounds one HTTP round-trip. Zero uses DefaultTimeout.
	Timeout time.Duration
	// MaxCallsPerSession hard-caps invokes per session ID. Zero uses
	// DefaultMaxCallsPerSession.
	MaxCallsPerSession int
	// HTTPClient overrides the transport (tests). Nil builds one from Timeout.
	HTTPClient *http.Client
	// Clock overrides the time source (tests). Nil uses time.Now.
	Clock func() time.Time
}

// Session identifies one voice session towards the bridge. Credential is the
// opaque per-session secret forwarded as Authorization bearer; it must never
// be logged.
type Session struct {
	ID         string
	Locale     string
	PersonaID  string
	Credential string
}

// ToolDefinition is one manifest entry.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	TimeoutMs   int            `json:"timeout_ms,omitempty"`
}

// manifestEnvelope is the GET /manifest response shape.
type manifestEnvelope struct {
	Version    string           `json:"version"`
	Harness    string           `json:"harness,omitempty"`
	ToolBridge bool             `json:"tool_bridge,omitempty"`
	Tools      []ToolDefinition `json:"tools"`
}

// callRequest is the POST /call request body.
type callRequest struct {
	Version string         `json:"version"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Session callSession    `json:"session"`
}

type callSession struct {
	SessionID string `json:"session_id"`
	Locale    string `json:"locale,omitempty"`
	PersonaID string `json:"persona_id,omitempty"`
}

// callResponse is the POST /call response body: either a success payload
// (status "ok" + result_text/result_data) or a denial/error envelope carrying
// error_code, reason_code, message, retryable, and user_guidance per the
// Feature-Entitlement-UX contract.
type callResponse struct {
	Status       string             `json:"status,omitempty"`
	Version      string             `json:"version,omitempty"`
	Tool         string             `json:"tool,omitempty"`
	ResultText   string             `json:"result_text,omitempty"`
	ResultData   map[string]any     `json:"result_data,omitempty"`
	ErrorCode    string             `json:"error_code,omitempty"`
	ReasonCode   string             `json:"reason_code,omitempty"`
	Message      string             `json:"message,omitempty"`
	Retryable    *bool              `json:"retryable,omitempty"`
	UserGuidance map[string]any     `json:"user_guidance,omitempty"`
	Error        *callResponseError `json:"error,omitempty"`
}

type callResponseError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Bridge is a concurrency-safe toolbridge HTTP client shared across sessions.
type Bridge struct {
	manifestURL string
	invokeURL   string
	timeout     time.Duration
	maxCalls    int
	client      *http.Client
	clock       func() time.Time

	mu       sync.Mutex
	manifest map[string]manifestCacheEntry // keyed by credential hash
	inflight map[string]*manifestFetch     // keyed by credential hash
	sessions map[string]*sessionCallState  // keyed by session ID
}

type manifestCacheEntry struct {
	tools     []ToolDefinition
	fetchedAt time.Time
}

// manifestFetch is one network manifest fetch shared by every caller with
// the same credential; done closes once tools/err are set.
type manifestFetch struct {
	done  chan struct{}
	tools []ToolDefinition
	err   error
}

type sessionCallState struct {
	calls   int
	touched time.Time
}

// New constructs a Bridge. Returns an error when either URL is missing so a
// misconfigured deployment fails at wiring time, not mid-session.
func New(opts Options) (*Bridge, error) {
	manifestURL := strings.TrimSpace(opts.ManifestURL)
	invokeURL := strings.TrimSpace(opts.InvokeURL)
	if manifestURL == "" || invokeURL == "" {
		return nil, errors.New("toolbridge: ManifestURL and InvokeURL are required")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxCalls := opts.MaxCallsPerSession
	if maxCalls <= 0 {
		maxCalls = DefaultMaxCallsPerSession
	}
	client := opts.HTTPClient
	if client == nil {
		client = netsec.NewClient(netsec.ClientOptions{Timeout: timeout})
	}
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Bridge{
		manifestURL: manifestURL,
		invokeURL:   invokeURL,
		timeout:     timeout,
		maxCalls:    maxCalls,
		client:      client,
		clock:       clock,
		manifest:    make(map[string]manifestCacheEntry),
		inflight:    make(map[string]*manifestFetch),
		sessions:    make(map[string]*sessionCallState),
	}, nil
}

// Definitions returns the tool manifest for the session, cached per
// credential for manifestCacheTTL. A session without a credential gets no
// tools (fail-closed). Errors mean "run tool-less", never "kill the session".
// ctx bounds only the wait: the fetch itself runs for the bridge timeout and
// is shared with a Prefetch or another caller for the same credential, so a
// session started right after its creation waits for the fetch that the
// creation began instead of starting a second one.
func (b *Bridge) Definitions(ctx context.Context, session Session) ([]ToolDefinition, error) {
	credential := strings.TrimSpace(session.Credential)
	if credential == "" {
		return nil, nil
	}
	tools, fetch := b.cachedOrFetch(credential)
	if fetch == nil {
		return tools, nil
	}
	select {
	case <-fetch.done:
		return fetch.tools, fetch.err
	case <-ctx.Done():
		return nil, fmt.Errorf("toolbridge: manifest fetch: %w", ctx.Err())
	}
}

// Prefetch starts the manifest fetch for a session that is about to start,
// typically at session creation, so the cold path of the remote endpoint is
// paid before the start frame instead of inside its short merge budget.
func (b *Bridge) Prefetch(session Session) {
	credential := strings.TrimSpace(session.Credential)
	if credential == "" {
		return
	}
	_, _ = b.cachedOrFetch(credential)
}

// cachedOrFetch returns the cached tools, or the running or newly started
// fetch for the credential.
func (b *Bridge) cachedOrFetch(credential string) ([]ToolDefinition, *manifestFetch) {
	key := credentialHash(credential)
	now := b.clock()

	b.mu.Lock()
	defer b.mu.Unlock()
	if entry, ok := b.manifest[key]; ok && now.Sub(entry.fetchedAt) < manifestCacheTTL {
		return entry.tools, nil
	}
	if fetch, ok := b.inflight[key]; ok {
		return nil, fetch
	}
	fetch := &manifestFetch{done: make(chan struct{})}
	b.inflight[key] = fetch
	go b.runFetch(key, credential, fetch)
	return nil, fetch
}

func (b *Bridge) runFetch(key, credential string, fetch *manifestFetch) {
	started := b.clock()
	tools, err := b.fetchManifest(context.Background(), credential)
	fetchedAt := b.clock()

	b.mu.Lock()
	delete(b.inflight, key)
	if err == nil {
		b.purgeManifestLocked(fetchedAt)
		b.manifest[key] = manifestCacheEntry{tools: tools, fetchedAt: fetchedAt}
	}
	b.mu.Unlock()

	fetch.tools, fetch.err = tools, err
	close(fetch.done)
	if err == nil {
		slog.Info("toolbridge: manifest fetched", "tools", len(tools), "duration_ms", fetchedAt.Sub(started).Milliseconds())
	} else {
		slog.Info("toolbridge: manifest fetch failed", "duration_ms", fetchedAt.Sub(started).Milliseconds(), "err", err) // #nosec G706 -- structured attribute, no credential in the error path.
	}
}

func (b *Bridge) fetchManifest(ctx context.Context, credential string) ([]ToolDefinition, error) {
	reqCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, b.manifestURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("toolbridge: build manifest request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("toolbridge: manifest fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("toolbridge: manifest read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("toolbridge: manifest endpoint returned HTTP %d", resp.StatusCode)
	}
	var envelope manifestEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("toolbridge: manifest decode: %w", err)
	}
	if envelope.Version != WireVersion {
		// Fail-closed on contract drift: an unknown version means we cannot
		// trust the tool shapes; the session runs tool-less.
		return nil, fmt.Errorf("toolbridge: unsupported manifest version %q (want %q)", envelope.Version, WireVersion)
	}
	tools := make([]ToolDefinition, 0, len(envelope.Tools))
	for _, tool := range envelope.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

// Execute invokes one tool call. The returned map is always a model-facing
// tool response payload; ok=false is returned only when no usable payload
// could be produced (the caller substitutes a generic structured error).
// Denial envelopes from the remote endpoint are mapped to
// {error:{code,message}, user_guidance:...} so the model verbalizes the
// denial instead of hallucinating a result.
func (b *Bridge) Execute(ctx context.Context, session Session, tool string, args map[string]any) (map[string]any, bool) {
	credential := strings.TrimSpace(session.Credential)
	if credential == "" {
		return errorPayload("tool_bridge_credential_missing", "this session has no tool bridge credential", nil), true
	}
	if !b.admitCall(session.ID) {
		return errorPayload("tool_call_limit_exceeded",
			fmt.Sprintf("the per-session tool call limit (%d) was reached", b.maxCalls),
			map[string]any{
				"title": "Tool limit reached",
				"body":  "This voice session used up its tool call budget. Continue the conversation without tools or start a new session.",
			}), true
	}

	if args == nil {
		args = map[string]any{}
	}
	payload, err := json.Marshal(callRequest{
		Version: WireVersion,
		Tool:    tool,
		Args:    args,
		Session: callSession{
			SessionID: session.ID,
			Locale:    session.Locale,
			PersonaID: session.PersonaID,
		},
	})
	if err != nil {
		slog.Warn("toolbridge: encode call request failed", "tool", tool, "err", err)
		return nil, false
	}

	reqCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, b.invokeURL, bytes.NewReader(payload))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := b.client.Do(req)
	if err != nil {
		slog.Warn("toolbridge: invoke failed", "tool", tool, "err", err)
		return errorPayload("tool_bridge_unreachable", "the tool endpoint did not answer in time", nil), true
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		slog.Warn("toolbridge: invoke response read failed", "tool", tool, "err", err)
		return nil, false
	}

	var call callResponse
	if err := json.Unmarshal(body, &call); err != nil {
		slog.Warn("toolbridge: invoke response decode failed", "tool", tool, "status", resp.StatusCode, "err", err)
		return nil, false
	}
	return mapCallResponse(call), true
}

// mapCallResponse converts a decoded wire response into the model-facing tool
// response payload.
func mapCallResponse(call callResponse) map[string]any {
	if call.Status == "ok" {
		result := map[string]any{"result": call.ResultText}
		if len(call.ResultData) > 0 {
			result["data"] = call.ResultData
		}
		return result
	}
	code := call.ErrorCode
	message := call.Message
	if call.Error != nil {
		if code == "" {
			code = call.Error.Code
		}
		if message == "" {
			message = call.Error.Message
		}
	}
	if code == "" {
		code = "tool_call_failed"
	}
	if message == "" {
		message = "the tool call was not completed"
	}
	return errorPayload(code, message, call.UserGuidance)
}

// errorPayload builds the structured error tool response
// {error:{code,message}, user_guidance?} the model verbalizes.
func errorPayload(code, message string, userGuidance map[string]any) map[string]any {
	payload := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	if len(userGuidance) > 0 {
		payload["user_guidance"] = userGuidance
	}
	return payload
}

// admitCall counts one invoke against the session's hard cap. Stale session
// counters are purged lazily so the map stays bounded by active sessions.
func (b *Bridge) admitCall(sessionID string) bool {
	now := b.clock()
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, state := range b.sessions {
		if now.Sub(state.touched) > sessionStateTTL {
			delete(b.sessions, id)
		}
	}
	state, ok := b.sessions[sessionID]
	if !ok {
		state = &sessionCallState{}
		b.sessions[sessionID] = state
	}
	state.touched = now
	if state.calls >= b.maxCalls {
		return false
	}
	state.calls++
	return true
}

func (b *Bridge) purgeManifestLocked(now time.Time) {
	for key, entry := range b.manifest {
		if now.Sub(entry.fetchedAt) >= manifestCacheTTL {
			delete(b.manifest, key)
		}
	}
}

func credentialHash(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return hex.EncodeToString(sum[:])
}
