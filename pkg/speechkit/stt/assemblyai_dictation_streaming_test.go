package stt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func TestAssemblyAI_StartDictationStream_QueryAndTurnMapping(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.Header.Get("Authorization") != "assembly-test-key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck // test cleanup

		writeJSON := func(v any) {
			body, _ := json.Marshal(v)
			if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
				t.Errorf("server write: %v", err)
			}
		}
		writeJSON(map[string]any{"type": "Begin", "id": "sess-1"})
		// Draft (partial) turn.
		writeJSON(assemblyAIStreamingTurn{
			Type:       "Turn",
			Transcript: "hallo we",
			TurnOrder:  0,
			Words: []assemblyAIStreamingWord{
				{Text: "hallo", Start: 0, End: 400, Confidence: 0.7},
			},
		})
		// Unformatted end-of-turn stays a draft (format_turns=true semantics).
		writeJSON(assemblyAIStreamingTurn{
			Type:       "Turn",
			Transcript: "hallo welt",
			EndOfTurn:  true,
			TurnOrder:  0,
			Words: []assemblyAIStreamingWord{
				{Text: "hallo", Start: 0, End: 400, Confidence: 0.9},
				{Text: "welt", Start: 450, End: 900, Confidence: 0.93},
			},
		})
		// Formatted final.
		writeJSON(assemblyAIStreamingTurn{
			Type:            "Turn",
			Transcript:      "Hallo Welt.",
			EndOfTurn:       true,
			TurnIsFormatted: true,
			TurnOrder:       0,
			Words: []assemblyAIStreamingWord{
				{Text: "Hallo", Start: 0, End: 400, Confidence: 0.9},
				{Text: "Welt.", Start: 450, End: 900, Confidence: 0.93},
			},
		})
		// Wait for the client Terminate, then confirm and close.
		_, payload, err := conn.Read(context.Background())
		if err == nil && strings.Contains(string(payload), "Terminate") {
			writeJSON(map[string]any{"type": "Termination", "audio_duration_seconds": 1})
		}
	}))
	defer server.Close()

	p := NewAssemblyAIProvider("assembly-test-key", "")
	p.StreamingBaseURL = server.URL
	p.Validation = testValidation

	stream, err := p.StartDictationStream(context.Background(), speechkit.DictationStreamOptions{
		Language:       "de",
		InterimResults: true,
		EndpointingMs:  700,
		Keyterms:       []string{"Kombify", strings.Repeat("x", 60)},
		PromptHint:     "Homelab context; non-technical speaker.",
	}, speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck // test cleanup

	if got := gotQuery.Get("speech_model"); got != "universal-3-5-pro" {
		t.Fatalf("speech_model = %q", got)
	}
	if got := gotQuery.Get("agent_context"); got != "Homelab context; non-technical speaker." {
		t.Fatalf("agent_context = %q", got)
	}
	if got := gotQuery.Get("format_turns"); got != "true" {
		t.Fatalf("format_turns = %q", got)
	}
	if got := gotQuery.Get("max_turn_silence"); got != "700" {
		t.Fatalf("max_turn_silence = %q", got)
	}
	if got := gotQuery.Get("min_turn_silence"); got != "200" {
		t.Fatalf("min_turn_silence = %q", got)
	}
	if got := gotQuery.Get("encoding"); got != "pcm_s16le" {
		t.Fatalf("encoding = %q, want the S16LE the protocol guarantees", got)
	}
	if got := gotQuery.Get("speaker_labels"); got != "" {
		t.Fatalf("speaker_labels = %q, want absent when Diarization is false", got)
	}
	var keyterms []string
	if err := json.Unmarshal([]byte(gotQuery.Get("keyterms_prompt")), &keyterms); err != nil {
		t.Fatalf("keyterms_prompt = %q: %v", gotQuery.Get("keyterms_prompt"), err)
	}
	if len(keyterms) != 1 || keyterms[0] != "Kombify" {
		t.Fatalf("keyterms = %v, want oversized term dropped", keyterms)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	draft, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive draft: %v", err)
	}
	if draft.IsFinal || draft.Text != "hallo we" {
		t.Fatalf("draft = %+v", draft)
	}
	unformatted, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive unformatted: %v", err)
	}
	if unformatted.IsFinal {
		t.Fatalf("unformatted end-of-turn must stay a draft: %+v", unformatted)
	}
	final, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive final: %v", err)
	}
	if !final.IsFinal || final.Text != "Hallo Welt." || len(final.Words) != 2 {
		t.Fatalf("final = %+v", final)
	}
	if final.Confidence < 0.9 || final.Confidence > 0.94 {
		t.Fatalf("final confidence = %v, want mean of word confidences", final.Confidence)
	}
	if final.Provider != "assemblyai" || final.Model != "universal-3-5-pro" || final.Language != "de" {
		t.Fatalf("final metadata = %+v", final)
	}
	if final.Speakers != nil {
		t.Fatalf("final.Speakers = %+v, want nil when Diarization is false", final.Speakers)
	}

	if err := stream.Finalize(ctx); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if _, err := stream.Receive(ctx); err == nil {
		t.Fatal("Receive after Termination should end the stream")
	}
}

func TestAssemblyAI_StartDictationStream_SanitizesCatalogModelList(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck // test cleanup
	}))
	defer server.Close()

	p := NewAssemblyAIProvider("assembly-test-key", "")
	p.StreamingBaseURL = server.URL
	p.Validation = testValidation

	stream, err := p.StartDictationStream(context.Background(), speechkit.DictationStreamOptions{
		Model: "universal-3-5-pro,universal-2",
	}, speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck // test cleanup

	if got := gotQuery.Get("speech_model"); got != "universal-3-5-pro" {
		t.Fatalf("speech_model = %q, want the first streaming id from the catalog list", got)
	}
	if got := gotQuery.Get("max_turn_silence"); got != "2000" {
		t.Fatalf("max_turn_silence = %q, want patient dictation default", got)
	}
}

func TestAssemblyAI_StartDictationStream_AppliesLLMGatewayCleanup(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck // test cleanup

		writeJSON := func(v any) {
			body, _ := json.Marshal(v)
			if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
				t.Errorf("server write: %v", err)
			}
		}
		writeJSON(assemblyAIStreamingTurn{
			Type:            "Turn",
			Transcript:      "hallo welt",
			EndOfTurn:       true,
			TurnIsFormatted: true,
			TurnOrder:       0,
		})
		writeJSON(map[string]any{
			"type": "LLMGatewayResponse",
			"data": map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": "Hallo Welt."}},
				},
			},
		})
		writeJSON(map[string]any{"type": "Termination"})
	}))
	defer server.Close()

	p := NewAssemblyAIProvider("assembly-test-key", "")
	p.StreamingBaseURL = server.URL
	p.Validation = testValidation
	p.EnableStreamingLLM("qwen3.5-4b-32k-fast", "", 128)

	stream, err := p.StartDictationStream(context.Background(), speechkit.DictationStreamOptions{
		Language: "de",
	}, speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck // test cleanup

	raw := gotQuery.Get("llm_gateway")
	if raw == "" {
		t.Fatal("llm_gateway query missing")
	}
	var gateway map[string]any
	if err := json.Unmarshal([]byte(raw), &gateway); err != nil {
		t.Fatalf("llm_gateway = %q: %v", raw, err)
	}
	if gateway["model"] != "qwen3.5-4b-32k-fast" {
		t.Fatalf("llm_gateway model = %v", gateway["model"])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	final, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !final.IsFinal || final.Text != "Hallo Welt." {
		t.Fatalf("cleaned final = %+v", final)
	}
}

// Diarization is native on the v3 realtime API (the same speaker_labels param
// the speaker-stream path sets on this same endpoint), so the fix is to wire
// it — not to reject it. Pins both halves: the param goes out, and the labels
// the provider sends back actually reach the event.
func TestAssemblyAI_StartDictationStream_WiresDiarization(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck // test cleanup
		body, _ := json.Marshal(assemblyAIStreamingTurn{
			Type:            "Turn",
			Transcript:      "Hallo. Servus.",
			EndOfTurn:       true,
			TurnIsFormatted: true,
			TurnOrder:       0,
			Words: []assemblyAIStreamingWord{
				{Text: "Hallo.", Start: 0, End: 400, Confidence: 0.9, SpeakerLabel: "A", SpeakerConfidence: 0.8},
				{Text: "Servus.", Start: 450, End: 900, Confidence: 0.9, SpeakerLabel: "B", SpeakerConfidence: 0.7},
			},
		})
		if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
			t.Errorf("server write: %v", err)
		}
		_, _, _ = conn.Read(context.Background())
	}))
	defer server.Close()

	p := NewAssemblyAIProvider("assembly-test-key", "")
	p.StreamingBaseURL = server.URL
	p.Validation = testValidation

	stream, err := p.StartDictationStream(context.Background(), speechkit.DictationStreamOptions{
		Diarization: true,
	}, speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck // test cleanup

	if got := gotQuery.Get("speaker_labels"); got != "true" {
		t.Fatalf("speaker_labels = %q, want true", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	final, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive final: %v", err)
	}
	if final.Speakers == nil {
		t.Fatal("final.Speakers is nil — labels were parsed off the wire and dropped")
	}
	if got := len(final.Speakers.Speakers); got != 2 {
		t.Fatalf("speakers = %d, want 2 distinct labels", got)
	}
	if final.Speakers.Level != speaker.IdentificationDiarization {
		t.Fatalf("level = %q, want diarization", final.Speakers.Level)
	}
	if len(final.Speakers.Words) != 2 || final.Speakers.Words[0].SpeakerLabel == "" {
		t.Fatalf("words = %+v, want per-word labels", final.Speakers.Words)
	}
}

// The v3 realtime API has no channel parameter and decodes the socket as one
// channel, so stereo would braid into garbage — silently, and billed. Refuse
// before dialing.
func TestAssemblyAI_StartDictationStream_RejectsMultiChannel(t *testing.T) {
	dialed := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dialed++
	}))
	defer server.Close()

	p := NewAssemblyAIProvider("assembly-test-key", "")
	p.StreamingBaseURL = server.URL
	p.Validation = testValidation

	_, err := p.StartDictationStream(context.Background(), speechkit.DictationStreamOptions{},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 2})
	if !errors.Is(err, speechkit.ErrUnsupportedAudioFormat) {
		t.Fatalf("err = %v, want ErrUnsupportedAudioFormat", err)
	}
	if dialed != 0 {
		t.Fatalf("dialed the provider %d times; a rejected format must not open a billable session", dialed)
	}
}

func TestAssemblyAI_StartDictationStream_SkipsDraftsWithoutInterim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck // test cleanup
		writeJSON := func(v any) {
			body, _ := json.Marshal(v)
			_ = conn.Write(context.Background(), websocket.MessageText, body)
		}
		writeJSON(assemblyAIStreamingTurn{Type: "Turn", Transcript: "draft", TurnOrder: 0})
		writeJSON(assemblyAIStreamingTurn{Type: "Turn", Transcript: "Final.", EndOfTurn: true, TurnIsFormatted: true, TurnOrder: 0})
		_, _, _ = conn.Read(context.Background())
	}))
	defer server.Close()

	p := NewAssemblyAIProvider("assembly-test-key", "")
	p.StreamingBaseURL = server.URL
	p.Validation = testValidation

	stream, err := p.StartDictationStream(context.Background(), speechkit.DictationStreamOptions{InterimResults: false},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck // test cleanup

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !event.IsFinal || event.Text != "Final." {
		t.Fatalf("expected only the final without interim results, got %+v", event)
	}
}
