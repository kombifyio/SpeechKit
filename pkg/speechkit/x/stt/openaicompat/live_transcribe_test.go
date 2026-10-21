package openaicompat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func TestStartDictationStream_LiveTranscribeDraftsAndFinal(t *testing.T) {
	type clientEvent struct {
		Type    string `json:"type"`
		Audio   string `json:"audio"`
		Session struct {
			Type  string `json:"type"`
			Audio struct {
				Input struct {
					Transcription struct {
						Model    string   `json:"model"`
						Language string   `json:"language"`
						Keywords []string `json:"keywords"`
					} `json:"transcription"`
				} `json:"input"`
			} `json:"audio"`
		} `json:"session"`
	}
	sessionUpdate := make(chan clientEvent, 1)
	appended := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("OpenAI-Beta") != "" {
			t.Errorf("GA Realtime must not send OpenAI-Beta, got %q", r.Header.Get("OpenAI-Beta"))
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck // test cleanup
		ctx := context.Background()
		writeJSON := func(v any) {
			body, _ := json.Marshal(v)
			if err := conn.Write(ctx, websocket.MessageText, body); err != nil {
				t.Errorf("server write: %v", err)
			}
		}
		for {
			_, payload, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var event clientEvent
			if err := json.Unmarshal(payload, &event); err != nil {
				t.Errorf("client event: %v", err)
				return
			}
			switch event.Type {
			case "session.update":
				sessionUpdate <- event
				writeJSON(map[string]any{"type": "session.updated"})
			case "input_audio_buffer.append":
				audio, err := base64.StdEncoding.DecodeString(event.Audio)
				if err != nil {
					t.Errorf("append audio: %v", err)
				}
				appended <- audio
				writeJSON(map[string]any{"type": "input_audio_buffer.committed", "item_id": "item_1"})
				writeJSON(map[string]any{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item_1", "delta": "Hallo"})
				writeJSON(map[string]any{"type": "conversation.item.input_audio_transcription.delta", "item_id": "item_1", "delta": " Welt"})
				writeJSON(map[string]any{"type": "conversation.item.input_audio_transcription.completed", "item_id": "item_1", "transcript": "Hallo Welt."})
			}
		}
	}))
	defer server.Close()

	p := NewOpenAI(Options{APIKey: "sk-test"})
	p.BaseURL = server.URL
	p.Validation = testValidation

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := p.StartDictationStream(ctx, speechkit.DictationStreamOptions{
		Language:       "de-DE",
		InterimResults: true,
		Keyterms:       []string{"Kombify"},
	}, speaker.AudioFormat{Encoding: speaker.AudioEncodingPCM16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck // test cleanup

	pcm := make([]byte, 640) // 20 ms of 16 kHz mono PCM16
	if err := stream.SendPCM(ctx, pcm); err != nil {
		t.Fatalf("SendPCM: %v", err)
	}

	update := <-sessionUpdate
	transcription := update.Session.Audio.Input.Transcription
	if update.Session.Type != "transcription" || transcription.Model != OpenAILiveTranscribeModel {
		t.Fatalf("session.update = %+v, want a transcription session on %s", update.Session, OpenAILiveTranscribeModel)
	}
	if transcription.Language != "de" || len(transcription.Keywords) != 1 || transcription.Keywords[0] != "Kombify" {
		t.Fatalf("transcription config = %+v, want ISO-639-1 language and native keywords", transcription)
	}
	if audio := <-appended; len(audio) != len(pcm)*3/2 {
		t.Fatalf("appended %d bytes, want the 16 kHz chunk upsampled to 24 kHz (%d)", len(audio), len(pcm)*3/2)
	}

	var events []speechkit.DictationStreamEvent
	for len(events) == 0 || !events[len(events)-1].IsFinal {
		event, err := stream.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		events = append(events, event)
	}
	final := events[len(events)-1]
	if final.Text != "Hallo Welt." || final.Model != OpenAILiveTranscribeModel || final.Provider != "openai" || final.Language != "de-DE" {
		t.Fatalf("final = %+v", final)
	}
	for _, draft := range events[:len(events)-1] {
		if draft.IsFinal || draft.SegmentID != final.SegmentID || draft.Sequence >= final.Sequence {
			t.Fatalf("draft %+v must precede the final of the same segment %+v", draft, final)
		}
	}
	if len(events) < 2 || events[len(events)-2].Text != "Hallo Welt" {
		t.Fatalf("events = %+v, want accumulated deltas as drafts before the final", events)
	}
}

func TestStartDictationStream_RefusesNonOpenAIProvider(t *testing.T) {
	p := NewGroq(Options{APIKey: "gsk-test"})
	_, err := p.StartDictationStream(context.Background(), speechkit.DictationStreamOptions{},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingPCM16, SampleRateHz: 16000, Channels: 1})
	if !errors.Is(err, ErrDictationStreamUnsupported) {
		t.Fatalf("err = %v, want ErrDictationStreamUnsupported", err)
	}
}
