//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestAdapterSelectProviderNormalizesPublicAliases(t *testing.T) {
	deepgram := newFakeProvider()
	assemblyAI := newFakeProvider()
	openAI := newFakeProvider()
	cascaded := newFakeProvider()
	adapter := &Adapter{
		DefaultProvider: "deepgram",
		Providers: map[string]ProviderFactory{
			"deepgram":   staticProviderFactory{provider: deepgram},
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
		{name: "default provider", requested: "", wantName: "deepgram", want: deepgram},
		{name: "deepgram profile id", requested: "realtime.deepgram.voice-agent", wantName: "deepgram", want: deepgram},
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
			"deepgram": staticProviderFactory{provider: newFakeProvider()},
		},
	}

	_, resolved, err := adapter.selectProvider("realtime.assemblyai.voice-agent")
	if err == nil {
		t.Fatal("selectProvider() error = nil, want unavailable provider")
	}
	if resolved != "assemblyai" {
		t.Fatalf("resolved = %q, want assemblyai", resolved)
	}
	if !strings.Contains(err.Error(), `"assemblyai"`) || !strings.Contains(err.Error(), "deepgram") {
		t.Fatalf("error = %q, want normalized provider and configured list", err.Error())
	}
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
