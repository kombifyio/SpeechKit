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
	"github.com/kombifyio/SpeechKit/internal/voiceeval"
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

func TestAdapterSelectProviderNormalizesPublicAliases(t *testing.T) {
	gemini := newFakeProvider()
	assemblyAI := newFakeProvider()
	openAI := newFakeProvider()
	cascaded := newFakeProvider()
	adapter := &Adapter{
		DefaultProvider: "google",
		Providers: map[string]ProviderFactory{
			"gemini":     staticProviderFactory{provider: gemini},
			"assemblyai": staticProviderFactory{provider: assemblyAI},
			"openai":     staticProviderFactory{provider: openAI},
			"cascaded":   staticProviderFactory{provider: cascaded},
		},
	}

	tests := []struct {
		name      string
		requested string
		wantName  string
		want      LiveProviderAdapter
	}{
		{name: "default google alias", requested: "", wantName: "gemini", want: gemini},
		{name: "google alias", requested: "google", wantName: "gemini", want: gemini},
		{name: "gemini profile id", requested: "realtime.google.gemini-native-audio", wantName: "gemini", want: gemini},
		{name: "assembly alias", requested: "assembly-ai", wantName: "assemblyai", want: assemblyAI},
		{name: "assembly profile id", requested: "realtime.assemblyai.voice-agent", wantName: "assemblyai", want: assemblyAI},
		{name: "openai profile id", requested: "realtime.openai.gpt-realtime-2", wantName: "openai", want: openAI},
		{name: "cascaded alias", requested: "pipeline-fallback", wantName: "cascaded", want: cascaded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, resolved, err := adapter.selectProvider(tt.requested)
			if err != nil {
				t.Fatalf("selectProvider() error = %v", err)
			}
			if resolved != tt.wantName {
				t.Fatalf("resolved = %q, want %q", resolved, tt.wantName)
			}
			if got != tt.want {
				t.Fatalf("provider = %T/%p, want %T/%p", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestAdapterSelectProviderReportsNormalizedUnknownProvider(t *testing.T) {
	adapter := &Adapter{
		Providers: map[string]ProviderFactory{
			"gemini": staticProviderFactory{provider: newFakeProvider()},
		},
	}

	_, resolved, err := adapter.selectProvider("realtime.assemblyai.voice-agent")
	if err == nil {
		t.Fatal("selectProvider() error = nil, want unavailable provider")
	}
	if resolved != "assemblyai" {
		t.Fatalf("resolved = %q, want assemblyai", resolved)
	}
	if !strings.Contains(err.Error(), `"assemblyai"`) || !strings.Contains(err.Error(), "gemini") {
		t.Fatalf("error = %q, want normalized provider and configured list", err.Error())
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
	if stateMsg.EventType != EventSessionReady || !eventTypesContain(stateMsg.EventTypes, EventSessionReady) {
		t.Fatalf("state event fields = %+v, want session_ready", stateMsg.EventFrameFields)
	}

	provider.mu.Lock()
	gotConfig := provider.connectCfg
	provider.mu.Unlock()
	if gotConfig == nil || gotConfig.Locale != "en" {
		t.Fatalf("Connect was not called or got wrong cfg: %+v", gotConfig)
	}
}

func TestAdapter_SessionReadyReportsProviderAndMediaTransport(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	// A profile-ID alias in start.provider must surface as the normalized
	// provider name on session_ready; the default transport is websocket.
	sendStart(t, env.conn, StartFrame{Provider: "realtime.deepgram.voice-agent"})

	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	if stateMsg.EventType != EventSessionReady {
		t.Fatalf("expected session_ready state frame, got %+v", stateMsg)
	}
	if stateMsg.Provider != "deepgram" {
		t.Fatalf("session_ready provider = %q, want deepgram", stateMsg.Provider)
	}
	if stateMsg.MediaTransport != MediaTransportWebSocket {
		t.Fatalf("session_ready media_transport = %q, want websocket", stateMsg.MediaTransport)
	}
}

// TestAdapter_CancelSuppressesCurrentReplyAndAcks covers the client `cancel`
// frame: it acks with `interrupted` (idempotently), invokes the provider's
// native cancel, drops the CURRENT reply's remaining downlink audio while
// transcripts keep flowing, and stops suppressing at the turn boundary.
func TestAdapter_CancelSuppressesCurrentReplyAndAcks(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	// Reply in flight: first chunk reaches the client.
	provider.push(&LiveMessage{Audio: []byte{0xAA}})
	if got := readBinaryFrame(t, env.conn); len(got) != 1 || got[0] != 0xAA {
		t.Fatalf("pre-cancel audio = %x, want aa", got)
	}

	cancelFrame, _ := json.Marshal(map[string]string{"type": MsgCancel})
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancelWrite()
	if err := env.conn.Write(writeCtx, websocket.MessageText, cancelFrame); err != nil {
		t.Fatalf("write cancel: %v", err)
	}
	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgInterrupted {
		t.Fatalf("expected interrupted ack, got %s body=%s", typeName, string(raw))
	}
	var ack InterruptedFrame
	_ = json.Unmarshal(raw, &ack)
	if ack.ProviderMetadata["reason"] != "client_cancel" {
		t.Fatalf("ack metadata = %#v, want reason=client_cancel", ack.ProviderMetadata)
	}

	// Idempotent: a second cancel acks again without erroring the session.
	if err := env.conn.Write(writeCtx, websocket.MessageText, cancelFrame); err != nil {
		t.Fatalf("write second cancel: %v", err)
	}
	typeName, raw = readEnvelope(t, env.conn)
	if typeName != MsgInterrupted {
		t.Fatalf("expected second interrupted ack, got %s body=%s", typeName, string(raw))
	}

	// The provider-native cancel was invoked for the in-flight reply.
	deadline := time.Now().Add(2 * time.Second)
	for {
		provider.mu.Lock()
		calls := provider.cancelCalls
		provider.mu.Unlock()
		if calls >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("provider CancelResponse was not invoked")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Remaining audio of the cancelled reply is dropped; its transcript still
	// flows. If 0xBB leaked, the next frame would be binary and readEnvelope
	// would fail.
	provider.push(&LiveMessage{Audio: []byte{0xBB}})
	provider.push(&LiveMessage{OutputTranscript: "cancelled tail", OutputTranscriptDone: true})
	typeName, raw = readEnvelope(t, env.conn)
	if typeName != MsgOutputTranscript {
		t.Fatalf("expected output_transcript after suppressed audio, got %s body=%s", typeName, string(raw))
	}

	// Turn boundary clears the suppression: the next reply plays again.
	provider.push(&LiveMessage{Done: true})
	if typeName, raw = readEnvelope(t, env.conn); typeName != MsgEvent {
		t.Fatalf("expected turn_end event, got %s body=%s", typeName, string(raw))
	}
	provider.push(&LiveMessage{Audio: []byte{0xCC}})
	if got := readBinaryFrame(t, env.conn); len(got) != 1 || got[0] != 0xCC {
		t.Fatalf("post-turn audio = %x, want cc (suppression must end at turn boundary)", got)
	}
}

func TestAdapter_CancelWhileIdleAcksWithoutSuppressing(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	cancelFrame, _ := json.Marshal(map[string]string{"type": MsgCancel})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, cancelFrame); err != nil {
		t.Fatalf("write cancel: %v", err)
	}
	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgInterrupted {
		t.Fatalf("expected interrupted ack, got %s body=%s", typeName, string(raw))
	}

	// Nothing was playing: no provider cancel, and the NEXT reply is not
	// muted by a stale suppression flag.
	provider.push(&LiveMessage{Audio: []byte{0xDD}})
	if got := readBinaryFrame(t, env.conn); len(got) != 1 || got[0] != 0xDD {
		t.Fatalf("post-idle-cancel audio = %x, want dd", got)
	}
	provider.mu.Lock()
	calls := provider.cancelCalls
	provider.mu.Unlock()
	if calls != 0 {
		t.Fatalf("CancelResponse calls = %d, want 0 for cancel while idle", calls)
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

func TestAdapter_LiveKitTransportStartsBridgeAndRelaysProviderAudio(t *testing.T) {
	provider := newFakeProvider()
	provider.liveKitSupport = true
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	bridgeFactory := &fakeMediaBridgeFactory{}
	env := startAdapterEnvWithBridge(t, 0, provider, resolver, bridgeFactory)

	sendStart(t, env.conn, StartFrame{MediaTransport: MediaTransportLiveKit})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	if stateMsg.Type != MsgState || stateMsg.State != "listening" {
		t.Fatalf("expected state listening, got %+v", stateMsg)
	}
	if stateMsg.MediaTransport != MediaTransportLiveKit {
		t.Fatalf("session_ready media_transport = %q, want livekit", stateMsg.MediaTransport)
	}

	bridgeFactory.mu.Lock()
	starts := append([]MediaBridgeRequest(nil), bridgeFactory.starts...)
	bridge := bridgeFactory.bridge
	bridgeFactory.mu.Unlock()
	if len(starts) != 1 {
		t.Fatalf("bridge starts = %d, want 1", len(starts))
	}
	if starts[0].SessionID != "test-session" || starts[0].Owner.UserID != "u1" || starts[0].Provider != provider {
		t.Fatalf("unexpected bridge request: %+v", starts[0])
	}
	if bridge == nil {
		t.Fatal("bridge was not retained")
	}

	provider.push(&LiveMessage{Audio: []byte{0x11, 0x22}})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bridge.mu.Lock()
		got := append([][]byte(nil), bridge.audio...)
		bridge.mu.Unlock()
		if len(got) == 1 && string(got[0]) == string([]byte{0x11, 0x22}) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider audio was not relayed to LiveKit bridge")
}

func TestAdapter_LiveKitTransportRejectsUnsupportedProvider(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	bridgeFactory := &fakeMediaBridgeFactory{}
	env := startAdapterEnvWithBridge(t, 0, provider, resolver, bridgeFactory)

	sendStart(t, env.conn, StartFrame{MediaTransport: MediaTransportLiveKit})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgError {
		t.Fatalf("expected error frame, got %s body=%s", typeName, string(raw))
	}
	var ef ErrorFrame
	_ = json.Unmarshal(raw, &ef)
	if ef.Code != "media_transport_unsupported" {
		t.Fatalf("expected code=media_transport_unsupported, got %q", ef.Code)
	}
	provider.mu.Lock()
	connectCfg := provider.connectCfg
	provider.mu.Unlock()
	if connectCfg != nil {
		t.Fatalf("provider connected despite unsupported LiveKit transport: %+v", connectCfg)
	}
}

func TestAdapter_LiveKitTransportRejectsWebSocketBinaryAudio(t *testing.T) {
	provider := newFakeProvider()
	provider.liveKitSupport = true
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnvWithBridge(t, 0, provider, resolver, &fakeMediaBridgeFactory{})

	sendStart(t, env.conn, StartFrame{MediaTransport: MediaTransportLiveKit})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageBinary, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgError {
		t.Fatalf("expected error frame, got %s body=%s", typeName, string(raw))
	}
	var ef ErrorFrame
	_ = json.Unmarshal(raw, &ef)
	if ef.Code != "audio_transport_mismatch" {
		t.Fatalf("expected code=audio_transport_mismatch, got %q", ef.Code)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.sentAudio) != 0 {
		t.Fatalf("websocket binary audio reached provider under LiveKit transport: %x", provider.sentAudio)
	}
}

func TestAdapter_LiveKitTransportClosesBridgeOnStop(t *testing.T) {
	provider := newFakeProvider()
	provider.liveKitSupport = true
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	bridgeFactory := &fakeMediaBridgeFactory{}
	env := startAdapterEnvWithBridge(t, 0, provider, resolver, bridgeFactory)

	sendStart(t, env.conn, StartFrame{MediaTransport: MediaTransportLiveKit})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	stopFrame, _ := json.Marshal(map[string]string{"type": MsgStop})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, stopFrame); err != nil {
		t.Fatalf("write stop: %v", err)
	}
	var ended SessionEndFrame
	readJSONFrame(t, env.conn, &ended)
	if ended.Type != MsgSessionEnd || ended.Reason != "client" {
		t.Fatalf("session end = %+v", ended)
	}

	select {
	case <-env.done:
	case <-time.After(30 * time.Second):
		t.Fatalf("adapter did not invoke OnClose after LiveKit stop")
	}
	bridgeFactory.mu.Lock()
	bridge := bridgeFactory.bridge
	bridgeFactory.mu.Unlock()
	if bridge == nil {
		t.Fatal("bridge missing")
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if !bridge.closed {
		t.Fatal("LiveKit bridge was not closed")
	}
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
	provider.push(&LiveMessage{
		InputTranscript:     "hello",
		InputTranscriptDone: true,
		ProviderMetadata:    map[string]any{"provider_event": "fake.input.final"},
		SessionResumable:    true,
	})
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
	if transcript.EventType != EventInputFinal ||
		!eventTypesContain(transcript.EventTypes, EventInputFinal) ||
		!eventTypesContain(transcript.EventTypes, EventSessionResumable) {
		t.Fatalf("transcript event fields = %+v", transcript.EventFrameFields)
	}
	if transcript.ProviderMetadata["provider_event"] != "fake.input.final" {
		t.Fatalf("provider metadata = %#v", transcript.ProviderMetadata)
	}

	got := readBinaryFrame(t, env.conn)
	if len(got) != 2 || got[0] != 0xAA || got[1] != 0xBB {
		t.Fatalf("unexpected audio bytes: %x", got)
	}
}

func TestAdapter_OutputTranscriptCarriesProviderEventFields(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		EventType:            EventOutputText,
		EventTypes:           []string{EventOutputText, EventTurnEnd},
		OutputTranscript:     "done",
		OutputTranscriptDone: true,
		ProviderMetadata:     map[string]any{"provider_event": "fake.output.final"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgOutputTranscript {
		t.Fatalf("expected output_transcript, got %s body=%s", typeName, string(raw))
	}
	var transcript TranscriptFrame
	if err := json.Unmarshal(raw, &transcript); err != nil {
		t.Fatalf("unmarshal output transcript: %v", err)
	}
	if transcript.Text != "done" || !transcript.Done {
		t.Fatalf("unexpected output transcript frame: %+v", transcript)
	}
	if transcript.EventType != EventOutputText ||
		!eventTypesContain(transcript.EventTypes, EventOutputText) ||
		!eventTypesContain(transcript.EventTypes, EventTurnEnd) {
		t.Fatalf("output transcript event fields = %+v", transcript.EventFrameFields)
	}
	if transcript.ProviderMetadata["provider_event"] != "fake.output.final" {
		t.Fatalf("output transcript provider metadata = %#v", transcript.ProviderMetadata)
	}
}

func TestAdapter_InterruptedCarriesProviderEventFields(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		Interrupted:      true,
		ProviderMetadata: map[string]any{"provider_event": "fake.barge_in"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgInterrupted {
		t.Fatalf("expected interrupted, got %s body=%s", typeName, string(raw))
	}
	var frame InterruptedFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal interrupted: %v", err)
	}
	if frame.EventType != EventInterrupted || !eventTypesContain(frame.EventTypes, EventInterrupted) {
		t.Fatalf("interrupted event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.barge_in" {
		t.Fatalf("interrupted provider metadata = %#v", frame.ProviderMetadata)
	}
}

func TestAdapter_StandaloneProviderEventRelayedToClient(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		Done:             true,
		SessionResumable: true,
		ProviderMetadata: map[string]any{"provider_event": "fake.turn.done"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgEvent {
		t.Fatalf("expected event, got %s body=%s", typeName, string(raw))
	}
	var frame EventFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if frame.EventType != EventSessionResumable ||
		!eventTypesContain(frame.EventTypes, EventSessionResumable) ||
		!eventTypesContain(frame.EventTypes, EventTurnEnd) {
		t.Fatalf("event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.turn.done" {
		t.Fatalf("provider metadata = %#v", frame.ProviderMetadata)
	}

	provider.push(&LiveMessage{
		Audio:            []byte{0xCC},
		Done:             true,
		ProviderMetadata: map[string]any{"provider_event": "fake.audio.done"},
	})
	if got := readBinaryFrame(t, env.conn); len(got) != 1 || got[0] != 0xCC {
		t.Fatalf("audio frame = %x, want cc", got)
	}
	typeName, raw = readEnvelope(t, env.conn)
	if typeName != MsgEvent {
		t.Fatalf("expected audio follow-up event, got %s body=%s", typeName, string(raw))
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal audio follow-up event: %v", err)
	}
	if frame.EventType != EventTurnEnd ||
		!eventTypesContain(frame.EventTypes, EventOutputAudio) ||
		!eventTypesContain(frame.EventTypes, EventTurnEnd) {
		t.Fatalf("audio follow-up event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.audio.done" {
		t.Fatalf("audio follow-up provider metadata = %#v", frame.ProviderMetadata)
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
	if seq.EventType != EventSessionEnd || !eventTypesContain(seq.EventTypes, EventSessionEnd) {
		t.Fatalf("session_end event fields = %+v", seq.EventFrameFields)
	}

	// Adapter should call OnClose shortly. The deadline is generous to
	// absorb GitHub-hosted Linux runner contention; the watchdog itself
	// is sub-millisecond when the pumps actually finish.
	select {
	case <-env.done:
		// expected
	case <-time.After(30 * time.Second):
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
	if seq.EventType != EventSessionEnd || !eventTypesContain(seq.EventTypes, EventSessionEnd) {
		t.Fatalf("go_away event fields = %+v", seq.EventFrameFields)
	}

	select {
	case <-env.done:
		// expected
	case <-time.After(30 * time.Second):
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
	case <-time.After(30 * time.Second):
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
	if want := ErrorRemediation("start_required"); ef.Remediation != want {
		t.Fatalf("expected remediation %q, got %q", want, ef.Remediation)
	}
	if ef.RequestID == "" {
		t.Fatal("expected error frame to carry the upgrade request id")
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

func TestAdapter_StartFrameEmitsInitialSequenceStep(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{frame: LiveConfigFrame{
		SequenceID: "meeting",
		StepID:     "opening",
		StepIndex:  0,
		StepCount:  2,
	}}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{SequenceID: "meeting"})

	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	if stateMsg.Type != MsgState || stateMsg.State != "listening" {
		t.Fatalf("expected state listening, got %+v", stateMsg)
	}

	var stepMsg SequenceStepFrame
	readJSONFrame(t, env.conn, &stepMsg)
	if stepMsg.Type != MsgSequenceStep || stepMsg.SequenceID != "meeting" ||
		stepMsg.StepID != "opening" || stepMsg.StepIndex != 0 || stepMsg.Status != "entered" {
		t.Fatalf("unexpected sequence step frame: %+v", stepMsg)
	}
}

func TestAdapter_AdvanceStepUpdatesProviderAndEmitsSequenceFrames(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{
		frame: LiveConfigFrame{
			SequenceID: "meeting",
			StepID:     "opening",
			StepIndex:  0,
			StepCount:  2,
		},
		stepFrames: map[int]LiveConfigFrame{
			1: {
				SequenceID:      "meeting",
				StepID:          "discussion",
				StepIndex:       1,
				StepCount:       2,
				StepInstruction: "Moderate the main discussion.",
				SystemPrompt:    "Role prompt\n\n[Current step: discussion]\nModerate the main discussion.",
			},
		},
	}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{SequenceID: "meeting"})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	var enteredOpening SequenceStepFrame
	readJSONFrame(t, env.conn, &enteredOpening)

	advanceFrame, _ := json.Marshal(AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "host"})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, advanceFrame); err != nil {
		t.Fatalf("write advance_step: %v", err)
	}

	var completed SequenceStepFrame
	readJSONFrame(t, env.conn, &completed)
	if completed.Status != "completed" || completed.StepID != "opening" {
		t.Fatalf("completed frame = %+v", completed)
	}
	var enteredDiscussion SequenceStepFrame
	readJSONFrame(t, env.conn, &enteredDiscussion)
	if enteredDiscussion.Status != "entered" || enteredDiscussion.StepID != "discussion" || enteredDiscussion.StepIndex != 1 {
		t.Fatalf("entered frame = %+v", enteredDiscussion)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		updates := append([]LiveConfigFrame(nil), provider.updatedConfigs...)
		provider.mu.Unlock()
		if len(updates) == 1 && updates[0].StepID == "discussion" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider did not receive instruction update")
}

func TestAdapter_EvaluatesWorkflowDialogSimulation(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{
		frame: LiveConfigFrame{
			SequenceID:      "strategy-meeting",
			StepID:          "frame",
			StepIndex:       0,
			StepCount:       3,
			StepInstruction: "Clarify the goal and constraints.",
			StepMaxTurns:    3,
		},
		stepFrames: map[int]LiveConfigFrame{
			1: {
				SequenceID:      "strategy-meeting",
				StepID:          "diverge",
				StepIndex:       1,
				StepCount:       3,
				StepInstruction: "Explore alternatives and blind spots.",
				StepMaxTurns:    3,
				SystemPrompt:    "Explore alternatives and blind spots.",
			},
			2: {
				SequenceID:      "strategy-meeting",
				StepID:          "converge",
				StepIndex:       2,
				StepCount:       3,
				StepInstruction: "Converge on a concrete next step.",
				StepMaxTurns:    3,
				SystemPrompt:    "Converge on a concrete next step.",
			},
		},
	}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{SequenceID: "strategy-meeting"})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)
	var enteredFrame SequenceStepFrame
	readJSONFrame(t, env.conn, &enteredFrame)

	dialog := []struct {
		stepID string
		user   string
		agent  string
		rule   voiceeval.Rule
	}{
		{
			stepID: "frame",
			user:   "We need to plan the launch meeting.",
			agent:  "Goal first: define the launch decision, constraints, and useful outcome.",
			rule:   voiceeval.Rule{Contains: []string{"goal", "constraints", "outcome"}, NotContains: []string{"final recommendation"}},
		},
		{
			stepID: "diverge",
			user:   "We have two engineers.",
			agent:  "Alternative paths: reduce scope or stage rollout. A blind spot is support load.",
			rule:   voiceeval.Rule{Contains: []string{"alternative", "blind spot"}, NotContains: []string{"diagnosis"}},
		},
		{
			stepID: "converge",
			user:   "Staged rollout sounds right.",
			agent:  "Recommendation: choose staged rollout and make the next step an owner review.",
			rule:   voiceeval.Rule{Contains: []string{"recommendation", "next step"}, NotContains: []string{"another hour"}},
		},
	}

	var transcript []voiceeval.Turn
	for i, turn := range dialog {
		provider.push(&LiveMessage{InputTranscript: turn.user, InputTranscriptDone: true})
		var input TranscriptFrame
		readJSONFrame(t, env.conn, &input)
		if input.Type != MsgInputTranscript || input.Text != turn.user || !input.Done {
			t.Fatalf("input transcript frame = %+v", input)
		}

		provider.push(&LiveMessage{OutputTranscript: turn.agent, OutputTranscriptDone: true})
		var output TranscriptFrame
		readJSONFrame(t, env.conn, &output)
		if output.Type != MsgOutputTranscript || output.Text != turn.agent || !output.Done {
			t.Fatalf("output transcript frame = %+v", output)
		}
		transcript = append(transcript, voiceeval.Turn{
			Speaker: "agent",
			StepID:  turn.stepID,
			Text:    turn.agent,
			Rule:    turn.rule,
		})

		if i < len(dialog)-1 {
			advanceFrame, _ := json.Marshal(AdvanceStepFrame{Type: MsgAdvanceStep, Reason: "simulation"})
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if err := env.conn.Write(ctx, websocket.MessageText, advanceFrame); err != nil {
				cancel()
				t.Fatalf("write advance_step: %v", err)
			}
			cancel()
			var completed SequenceStepFrame
			readJSONFrame(t, env.conn, &completed)
			if completed.Status != "completed" || completed.StepID != turn.stepID {
				t.Fatalf("completed frame = %+v, want step %q", completed, turn.stepID)
			}
			var entered SequenceStepFrame
			readJSONFrame(t, env.conn, &entered)
			if entered.Status != "entered" || entered.StepID != dialog[i+1].stepID {
				t.Fatalf("entered frame = %+v, want step %q", entered, dialog[i+1].stepID)
			}
		}
	}

	if err := voiceeval.EvaluateTranscript(transcript); err != nil {
		t.Fatalf("dialog simulation failed instruction checks: %v", err)
	}
}

func TestAdapter_ProviderToolCallRelayedToClient(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	provider.push(&LiveMessage{
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Name: "summarize",
			Args: map[string]any{"text": "raw notes"},
		}},
		ProviderMetadata: map[string]any{"provider_event": "fake.tool.call"},
	})

	typeName, raw := readEnvelope(t, env.conn)
	if typeName != MsgToolCall {
		t.Fatalf("expected tool_call, got %s body=%s", typeName, string(raw))
	}
	var frame ToolCallFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatalf("unmarshal tool_call: %v", err)
	}
	if frame.ID != "call-1" || frame.Name != "summarize" || frame.Args["text"] != "raw notes" {
		t.Fatalf("unexpected tool_call frame: %+v", frame)
	}
	if frame.EventType != EventToolCall || !eventTypesContain(frame.EventTypes, EventToolCall) {
		t.Fatalf("tool_call event fields = %+v", frame.EventFrameFields)
	}
	if frame.ProviderMetadata["provider_event"] != "fake.tool.call" {
		t.Fatalf("tool_call provider metadata = %#v", frame.ProviderMetadata)
	}
}

func TestAdapter_ToolResponseForwardedToProvider(t *testing.T) {
	provider := newFakeProvider()
	defer provider.Close() //nolint:errcheck
	resolver := &fakeResolver{}
	env := startAdapterEnv(t, 0, provider, resolver)

	sendStart(t, env.conn, StartFrame{})
	var stateMsg StateFrame
	readJSONFrame(t, env.conn, &stateMsg)

	response, _ := json.Marshal(ToolResponseFrame{
		Type: MsgToolResponse,
		ID:   "call-1",
		Name: "summarize",
		Response: map[string]any{
			"summary": "done",
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := env.conn.Write(ctx, websocket.MessageText, response); err != nil {
		t.Fatalf("write tool_response: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.Lock()
		got := append([]ToolResponseFrame(nil), provider.toolResponses...)
		provider.mu.Unlock()
		if len(got) == 1 && got[0].ID == "call-1" && got[0].Response["summary"] == "done" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("provider did not receive tool response")
}
