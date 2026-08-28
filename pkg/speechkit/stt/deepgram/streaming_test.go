package deepgram

import (
	"context"
	"encoding/json"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
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
	p.ApplyOptions(Options{
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

	// The caller asked for "de"; the provider-level "multi" must not
	// override it, matching the batch path's option layering.
	for _, want := range []string{"smart_format=true", "filler_words=true", "language=de", "endpointing=100", "keyterm=Kombify"} {
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

func TestDeepgram_StartDictationStream_UsesRealtimeDictationQuery(t *testing.T) {
	var gotQuery string
	gotFinalize := make(chan string, 1)
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
		resp := deepgramStreamingResponse{
			Type:    "Results",
			IsFinal: false,
			Channel: deepgramChannel{
				DetectedLanguage: "de",
				Alternatives: []deepgramAlternative{
					{
						Transcript: "hallo kombify",
						Confidence: 0.91,
						Words: []deepgramWord{
							{Word: "hallo", PunctuatedWord: "Hallo", Start: 0, End: 0.4, Confidence: 0.93},
							{Word: "kombify", PunctuatedWord: "Kombify", Start: 0.5, End: 0.9, Confidence: 0.89},
						},
					},
				},
			},
		}
		body, _ := json.Marshal(resp)
		if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
			t.Fatalf("write: %v", err)
		}
		typ, data, err := conn.Read(context.Background())
		if err == nil && typ == websocket.MessageText {
			gotFinalize <- string(data)
		}
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	p.ApplyOptions(Options{
		Configured:            true,
		SmartFormat:           true,
		LanguageOverride:      "multi",
		UseVocabularyKeyterms: true,
		Keyterms:              []string{"SpeechKit"},
	})
	stream, err := p.StartDictationStream(context.Background(),
		speechkit.DictationStreamOptions{
			SessionID:      42,
			Language:       "de",
			InterimResults: true,
			EndpointingMs:  250,
			Keyterms:       []string{"Kombify"},
		},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1},
	)
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close()

	event, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if event.Text != "hallo kombify" || event.SessionID != 42 || event.IsFinal {
		t.Fatalf("event = %+v", event)
	}
	if event.Language != "de" || event.Provider != "deepgram" || event.Model != "nova-3" {
		t.Fatalf("event metadata = %+v", event)
	}
	if len(event.Words) != 2 || event.Words[0].Text != "Hallo" {
		t.Fatalf("words = %+v", event.Words)
	}
	if err := stream.Finalize(context.Background()); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	select {
	case finalizePayload := <-gotFinalize:
		if !strings.Contains(finalizePayload, "Finalize") {
			t.Fatalf("finalize payload = %q", finalizePayload)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive Finalize")
	}
	for _, want := range []string{
		"interim_results=true",
		// The stream was started with Language "de"; a provider-level
		// "multi" must not outrank it.
		"language=de",
		"endpointing=250",
		"utterance_end_ms=1000",
		"keyterm=SpeechKit",
		"keyterm=Kombify",
		"encoding=linear16",
		"sample_rate=16000",
		"channels=1",
	} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query = %q, want %q", gotQuery, want)
		}
	}
	if strings.Contains(gotQuery, "diarize=true") {
		t.Fatalf("dictation stream must not force diarization: %q", gotQuery)
	}
}

func TestDeepgram_StartDictationStream_ConcatenatesUtteranceFinals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		write := func(isFinal, speechFinal bool, text string) {
			resp := deepgramStreamingResponse{
				Type:        "Results",
				IsFinal:     isFinal,
				SpeechFinal: speechFinal,
				Channel: deepgramChannel{
					Alternatives: []deepgramAlternative{{Transcript: text}},
				},
			}
			body, _ := json.Marshal(resp)
			if err := conn.Write(context.Background(), websocket.MessageText, body); err != nil {
				t.Errorf("write: %v", err)
			}
		}
		write(true, false, "Das ist ein Satz.")
		write(true, true, "Und weiter.")
		_, _, _ = conn.Read(context.Background())
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	stream, err := p.StartDictationStream(context.Background(),
		speechkit.DictationStreamOptions{InterimResults: true},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1},
	)
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close()

	draft, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive draft: %v", err)
	}
	if draft.IsFinal {
		t.Fatalf("is_final without speech_final must stay a draft: %+v", draft)
	}
	final, err := stream.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive final: %v", err)
	}
	if !final.IsFinal {
		t.Fatalf("speech_final = %+v, want a committed utterance", final)
	}
	if !strings.Contains(final.Text, "Satz. Und") {
		t.Fatalf("utterance = %q, want concatenated slices with a space", final.Text)
	}
}

func TestDeepgram_StartDictationStream_DefaultsDictationEndpointing(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
	}))
	defer server.Close()

	p := newTestDeepgramProvider(server.URL)
	stream, err := p.StartDictationStream(context.Background(),
		speechkit.DictationStreamOptions{InterimResults: true},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1},
	)
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close()
	if !strings.Contains(gotQuery, "endpointing=700") {
		t.Fatalf("query = %q, want dictation endpointing default", gotQuery)
	}
}

// Deepgram Listen treats hyphenated BCP-47 codes as distinct models, so the
// region subtag must survive; only the underscore separator is rejected
// ("No such model/language/tier combination found"). Verified against the
// live API.
func TestNormalizedDeepgramLanguagePreservesRegionAndFixesSeparator(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"  ":     "",
		"auto":   "",
		"AUTO":   "",
		"de":     "de",
		"de-DE":  "de-DE",
		"en-GB":  "en-GB",
		"de_DE":  "de-DE",
		"zh_CN":  "zh-CN",
		" en-US": "en-US",
		"multi":  "multi",
	}
	for in, want := range cases {
		if got := normalizedDeepgramLanguage(in); got != want {
			t.Errorf("normalizedDeepgramLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}

// The batch path resolves options as request > provider override; the
// streaming path used to invert that, which only became observable once a
// configured language stopped being coerced to "multi".
func TestDeepgram_DictationStreamRequestLanguageOutranksProviderOverride(t *testing.T) {
	provider := &Provider{}
	provider.ApplyOptions(Options{Configured: true, LanguageOverride: "de"})

	endpoint, err := provider.deepgramDictationStreamingEndpoint(
		"nova-3",
		stt.FirstNonEmptyTrimmed("en", provider.LanguageOverride, deepgramCodeSwitchingLanguage()),
		speechkit.DictationStreamOptions{Language: "en"},
		speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1},
	)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if !strings.Contains(endpoint, "language=en") {
		t.Fatalf("endpoint = %q, want the request language to win", endpoint)
	}
}

// With no per-request language, the provider setting is what reaches the
// API — and when neither is set, multilingual code-switching remains the
// fallback.
func TestDeepgram_DictationStreamFallsBackToProviderThenMultilingual(t *testing.T) {
	provider := &Provider{}
	provider.ApplyOptions(Options{Configured: true, LanguageOverride: "de"})
	format := speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1}

	endpoint, err := provider.deepgramDictationStreamingEndpoint(
		"nova-3",
		stt.FirstNonEmptyTrimmed("", provider.LanguageOverride, deepgramCodeSwitchingLanguage()),
		speechkit.DictationStreamOptions{},
		format,
	)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if !strings.Contains(endpoint, "language=de") {
		t.Fatalf("endpoint = %q, want the provider override", endpoint)
	}

	bare := &Provider{}
	bare.ApplyOptions(Options{Configured: true})
	endpoint, err = bare.deepgramDictationStreamingEndpoint(
		"nova-3",
		stt.FirstNonEmptyTrimmed("", bare.LanguageOverride, deepgramCodeSwitchingLanguage()),
		speechkit.DictationStreamOptions{},
		format,
	)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if !strings.Contains(endpoint, "language=multi") {
		t.Fatalf("endpoint = %q, want the multilingual fallback", endpoint)
	}
}
