//go:build linux

package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	vsserver "github.com/kombifyio/SpeechKit/app/internal/server/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

func TestOpenAIBridgeWaitsForConfigurationAckAndPreservesControl(t *testing.T) {
	configured, acknowledge := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow() //nolint:errcheck
		if _, _, err := conn.Read(r.Context()); err != nil {
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"session.created"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"input_audio_buffer.speech_started"}`))
		close(configured)
		select {
		case <-acknowledge:
		case <-r.Context().Done():
			return
		}
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"session.updated"}`))
		_, _, _ = conn.Read(r.Context())
	}))
	defer server.Close()
	inner := openai.New()
	inner.DialURL = func(string, string) string { return "ws" + strings.TrimPrefix(server.URL, "http") }
	bridge := &openaiLiveBridge{inner: inner}
	defer bridge.Close() //nolint:errcheck
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connected := make(chan error, 1)
	go func() { connected <- bridge.Connect(ctx, vsserver.LiveConfigFrame{APIKey: "test-key"}) }()
	select {
	case <-configured:
	case <-ctx.Done():
		t.Fatal("configuration was not sent")
	}
	select {
	case err := <-connected:
		t.Fatalf("socket-open was treated as ready: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(acknowledge)
	select {
	case err := <-connected:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("provider ack did not admit session")
	}
	msg, err := bridge.Receive(ctx)
	if err != nil || msg == nil || msg.EventType != string(live.LiveEventInterrupted) {
		t.Fatalf("pre-ready control was discarded: %+v %v", msg, err)
	}
}
