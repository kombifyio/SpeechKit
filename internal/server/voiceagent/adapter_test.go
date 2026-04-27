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
)

// ── fakes ───────────────────────────────────────────────────────────────────

// fakeProvider is a controllable LiveProviderAdapter for adapter tests.
// Messages enqueued via Push are returned from Receive in order; client
// pushes are recorded for assertion.
type fakeProvider struct {
	mu              sync.Mutex
	connectErr      error
	connectCfg      *LiveConfigFrame
	receiveQueue    chan *LiveMessage
	sentAudio       [][]byte
	sentText        []string
	streamEndCalls  int
	closed          bool
	receiveBlocking bool // when true, Receive blocks until ctx canceled OR Push
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

func (p *fakeProvider) push(msg *LiveMessage) {
	select {
	case p.receiveQueue <- msg:
	default:
	}
}

// fakeResolver returns a fixed LiveConfigFrame, optionally with an error.
type fakeResolver struct {
	frame LiveConfigFrame
	err   error
}

func (r *fakeResolver) Resolve(_ StartFrame) (LiveConfigFrame, error) {
	return r.frame, r.err
}

// ── test infrastructure ─────────────────────────────────────────────────────

// adapterTestEnv pairs a running httptest.Server hosting a single Adapter
// run() with a connected client websocket. Cleans up on test completion.
type adapterTestEnv struct {
	srv      *httptest.Server
	conn     *websocket.Conn
	provider *fakeProvider
	resolver *fakeResolver
	done     chan struct{}
	idle     time.Duration
}

func startAdapterEnv(t *testing.T, idle time.Duration, provider *fakeProvider, resolver *fakeResolver) *adapterTestEnv {
	t.Helper()
	env := &adapterTestEnv{
		provider: provider,
		resolver: resolver,
		done:     make(chan struct{}),
		idle:     idle,
	}
	env.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			OnClose:     func() { close(env.done) },
		}
		adapter.Run(r.Context())
	}))
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

// ── tests ───────────────────────────────────────────────────────────────────

func TestAdapter_StartFrameTriggersConnectAndStateListening(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{frame: LiveConfigFrame{Locale: "en"}}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{PersonaID: "default"})

	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	if stateMsg.Type != MsgState || stateMsg.State != "listening" {
		t.Fatalf("expected state listening, got %+v", stateMsg)
	}

	provider.mu.Lock()
	gotConfig := provider.connectCfg
	provider.mu.Unlock()
	if gotConfig == nil || gotConfig.Locale != "en" {
		t.Fatalf("Connect was not called or got wrong cfg: %+v", gotConfig)
	}
}

func TestAdapter_BinaryAudioForwardedToProvider(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg) // drain "listening"

	payload := []byte{0x01, 0x02, 0x03, 0x04}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	// Poll provider until SendAudio is observed (race-tolerant).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		got := append([][]byte(nil), provider.sentAudio...)
		provider.mu.Unlock()
		if len(got) == 1 && string(got[0]) == string(payload) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider did not receive forwarded audio")
}

func TestAdapter_ProviderMessagesRelayedToClient(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	// Push input transcript + audio chunk.
	provider.push(&LiveMessage{InputTranscript: "hello", InputTranscriptDone: true})
	provider.push(&LiveMessage{Audio: []byte{0xAA, 0xBB}})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgInputTranscript {
		t.Fatalf("expected input_transcript, got %s body=%s", typeName, string(raw))
	}
	var transcript TranscriptFrame
	if err := json.Unmarshal(raw, &transcript); err != nil {
		t.Fatalf("unmarshal transcript: %v", err)
	}
	if transcript.Text != "hello" || !transcript.Done {
		t.Fatalf("unexpected transcript frame: %+v", transcript)
	}

	got := readBinaryFrame(t, env.conn)
	if len(got) != 2 || got[0] != 0xAA || got[1] != 0xBB {
		t.Fatalf("unexpected audio bytes: %x", got)
	}
}

func TestAdapter_ClientStopProducesSessionEnd(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	// Send stop.
	stopFrame, _ := json.Marshal(map[string]string{"type": MsgStop})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, stopFrame); err != nil {
		t.Fatalf("write stop: %v", err)
	}

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgSessionEnd {
		t.Fatalf("expected session_end, got %s body=%s", typeName, string(raw))
	}
	var seq SessionEndFrame
	if err := json.Unmarshal(raw, &seq); err != nil {
		t.Fatalf("unmarshal session_end: %v", err)
	}
	if seq.Reason != "client" {
		t.Fatalf("expected reason=client, got %q", seq.Reason)
	}

	// Adapter should call OnClose shortly.
	select {
	case <-env.done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatalf("adapter did not invoke OnClose after stop")
	}
}

func TestAdapter_GoAwayClosesSessionWithReason(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{GoAway: true})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgSessionEnd {
		t.Fatalf("expected session_end, got %s body=%s", typeName, string(raw))
	}
	var seq SessionEndFrame
	_ = json.Unmarshal(raw, &seq)
	if seq.Reason != "go_away" {
		t.Fatalf("expected reason=go_away, got %q", seq.Reason)
	}

	select {
	case <-env.done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatalf("adapter did not invoke OnClose after GoAway")
	}
}

func TestAdapter_IdleTimeoutClosesSession(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	// 80 ms idle; no client frames → adapter should fire watchdog.
	env := startAdapterEnv(t, 80*time.Millisecond, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgSessionEnd {
		t.Fatalf("expected session_end after idle, got %s body=%s", typeName, string(raw))
	}
	var seq SessionEndFrame
	_ = json.Unmarshal(raw, &seq)
	if seq.Reason != "idle" {
		t.Fatalf("expected reason=idle, got %q", seq.Reason)
	}

	select {
	case <-env.done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatalf("adapter did not invoke OnClose after idle timeout")
	}
}

func TestAdapter_FirstFrameNotStartReturnsError(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	// Send a non-start text frame as the first frame.
	pingFrame, _ := json.Marshal(map[string]string{"type": MsgPing})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, pingFrame); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgError {
		t.Fatalf("expected error frame, got %s body=%s", typeName, string(raw))
	}
	var ef ErrorFrame
	_ = json.Unmarshal(raw, &ef)
	if ef.Code != "start_required" {
		t.Fatalf("expected code=start_required, got %q", ef.Code)
	}
}

func TestAdapter_PersonaResolverErrorReturnsError(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{err: errors.New("persona missing")}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{PersonaID: "ghost"})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgError {
		t.Fatalf("expected error frame, got %s body=%s", typeName, string(raw))
	}
	var ef ErrorFrame
	_ = json.Unmarshal(raw, &ef)
	if ef.Code != "persona_unresolved" {
		t.Fatalf("expected code=persona_unresolved, got %q", ef.Code)
	}
}

func TestAdapter_ProviderConnectErrorReturnsError(t *testing.T) {
	provider := newFakeProvider()
	provider.connectErr = errors.New("upstream down")
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgError {
		t.Fatalf("expected error frame, got %s body=%s", typeName, string(raw))
	}
	var ef ErrorFrame
	_ = json.Unmarshal(raw, &ef)
	if ef.Code != "provider_connect_failed" {
		t.Fatalf("expected code=provider_connect_failed, got %q", ef.Code)
	}
}

func TestAdapter_PingProducesPong(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	pingFrame, _ := json.Marshal(map[string]string{"type": MsgPing})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, pingFrame); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgPong {
		t.Fatalf("expected pong, got %s body=%s", typeName, string(raw))
	}
}

func TestAdapter_TextFrameForwardedToProvider(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	textFrame, _ := json.Marshal(map[string]string{"type": MsgText, "text": "compose an email"})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, textFrame); err != nil {
		t.Fatalf("write text: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		got := append([]string(nil), provider.sentText...)
		provider.mu.Unlock()
		if len(got) == 1 && got[0] == "compose an email" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("text frame was not forwarded to provider")
}

func TestAdapter_AudioEndForwardsStreamEnd(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	endFrame, _ := json.Marshal(map[string]string{"type": MsgAudioEnd})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, endFrame); err != nil {
		t.Fatalf("write audio_end: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		calls := provider.streamEndCalls
		provider.mu.Unlock()
		if calls == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider did not receive SendAudioStreamEnd")
}
