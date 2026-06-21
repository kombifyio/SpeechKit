package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/speakercontract"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func TestAssemblyAI_StartSpeakerStream_LabelsSpeakersAndUnknown(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.Header.Get("Authorization") != "assembly-test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		body, _ := json.Marshal(assemblyAIStreamingTurn{
			Type:            "Turn",
			Transcript:      "hello there",
			EndOfTurn:       true,
			TurnIsFormatted: true,
			Words: []assemblyAIStreamingWord{
				{Text: "hello", Start: 0, End: 100, Confidence: 0.9, Speaker: "UNKNOWN"},
				{Text: "there", Start: 120, End: 240, Confidence: 0.88, Speaker: "A", SpeakerConfidence: 0.7},
			},
		})
		if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer server.Close()

	p := newTestAssemblyAIProvider(server.URL)
	p.StreamingBaseURL = server.URL
	stream, err := p.StartSpeakerStream(context.Background(),
		speaker.Options{Diarization: true, MaxSpeakersExpected: 2},
		speaker.AudioFormat{SampleRateHz: 16000},
	)
	if err != nil {
		t.Fatalf("StartSpeakerStream: %v", err)
	}
	defer stream.Close()

	frame, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !frame.IsFinal || frame.Text != "hello there" {
		t.Fatalf("frame = %+v", frame)
	}
	if len(frame.Words) != 2 {
		t.Fatalf("words = %+v", frame.Words)
	}
	speakercontract.AssertSpeakerFrame(t, frame)
	speakercontract.AssertUnknownIsUnattributed(t, "UNKNOWN")
	if frame.Words[0].SpeakerLabel != "" {
		t.Fatalf("UNKNOWN speaker should be unattributed, got %q", frame.Words[0].SpeakerLabel)
	}
	if frame.Words[1].SpeakerLabel != "speaker_A" {
		t.Fatalf("speaker label = %q", frame.Words[1].SpeakerLabel)
	}
	if !strings.Contains(gotQuery, "speaker_labels=true") ||
		!strings.Contains(gotQuery, "max_speakers=2") ||
		!strings.Contains(gotQuery, "speech_model=u3-rt-pro") {
		t.Fatalf("query = %q", gotQuery)
	}
}

func TestAssemblyAIStreamingEndpointUsesConfiguredBaseURL(t *testing.T) {
	p := NewAssemblyAIProvider("assembly-test-key", "universal-3-pro")
	p.StreamingBaseURL = "https://eu.streaming.example.test"

	endpoint, err := p.assemblyAIStreamingEndpoint("u3-rt-pro",
		speaker.Options{Diarization: true},
		speaker.AudioFormat{SampleRateHz: 16000},
	)
	if err != nil {
		t.Fatalf("assemblyAIStreamingEndpoint: %v", err)
	}
	if !strings.HasPrefix(endpoint, "wss://eu.streaming.example.test/v3/ws?") {
		t.Fatalf("endpoint = %q", endpoint)
	}
	if !strings.Contains(endpoint, "speaker_labels=true") {
		t.Fatalf("endpoint = %q", endpoint)
	}
}

func TestAssemblyAIStreamingIdentityMapsKnownSpeakersWhenAllowed(t *testing.T) {
	opts := speaker.Options{
		AllowProviderMapping: true,
		KnownSpeakers: []speaker.KnownSpeaker{
			{ID: "p1", DisplayName: "Alice"},
			{ID: "p2", DisplayName: "Bob"},
		},
	}
	label, personID, displayName, _ := assemblyAIStreamingIdentity("B", opts)
	if label != "speaker_B" || personID != "p2" || displayName != "Bob" {
		t.Fatalf("identity = %q/%q/%q", label, personID, displayName)
	}
}
