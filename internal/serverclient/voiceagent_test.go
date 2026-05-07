package serverclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

// vaTestEnv stands up a fake speechkit-server that handles
//   - POST   /v1/voiceagent/sessions          → returns ws_url to itself
//   - GET    /v1/voiceagent/sessions/{id}/ws  → upgrades + echoes scripted frames
//
// The test interacts with VoiceAgentProvider against this env. Frames the
// fake echoes back are configured per-test via env.scripted.
type vaTestEnv struct {
	srv         *httptest.Server
	startFrames chan startFrame // captures every Start frame received
	scripted    []scriptedFrame // server-side push-back, drained on first frame
	gotAudio    chan []byte     // captures binary frames sent by client
	gotJSON     chan string     // captures JSON frames sent by client
}

type scriptedFrame struct {
	binary []byte // if non-nil, send as binary frame
	text   string // if non-empty, send as JSON text frame (raw, must be valid JSON)
	delay  time.Duration
}

func newVATestEnv(t *testing.T, scripted []scriptedFrame) *vaTestEnv {
	t.Helper()
	env := &vaTestEnv{
		startFrames: make(chan startFrame, 4),
		scripted:    scripted,
		gotAudio:    make(chan []byte, 16),
		gotJSON:     make(chan string, 16),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/voiceagent/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("sessions method = %q", r.Method)
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		// The test harness ignores the ticket logic and just returns a
		// path-only ws URL so the client's normaliseWSURL fills in the
		// host. The real server returns absolute URLs; the client's
		// normalisation handles both cases.
		resp := createSessionResponse{
			SessionID: "test-session",
			WSURL:     "ws://" + r.Host + "/v1/voiceagent/sessions/test-session/ws?ticket=tkt",
			Ticket:    "tkt",
			ExpiresAt: time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/v1/voiceagent/sessions/test-session/ws", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ticket") != "tkt" {
			http.Error(w, "missing ticket", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("server accept: %v", err)
			return
		}
		// Read the mandatory Start frame.
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Errorf("read start: %v", err)
			_ = conn.Close(websocket.StatusInternalError, "no start")
			return
		}
		var sf startFrame
		_ = json.Unmarshal(data, &sf)
		select {
		case env.startFrames <- sf:
		default:
		}

		// Push scripted frames at the client.
		for _, frame := range env.scripted {
			if frame.delay > 0 {
				time.Sleep(frame.delay)
			}
			if frame.binary != nil {
				if err := conn.Write(ctx, websocket.MessageBinary, frame.binary); err != nil {
					return
				}
				continue
			}
			if frame.text != "" {
				if err := conn.Write(ctx, websocket.MessageText, []byte(frame.text)); err != nil {
					return
				}
			}
		}

		// Drain client-side traffic so SendAudio etc. are observable in
		// the test.
		for {
			typ, payload, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				cp := append([]byte(nil), payload...)
				select {
				case env.gotAudio <- cp:
				default:
				}
			} else {
				select {
				case env.gotJSON <- string(payload):
				default:
				}
			}
		}
	})
	env.srv = httptest.NewServer(mux)
	t.Cleanup(env.srv.Close)
	return env
}

func TestVoiceAgentConnectAndReceive(t *testing.T) {
	env := newVATestEnv(t, []scriptedFrame{
		{binary: []byte{0x10, 0x11, 0x12, 0x13}},
		{text: `{"type":"output_transcript","text":"hello","done":false}`},
		{text: `{"type":"interrupted"}`},
		{text: `{"type":"session_end","reason":"go_away"}`},
	})

	c, _ := New(Options{BaseURL: env.srv.URL, BearerToken: "T", HTTPClient: env.srv.Client()})
	p, _ := NewVoiceAgent(c, WithPersona("default"), WithRole("triage"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.Connect(ctx, voiceagent.LiveConfig{Voice: "Kore", Locale: "de", Model: "test-model"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer p.Close()

	// Verify the Start frame was populated correctly.
	select {
	case sf := <-env.startFrames:
		if sf.Type != "start" {
			t.Errorf("Type = %q", sf.Type)
		}
		if sf.PersonaID != "default" || sf.RoleID != "triage" {
			t.Errorf("Persona/Role = (%q, %q)", sf.PersonaID, sf.RoleID)
		}
		if sf.Voice != "Kore" || sf.Locale != "de" || sf.Model != "test-model" {
			t.Errorf("voice/locale/model = (%q, %q, %q)", sf.Voice, sf.Locale, sf.Model)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Start frame")
	}

	// Frame 1: binary audio.
	msg, err := p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 1: %v", err)
	}
	if !bytes.Equal(msg.Audio, []byte{0x10, 0x11, 0x12, 0x13}) {
		t.Errorf("audio = %x", msg.Audio)
	}

	// Frame 2: output_transcript.
	msg, err = p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 2: %v", err)
	}
	if msg.OutputTranscript != "hello" || msg.OutputTranscriptDone {
		t.Errorf("output_transcript = (%q, done=%v)", msg.OutputTranscript, msg.OutputTranscriptDone)
	}

	// Frame 3: interrupted.
	msg, err = p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 3: %v", err)
	}
	if !msg.Interrupted {
		t.Error("Interrupted = false")
	}

	// Frame 4: session_end → GoAway.
	msg, err = p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 4: %v", err)
	}
	if !msg.GoAway {
		t.Error("GoAway = false")
	}
}

func TestVoiceAgentSendAudio(t *testing.T) {
	env := newVATestEnv(t, nil)
	c, _ := New(Options{BaseURL: env.srv.URL, HTTPClient: env.srv.Client()})
	p, _ := NewVoiceAgent(c)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := p.Connect(ctx, voiceagent.LiveConfig{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer p.Close()

	// Drain the start frame.
	<-env.startFrames

	if err := p.SendAudio([]byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	select {
	case got := <-env.gotAudio:
		if !bytes.Equal(got, []byte{0x01, 0x02, 0x03}) {
			t.Errorf("audio echoed = %x", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for echoed audio")
	}

	if err := p.SendAudioStreamEnd(); err != nil {
		t.Fatalf("SendAudioStreamEnd: %v", err)
	}
	select {
	case got := <-env.gotJSON:
		if !strings.Contains(got, `"type":"audio_end"`) {
			t.Errorf("expected audio_end JSON, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for audio_end JSON")
	}

	if err := p.SendText("hello"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	select {
	case got := <-env.gotJSON:
		if !strings.Contains(got, `"text":"hello"`) {
			t.Errorf("expected text frame, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for text frame")
	}

	if err := p.AdvanceStep("host"); err != nil {
		t.Fatalf("AdvanceStep: %v", err)
	}
	select {
	case got := <-env.gotJSON:
		if !strings.Contains(got, `"type":"advance_step"`) || !strings.Contains(got, `"reason":"host"`) {
			t.Errorf("expected advance_step JSON, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for advance_step JSON")
	}
}

func TestVoiceAgentReceiveErrorFrame(t *testing.T) {
	env := newVATestEnv(t, []scriptedFrame{
		{text: `{"type":"error","code":"provider_unavailable","message":"upstream gone"}`},
	})
	c, _ := New(Options{BaseURL: env.srv.URL, HTTPClient: env.srv.Client()})
	p, _ := NewVoiceAgent(c)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, voiceagent.LiveConfig{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer p.Close()
	<-env.startFrames

	_, err := p.Receive(ctx)
	var se *ServerError
	if !errors.As(err, &se) || se.Code != "provider_unavailable" {
		t.Fatalf("err = %v, want ServerError(provider_unavailable)", err)
	}
}

func TestVoiceAgentSwallowsStateAndPongFrames(t *testing.T) {
	env := newVATestEnv(t, []scriptedFrame{
		{text: `{"type":"state","state":"listening"}`},
		{text: `{"type":"pong"}`},
		{text: `{"type":"sequence_step","step_id":"step-1","status":"entered"}`},
		{text: `{"type":"output_transcript","text":"reply","done":true}`},
	})
	c, _ := New(Options{BaseURL: env.srv.URL, HTTPClient: env.srv.Client()})
	p, _ := NewVoiceAgent(c)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := p.Connect(ctx, voiceagent.LiveConfig{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer p.Close()
	<-env.startFrames

	msg, err := p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if msg.OutputTranscript != "reply" || !msg.OutputTranscriptDone {
		t.Errorf("expected swallow-and-deliver final frame, got %+v", msg)
	}
}

func TestVoiceAgentNotConnected(t *testing.T) {
	c, _ := New(Options{BaseURL: "http://localhost:9999"})
	p, _ := NewVoiceAgent(c)
	if err := p.SendAudio([]byte{0x01}); err == nil {
		t.Fatal("SendAudio on unconnected provider returned nil")
	}
	if err := p.SendAudioStreamEnd(); err == nil {
		t.Fatal("SendAudioStreamEnd on unconnected provider returned nil")
	}
}
