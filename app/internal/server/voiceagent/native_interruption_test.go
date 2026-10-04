//go:build linux

package voiceagent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/a2a"
)

// Sensitive integration boundary: both native barge-in and the existing client
// response-cancel port stop canonical work, independently of vendor HTTP close.
func TestNativeProviderInterruptionStopsCanonicalRequest(t *testing.T) {
	for _, source := range []string{"native", "client"} {
		t.Run(source, func(t *testing.T) {
			cancelled := make(chan struct{})
			started := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
				close(cancelled)
			}))
			defer upstream.Close()
			agent, err := a2a.New(a2a.Config{Endpoint: upstream.URL, TargetAgentID: "bound", SessionID: "durable"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			token := strings.Repeat("scoped-callback", 4)
			callback, err := a2a.NewOpenAIHandler(ctx, agent, token, time.Now().Add(time.Minute), "en")
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			finals := a2a.NewFinalTurnBinding("voice")
			finals.Observe("Hello", "item-1")
			callback.BindTurn = finals.Resolve
			media := newFakeProvider()
			provider := &registeredNativeProvider{LiveProviderAdapter: media, callback: callback, turns: finals}
			server := httptest.NewServer(callback)
			defer func() { cancel(); server.Close() }()
			req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"Hello"}]}`))
			req.Header.Set("Authorization", "Bearer "+token)
			finished := make(chan error, 1)
			go func() {
				response, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
				if err == nil {
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
				finished <- err
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("canonical request did not start")
			}
			if source == "native" {
				media.push(&LiveMessage{Interrupted: true})
				if _, err := provider.Receive(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				// Exercise the actual adapter cancel frame while no answer audio
				// or text has reached it, rather than invoking the port directly.
				env := startAdapterEnv(t, 0, &nativeCancelFixture{provider}, &fakeResolver{})
				sendStart(t, env.conn, StartFrame{})
				var state StateFrame
				readJSONFrame(t, env.conn, &state)
				frame, _ := json.Marshal(map[string]string{"type": MsgCancel})
				if err := env.conn.Write(ctx, websocket.MessageText, frame); err != nil {
					t.Fatal(err)
				}
				kind, _ := readEnvelope(t, env.conn)
				if kind != MsgInterrupted {
					t.Fatal("client cancellation was not acknowledged")
				}
			}
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("interruption left canonical agent work active")
			}
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Reuse the adapter's real WS loop while isolating already-bound provider
// provisioning from this interruption regression.
type nativeCancelFixture struct{ *registeredNativeProvider }

func (p *nativeCancelFixture) Connect(context.Context, LiveConfigFrame) error { return nil }
func (p *nativeCancelFixture) Close() error                                   { return p.LiveProviderAdapter.Close() }
