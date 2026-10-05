package deepgram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
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

// Configured language hints must not reject an otherwise supported listen leg.
// Deepgram accepts language_hints only on flux-general-multi; locale-aware TTS
// must still avoid English-only Flux speech when a non-English hint is present.
func TestConfiguredLanguageHintsAllowSupportedListenLegs(t *testing.T) {
	for _, model := range []string{"flux-general-multi", "flux-general-en", "nova-3"} {
		t.Run(model, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow() //nolint:errcheck // test server teardown
				ctx, cancel := context.WithTimeout(r.Context(), time.Second)
				defer cancel()
				var settings struct {
					Agent struct {
						Listen struct {
							Provider struct {
								Model         string   `json:"model"`
								LanguageHints []string `json:"language_hints"`
								Version       string   `json:"version"`
							} `json:"provider"`
						} `json:"listen"`
						Speak struct {
							Provider struct {
								Model string `json:"model"`
							} `json:"provider"`
						} `json:"speak"`
					} `json:"agent"`
				}
				_, body, err := conn.Read(ctx)
				if err != nil || json.Unmarshal(body, &settings) != nil {
					return
				}
				listen := settings.Agent.Listen.Provider
				accepted := listen.Model == model && !deepgramModelUsesFlux(settings.Agent.Speak.Provider.Model)
				if model == "flux-general-multi" {
					accepted = accepted && listen.Version == "v2" && strings.Join(listen.LanguageHints, ",") == "en,de"
				} else {
					accepted = accepted && len(listen.LanguageHints) == 0
				}
				if !accepted {
					_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"Error","description":"unsupported listen parameters"}`))
					return
				}
				_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ConversationText","role":"assistant","content":"Accepted turn"}`))
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow() //nolint:errcheck // test client teardown
			p := &Provider{ListenModel: model, SpeakModel: "flux-kit-en"}
			p.conn = conn
			if err := p.sendSettings(ctx, live.LiveConfig{Locale: "en-US", Options: provideropts.Values{provideropts.OptionLanguageHints: []string{"en", "de"}}}); err != nil {
				t.Fatal(err)
			}
			message, err := p.Receive(ctx)
			if err != nil || message == nil || message.Text == "" {
				t.Fatalf("accepted conversation: %v, %v", message, err)
			}
		})
	}
}
