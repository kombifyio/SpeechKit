//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
)

// ── fakes ───────────────────────────────────────────────────────────────────

// fakeProvider is a controllable LiveProviderAdapter for adapter tests.
// Messages enqueued via Push are returned from Receive in order; client
// pushes are recorded for assertion.
type fakeProvider struct {
	mu             sync.Mutex
	connectErr     error
	connectCfg     *LiveConfigFrame
	receiveQueue   chan *LiveMessage
	sentAudio      [][]byte
	sentText       []string
	updatedConfigs []LiveConfigFrame
	toolResponses  []ToolResponseFrame
	streamEndCalls int
	cancelCalls    int
	liveKitSupport bool
	closed         bool
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{receiveQueue: make(chan *LiveMessage, 16)}
}

func (p *fakeProvider) Connect(_ context.Context, cfg LiveConfigFrame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.connectCfg = &cfg
	return p.connectErr
}
func (p *fakeProvider) Receive(ctx context.Context) (*LiveMessage, error) {
	select {
	case msg, ok := <-p.receiveQueue:
		if !ok {
			return nil, errors.New("provider receive channel closed")
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (p *fakeProvider) SendAudio(b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := append([]byte(nil), b...)
	p.sentAudio = append(p.sentAudio, cp)
	return nil
}
func (p *fakeProvider) SendAudioStreamEnd() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streamEndCalls++
	return nil
}
func (p *fakeProvider) SendText(t string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sentText = append(p.sentText, t)
	return nil
}
func (p *fakeProvider) UpdateInstructions(_ context.Context, cfg LiveConfigFrame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.updatedConfigs = append(p.updatedConfigs, cfg)
	return nil
}
func (p *fakeProvider) SendToolResponse(frame ToolResponseFrame) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.toolResponses = append(p.toolResponses, frame)
	return nil
}
func (p *fakeProvider) CancelResponse() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancelCalls++
	return nil
}
func (p *fakeProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	close(p.receiveQueue)
	return nil
}
func (p *fakeProvider) Name() string { return "fake" }
func (p *fakeProvider) SupportsLiveKitTransport() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.liveKitSupport
}

func (p *fakeProvider) push(msg *LiveMessage) {
	select {
	case p.receiveQueue <- msg:
	default:
	}
}

// fakeResolver returns a fixed LiveConfigFrame, optionally with an error.
type fakeResolver struct {
	frame      LiveConfigFrame
	stepFrames map[int]LiveConfigFrame
	err        error
}

func (r *fakeResolver) Resolve(_ StartFrame) (LiveConfigFrame, error) {
	return r.frame, r.err
}
func (r *fakeResolver) ResolveStep(_ StartFrame, stepIndex int) (LiveConfigFrame, error) {
	if r.err != nil {
		return LiveConfigFrame{}, r.err
	}
	if r.stepFrames != nil {
		if frame, ok := r.stepFrames[stepIndex]; ok {
			return frame, nil
		}
	}
	return LiveConfigFrame{}, errors.New("missing step")
}

// ── test infrastructure ─────────────────────────────────────────────────────

// adapterTestEnv pairs a running httptest.Server hosting a single Adapter
// run() with a connected client websocket. Cleans up on test completion.
type adapterTestEnv struct {
	srv           *httptest.Server
	conn          *websocket.Conn
	provider      *fakeProvider
	resolver      *fakeResolver
	bridgeFactory *fakeMediaBridgeFactory
	done          chan struct{}
	usage         chan VoiceUsage
	idle          time.Duration
}

func startAdapterEnv(t *testing.T, idle time.Duration, provider *fakeProvider, resolver *fakeResolver) *adapterTestEnv {
	t.Helper()
	return startAdapterEnvWithBridge(t, idle, provider, resolver, nil)
}

func startAdapterEnvWithBridge(t *testing.T, idle time.Duration, provider *fakeProvider, resolver *fakeResolver, bridgeFactory *fakeMediaBridgeFactory) *adapterTestEnv {
	t.Helper()
	env := &adapterTestEnv{
		provider:      provider,
		resolver:      resolver,
		bridgeFactory: bridgeFactory,
		done:          make(chan struct{}),
		usage:         make(chan VoiceUsage, 1),
		idle:          idle,
	}
	// The upgrade runs behind the same RequestID middleware the real server
	// mounts, so error frames carry the correlation id a support request
	// would quote.
	env.srv = httptest.NewServer(middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("server accept: %v", err)
			return
		}
		conn.SetReadLimit(1 << 20)
		adapter := &Adapter{
			Session:     &ManagedSession{ID: "test-session", Owner: Identity{UserID: "u1"}},
			Conn:        conn,
			Provider:    provider,
			Persona:     resolver,
			IdleTimeout: idle,
			MediaBridge: bridgeFactory,
			OnUsage:     func(usage VoiceUsage) { env.usage <- usage },
			OnClose:     func() { close(env.done) },
		}
		adapter.Run(r.Context())
	})))
	t.Cleanup(func() {
		env.srv.Close()
	})

	// Dial as client.
	wsURL := "ws" + strings.TrimPrefix(env.srv.URL, "http")
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, wsURL, nil)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	env.conn = conn
	t.Cleanup(func() {
		_ = conn.CloseNow()
	})
	return env
}

type fakeMediaBridgeFactory struct {
	mu       sync.Mutex
	starts   []MediaBridgeRequest
	bridge   *fakeMediaBridge
	startErr error
}

func (f *fakeMediaBridgeFactory) Start(_ context.Context, req MediaBridgeRequest) (MediaBridge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts = append(f.starts, req)
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.bridge == nil {
		f.bridge = &fakeMediaBridge{}
	}
	return f.bridge, nil
}

type fakeMediaBridge struct {
	mu     sync.Mutex
	audio  [][]byte
	closed bool
}

func (b *fakeMediaBridge) SendAudio(data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.audio = append(b.audio, append([]byte(nil), data...))
	return nil
}

func (b *fakeMediaBridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

// sendStart sends a "start" frame; helper that nearly every test needs.
func sendStart(t *testing.T, conn *websocket.Conn, frame StartFrame) {
	t.Helper()
	frame.Type = MsgStart
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write start: %v", err)
	}
}

// readJSONFrame blocks on a single text frame and decodes it as the given
// target type. Times out after 2 seconds.
func readJSONFrame(t *testing.T, conn *websocket.Conn, dst any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("expected text frame, got %v", typ)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		t.Fatalf("unmarshal frame %s: %v", string(data), err)
	}
}

// readBinaryFrame blocks on a single binary frame.
func readBinaryFrame(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Fatalf("expected binary frame, got %v", typ)
	}
	return data
}

// readEnvelope blocks on a single text frame and returns its envelope type.
func readEnvelope(t *testing.T, conn *websocket.Conn) (string, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("expected text frame, got %v", typ)
	}
	var env envelope
	_ = json.Unmarshal(data, &env)
	return env.Type, data
}

func eventTypesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type staticProviderFactory struct {
	provider LiveProviderAdapter
}

func (f staticProviderFactory) NewProvider() LiveProviderAdapter {
	return f.provider
}

// mustManager builds a SessionManager with a deterministic test secret. The
// session-manager behavior tests themselves moved to
// internal/server/wssession; this helper stays because the WS handler and
// LiveKit tests construct managers constantly.
func mustManager(t *testing.T, opts Options) *SessionManager {
	t.Helper()
	if len(opts.TicketSecret) == 0 {
		opts.TicketSecret = []byte("super-secret-key-16+bytes")
	}
	m, err := NewSessionManager(opts)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	return m
}
