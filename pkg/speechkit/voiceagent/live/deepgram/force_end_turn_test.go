package deepgram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// Releasing push-to-talk must end the user's turn on a Flux listen leg, or
// the agent waits for its own end-of-turn detection before answering; the
// Nova leg has no client-side commit and must receive nothing.
func TestSendAudioStreamEndForcesEndOfTurnOnlyForFlux(t *testing.T) {
	for _, tc := range []struct {
		listen string
		want   bool
	}{
		{listen: "flux-general-multi", want: true},
		{listen: "nova-3", want: false},
	} {
		t.Run(tc.listen, func(t *testing.T) {
			received := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow() //nolint:errcheck // test server teardown
				ctx, cancel := context.WithTimeout(r.Context(), 300*time.Millisecond)
				defer cancel()
				if _, payload, err := conn.Read(ctx); err == nil {
					received <- string(payload)
				}
				close(received)
			}))
			defer srv.Close()

			conn, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.CloseNow() //nolint:errcheck // test client teardown
			p := &Provider{ListenModel: tc.listen}
			p.conn = conn

			if err := p.SendAudioStreamEnd(); err != nil {
				t.Fatalf("SendAudioStreamEnd: %v", err)
			}
			frame, got := <-received
			if got != tc.want || (tc.want && !strings.Contains(frame, `"ForceEndTurn"`)) {
				t.Fatalf("frame = %q (received=%v), want ForceEndTurn=%v", frame, got, tc.want)
			}
		})
	}
}
