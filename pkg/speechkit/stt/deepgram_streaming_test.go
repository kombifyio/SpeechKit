package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/speakercontract"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func TestDeepgram_StartSpeakerStream_UsesStreamingDiarizeQuery(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.Header.Get("Authorization") != "Token deepgram-test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		speaker0 := 0
		resp := deepgramStreamingResponse{
			Type:        "Results",
			IsFinal:     true,
			SpeechFinal: true,
			Channel: deepgramChannel{
				Alternatives: []deepgramAlternative{
					{
						Transcript: "hello",
						Words: []deepgramWord{
							{Word: "hello", Start: 0, End: 0.5, Confidence: 0.9, Speaker: &speaker0, SpeakerConfidence: 0.8},
						},
					},
				},
			},
		}
		body, _ := json.Marshal(resp)
		if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, _, _ = conn.Read(context.Background())
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	stream, err := p.StartSpeakerStream(context.Background(),
		speaker.Options{Diarization: true, DiarizationModel: "latest"},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1},
	)
	if err != nil {
		t.Fatalf("StartSpeakerStream: %v", err)
	}
	defer stream.Close()

	frame, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if frame.Text != "hello" || frame.Segment == nil || frame.Segment.SpeakerLabel != "speaker_0" {
		t.Fatalf("frame = %+v", frame)
	}
	speakercontract.AssertSpeakerFrame(t, frame)
	if !strings.Contains(gotQuery, "diarize=true") {
		t.Fatalf("query = %q, want diarize=true", gotQuery)
	}
	if strings.Contains(gotQuery, "diarize_model") {
		t.Fatalf("streaming query must not include diarize_model: %q", gotQuery)
	}
}

func TestDeepgram_StartSpeakerStream_AppliesStreamingOptions(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		_, _, _ = conn.Read(context.Background())
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	p.ApplyOptions(DeepgramOptions{
		Configured:            true,
		SmartFormat:           true,
		FillerWords:           true,
		LanguageOverride:      "multi",
		UseVocabularyKeyterms: true,
		Keyterms:              []string{"Kombify"},
		EndpointingMs:         100,
	})
	stream, err := p.StartSpeakerStream(context.Background(),
		speaker.Options{Diarization: true, Language: "de"},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1},
	)
	if err != nil {
		t.Fatalf("StartSpeakerStream: %v", err)
	}
	defer stream.Close()
	_ = stream.EndAudio(context.Background())

	for _, want := range []string{"smart_format=true", "filler_words=true", "language=multi", "endpointing=100", "keyterm=Kombify"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query = %q, want %q", gotQuery, want)
		}
	}
}

func TestDeepgram_StartSpeakerStream_SendsAudioAndCloseStream(t *testing.T) {
	gotAudio := make(chan []byte, 1)
	gotClose := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		for i := 0; i < 2; i++ {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			switch typ {
			case websocket.MessageBinary:
				gotAudio <- data
			case websocket.MessageText:
				gotClose <- string(data)
			}
		}
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	stream, err := p.StartSpeakerStream(context.Background(), speaker.Options{Diarization: true}, speaker.AudioFormat{})
	if err != nil {
		t.Fatalf("StartSpeakerStream: %v", err)
	}
	defer stream.Close()
	if err := stream.SendAudio(context.Background(), []byte("pcm")); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	if err := stream.EndAudio(context.Background()); err != nil {
		t.Fatalf("EndAudio: %v", err)
	}
	select {
	case audio := <-gotAudio:
		if string(audio) != "pcm" {
			t.Fatalf("audio = %q", audio)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive audio")
	}
	select {
	case closePayload := <-gotClose:
		if !strings.Contains(closePayload, "CloseStream") {
			t.Fatalf("close payload = %q", closePayload)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive CloseStream")
	}
}
