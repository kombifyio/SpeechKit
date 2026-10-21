package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// A Grok Voice session carries the configured voice, model and tool, maps the
// cumulative caller transcript and a function call to kernel messages, and
// answers the call with function_call_output plus response.create.
func TestGrokVoiceFunctionCallRoundTrip(t *testing.T) {
	t.Parallel()
	type frame map[string]any
	received := make(chan frame, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("model") != DefaultModel || r.Header.Get("Authorization") != "Bearer xai-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		ctx := r.Context()
		write := func(v frame) {
			body, _ := json.Marshal(v)
			_ = conn.Write(ctx, websocket.MessageText, body)
		}
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var got frame
			_ = json.Unmarshal(data, &got)
			received <- got
			if got["type"] == "session.update" {
				write(frame{"type": "session.updated"})
				write(frame{"type": "conversation.item.input_audio_transcription.updated", "item_id": "item-1", "transcript": "Book a"})
				write(frame{"type": "conversation.item.input_audio_transcription.updated", "item_id": "item-1", "transcript": "Book a table"})
				write(frame{"type": "response.function_call_arguments.done", "call_id": "call-1", "name": "ask_agent", "arguments": `{"request":"table"}`})
			}
		}
	}))
	defer server.Close()

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.Connect(ctx, live.LiveConfig{
		APIKey:   "xai-test",
		Endpoint: "ws" + strings.TrimPrefix(server.URL, "http"),
		Voice:    "Ara",
		Tools:    []live.ToolDefinition{{Name: "ask_agent", Description: "Ask the agent."}},
		Policies: live.LivePolicies{EnableInputAudioTranscription: true, ActivityDetection: live.ActivityDetectionPolicy{Automatic: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	update := <-received
	session, _ := update["session"].(map[string]any)
	tools, _ := session["tools"].([]any)
	if session["voice"] != "ara" || len(tools) != 1 || session["turn_detection"] == nil {
		t.Fatalf("session.update did not carry the Grok voice, tool and server VAD: %v", update)
	}

	var transcript strings.Builder
	var call live.ToolCall
	for call.ID == "" {
		msg, err := p.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		transcript.WriteString(msg.InputTranscript)
		if len(msg.ToolCalls) > 0 {
			call = msg.ToolCalls[0]
		}
	}
	if transcript.String() != "Book a table" || call.Name != "ask_agent" || call.Args["request"] != "table" {
		t.Fatalf("transcript %q / call %+v not mapped from the Grok events", transcript.String(), call)
	}

	if err := p.SendToolResponse(live.ToolResponse{ID: call.ID, Name: call.Name, Response: map[string]any{"result": "Done."}}); err != nil {
		t.Fatal(err)
	}
	output := <-received
	item, _ := output["item"].(map[string]any)
	if output["type"] != "conversation.item.create" || item["type"] != "function_call_output" || item["call_id"] != "call-1" || !strings.Contains(item["output"].(string), "Done.") {
		t.Fatalf("tool result not sent as function_call_output: %v", output)
	}
	if next := <-received; next["type"] != "response.create" {
		t.Fatalf("tool result did not continue the response: %v", next)
	}
}
