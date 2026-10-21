package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// TestConnectAndCloseDoNotRaceOnClosedFlag reproduces a shipped defect: closed
// and closeErr are declared beside closeMu and Close guards them with it, but
// Connect reset them while holding mu instead. A reconnect racing a Close — the
// ordinary voice-agent teardown-and-redial path — therefore wrote the same two
// fields under two different mutexes.
//
// Without the fix this fails under -race with "WARNING: DATA RACE" on
// Provider.closed. It is a regression test, not a structural one: it drives the
// real Connect and Close against a real WebSocket and asserts only that the two
// may overlap safely.
func TestConnectAndCloseDoNotRaceOnClosedFlag(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		// Hold the socket open and drain whatever Connect sends; the test is
		// about the provider's own state transitions, not the protocol.
		for {
			if _, _, readErr := conn.Read(r.Context()); readErr != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	p := New()
	p.DialURL = func(string, string) string { return wsURL }

	cfg := live.LiveConfig{APIKey: "test-key", Model: "gpt-realtime"}

	for i := 0; i < 50; i++ {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = p.Connect(context.Background(), cfg)
		}()
		go func() {
			defer wg.Done()
			_ = p.Close()
		}()
		wg.Wait()
		_ = p.Close()
	}
}
