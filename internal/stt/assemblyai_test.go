package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/speakercontract"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func newTestAssemblyAIProvider(serverURL string) *AssemblyAIProvider {
	p := NewAssemblyAIProvider("assembly-test-key", "universal-3-pro,universal-2")
	p.BaseURL = serverURL
	p.Validation = testValidation
	p.PollInterval = time.Millisecond
	p.PollTimeout = time.Second
	p.client.Timeout = 5 * time.Second
	return p
}

func TestAssemblyAI_Transcribe_DiarizationAndIdentification(t *testing.T) {
	var gotRequest assemblyAITranscriptRequest
	pollCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "assembly-test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v2/upload":
			if r.Method != http.MethodPost {
				t.Fatalf("upload method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(assemblyAIUploadResponse{UploadURL: "https://uploads.test/audio.wav"})
		case "/v2/transcript":
			if r.Method != http.MethodPost {
				t.Fatalf("transcript method = %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
				t.Fatalf("decode transcript request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(assemblyAITranscriptResponse{ID: "tx-1", Status: "queued"})
		case "/v2/transcript/tx-1":
			pollCount++
			resp := assemblyAITranscriptResponse{ID: "tx-1", Status: "completed", Text: "Hallo Alice", LanguageCode: "de", Confidence: 0.88,
				SpeechUnderstanding: &assemblyAISpeechUnderstanding{
					Response: assemblyAISpeechUnderstandingResponse{
						SpeakerIdentification: assemblyAISpeakerIdentificationResponse{
							Mapping: map[string]string{"A": "Alice"},
							Status:  "success",
						},
					},
				},
				Utterances: []assemblyAIUtterance{
					{
						Text:       "Hallo",
						Start:      0,
						End:        400,
						Speaker:    "A",
						Confidence: 0.91,
						Words: []assemblyAIWord{
							{Text: "Hallo", Start: 0, End: 400, Confidence: 0.95, Speaker: "A"},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := newTestAssemblyAIProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{
		Language: "de",
		Speaker: speaker.Options{
			Identification:      true,
			SpeakerType:         speaker.SpeakerTypeName,
			KnownValues:         []string{"Alice", "Bob"},
			MinSpeakersExpected: 1,
			MaxSpeakersExpected: 2,
		},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if !gotRequest.SpeakerLabels {
		t.Fatal("speaker_labels should be enabled")
	}
	if gotRequest.SpeechUnderstanding == nil {
		t.Fatal("speech_understanding should be set for identification")
	}
	known := gotRequest.SpeechUnderstanding.Request.SpeakerIdentification.KnownValues
	if strings.Join(known, ",") != "Alice,Bob" {
		t.Fatalf("known values = %#v", known)
	}
	if pollCount != 1 {
		t.Fatalf("pollCount = %d, want 1", pollCount)
	}
	if result.Speakers == nil || result.Speakers.Level != speaker.IdentificationProviderID {
		t.Fatalf("speakers = %+v", result.Speakers)
	}
	if got := result.Speakers.Segments[0].DisplayName; got != "Alice" {
		t.Fatalf("display name = %q", got)
	}
	if got := result.Speakers.Segments[0].SpeakerLabel; got != "speaker_A" {
		t.Fatalf("speaker label = %q", got)
	}
	speakercontract.AssertKnownAttribution(t, result.Speakers, "Alice")
}

func TestAssemblyAI_Transcribe_TextOnlyDoesNotSendSpeakerLabels(t *testing.T) {
	var gotRequest assemblyAITranscriptRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			_ = json.NewEncoder(w).Encode(assemblyAIUploadResponse{UploadURL: "https://uploads.test/audio.wav"})
		case "/v2/transcript":
			_ = json.NewDecoder(r.Body).Decode(&gotRequest)
			_ = json.NewEncoder(w).Encode(assemblyAITranscriptResponse{ID: "tx-1", Status: "queued"})
		case "/v2/transcript/tx-1":
			_ = json.NewEncoder(w).Encode(assemblyAITranscriptResponse{ID: "tx-1", Status: "completed", Text: "plain", Confidence: 0.7})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := newTestAssemblyAIProvider(server.URL)
	result, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if gotRequest.SpeakerLabels {
		t.Fatal("speaker_labels should be false when not requested")
	}
	if result.Speakers != nil {
		t.Fatalf("speakers = %+v, want nil", result.Speakers)
	}
}

func TestAssemblyAI_Transcribe_ProviderErrorIsRedacted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/upload":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid api key secret-token"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	p := newTestAssemblyAIProvider(server.URL)
	_, err := p.Transcribe(context.Background(), []byte("pcm"), TranscribeOpts{
		Language: "de",
		Speaker:  speaker.Options{Diarization: true},
	})
	speakercontract.AssertProviderError(t, err, "assemblyai")
}
