package openailive_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openailive"
)

// liveServer is a fake GPT-Live WebSocket: it hands every client frame to
// the test and writes whatever the test queues.
type liveServer struct {
	url     string
	headers chan http.Header
	frames  chan map[string]any
	send    chan map[string]any
}

func newLiveServer(t *testing.T) *liveServer {
	t.Helper()
	s := &liveServer{
		headers: make(chan http.Header, 1),
		frames:  make(chan map[string]any, 16),
		send:    make(chan map[string]any, 16),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.headers <- r.Header.Clone()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx := r.Context()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case frame := <-s.send:
					body, _ := json.Marshal(frame)
					if conn.Write(ctx, websocket.MessageText, body) != nil {
						return
					}
				}
			}
		}()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var frame map[string]any
			if json.Unmarshal(data, &frame) == nil {
				s.frames <- frame
			}
		}
	}))
	t.Cleanup(server.Close)
	s.url = "ws" + strings.TrimPrefix(server.URL, "http") + "/openai/v1/live/sessions"
	return s
}

// connect runs Connect while answering the session.start it sends, and
// returns that frame's session object.
func (s *liveServer) connect(t *testing.T, ctx context.Context, p *openailive.Provider, cfg live.LiveConfig) map[string]any {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- p.Connect(ctx, cfg) }()
	start := s.next(t)
	if start["type"] != "session.start" {
		t.Fatalf("first client frame = %v, want session.start", start["type"])
	}
	s.send <- map[string]any{"type": "session.started", "event_id": "ev_1", "session": map[string]any{"id": "sess_1", "model": "gpt-live-1", "status": "active"}}
	if err := <-errc; err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return object(t, start, "session")
}

func (s *liveServer) next(t *testing.T) map[string]any {
	t.Helper()
	select {
	case frame := <-s.frames:
		return frame
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a client frame")
		return nil
	}
}

// A session starts with the Live model in session.start; kernel mic audio
// reaches the server as 24 kHz PCM, and server audio and transcripts come
// back as kernel messages.
func TestSessionStreamsAudioAndTranscripts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newLiveServer(t)
	p := openailive.New()
	session := s.connect(t, ctx, p, live.LiveConfig{Endpoint: s.url, APIKey: "sk-test", Voice: "Vesper", FrameworkPrompt: "Be brief."})

	if got := (<-s.headers).Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want the API key as bearer", got)
	}
	if session["model"] != openailive.DefaultModel || session["instructions"] != "Be brief." {
		t.Fatalf("session.start session = %v", session)
	}
	audio := object(t, session, "audio")
	if object(t, audio, "output")["voice"] != "vesper" || object(t, audio, "format")["rate"] != float64(24000) {
		t.Fatalf("session.start audio = %v, want vesper at 24 kHz", audio)
	}

	mic := make([]byte, 320) // 10 ms of 16 kHz PCM16
	if err := p.SendAudio(mic); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	appended := s.next(t)
	sent, err := base64.StdEncoding.DecodeString(appended["audio"].(string))
	if appended["type"] != "session.input_audio.append" || err != nil || len(sent) != 480 {
		t.Fatalf("audio frame = %v (%d bytes, err %v), want 10 ms of 24 kHz PCM16", appended["type"], len(sent), err)
	}

	speech := []byte{1, 2, 3, 4}
	s.send <- map[string]any{"type": "session.output_audio.delta", "delta": base64.StdEncoding.EncodeToString(speech)}
	s.send <- map[string]any{"type": "session.output_transcript.delta", "event_id": "ev_2", "delta": "Hello", "start_ms": 0, "end_ms": 200}
	msg := receive(t, ctx, p)
	if msg.EventType != live.LiveEventOutputAudio || string(msg.Audio) != string(speech) {
		t.Fatalf("first message = %+v, want output audio", msg)
	}
	msg = receive(t, ctx, p)
	if msg.EventType != live.LiveEventOutputText || msg.OutputTranscript != "Hello" {
		t.Fatalf("second message = %+v, want output transcript", msg)
	}
}

// A function call from the Responses backend reaches the kernel as a tool
// call, and its result returns as a function_call_output item followed by
// response.create, the documented continuation.
func TestToolCallRoundTripsThroughResponsesDelegation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newLiveServer(t)
	p := openailive.New()
	p.BackendModel = "gpt-5.5"
	session := s.connect(t, ctx, p, live.LiveConfig{
		Endpoint: s.url,
		APIKey:   "sk-test",
		Tools: []live.ToolDefinition{{
			Name:                 "get_weather",
			Description:          "Get current weather for a location.",
			ParametersJSONSchema: map[string]any{"type": "object"},
		}},
	})
	delegation := object(t, session, "delegation")
	responses := object(t, delegation, "responses")
	tools, _ := responses["tools"].([]any)
	var tool map[string]any
	if len(tools) > 0 {
		tool, _ = tools[0].(map[string]any)
	}
	if delegation["type"] != "responses" || responses["model"] != "gpt-5.5" || tool["name"] != "get_weather" {
		t.Fatalf("session.start delegation = %v, want the backend model with the tool", delegation)
	}

	s.send <- map[string]any{
		"type":          "response.event",
		"event_id":      "ev_3",
		"delegation_id": "item_delegation_456",
		"event": map[string]any{
			"type": "response.output_item.done",
			"item": map[string]any{"type": "function_call", "call_id": "call_123", "name": "get_weather", "arguments": `{"location":"Seattle"}`},
		},
	}
	msg := receive(t, ctx, p)
	if msg.EventType != live.LiveEventToolCall || len(msg.ToolCalls) != 1 {
		t.Fatalf("message = %+v, want one tool call", msg)
	}
	call := msg.ToolCalls[0]
	if call.ID != "call_123" || call.Name != "get_weather" || call.Args["location"] != "Seattle" {
		t.Fatalf("tool call = %+v", call)
	}

	if err := p.SendToolResponse(live.ToolResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"temperature": 62}}); err != nil {
		t.Fatalf("SendToolResponse: %v", err)
	}
	result := s.next(t)
	item := object(t, result, "item")
	var output map[string]any
	if err := json.Unmarshal([]byte(item["output"].(string)), &output); err != nil {
		t.Fatalf("function_call_output.output is not JSON: %v", err)
	}
	if result["type"] != "response.item.create" || item["type"] != "function_call_output" || item["call_id"] != "call_123" || output["temperature"] != float64(62) {
		t.Fatalf("tool result frame = %v", result)
	}
	if next := s.next(t); next["type"] != "response.create" {
		t.Fatalf("frame after the result = %v, want response.create", next["type"])
	}
}

func receive(t *testing.T, ctx context.Context, p *openailive.Provider) *live.LiveMessage {
	t.Helper()
	msg, err := p.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	return msg
}

func object(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %v, want an object", key, m[key])
	}
	return v
}

// A Foundry resource key must never reach api.openai.com: without a Foundry
// endpoint the Foundry variant refuses to dial instead of falling back to
// the OpenAI default.
func TestFoundryVariantRefusesToDialWithoutEndpoint(t *testing.T) {
	p := openailive.NewFoundry()
	dialed := ""
	p.DialURL = func(endpoint string) string {
		dialed = endpoint
		return endpoint
	}
	err := p.Connect(context.Background(), live.LiveConfig{APIKey: "foundry-key", Model: "gpt-live-1"})
	if !errors.Is(err, live.ErrMissingEndpoint) {
		t.Fatalf("Connect error = %v, want ErrMissingEndpoint", err)
	}
	if dialed != "" {
		t.Fatalf("Connect dialed %q without a Foundry endpoint", dialed)
	}
}
