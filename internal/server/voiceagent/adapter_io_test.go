//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

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
