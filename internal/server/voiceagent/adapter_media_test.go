//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

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
