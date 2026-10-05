package stt_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
)

// Regression: desktop live recognition bypassed the batch dictionary resolver.
// Exercise the public consumer through real HTTP and WebSocket requests. The
// fixture's transcript echoes vocabulary the provider actually received, so
// enabled/disabled bias must affect recognition identically on both paths.
func TestDictionaryRecognitionParity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		text := strings.Join(r.URL.Query()["keyterm"], " ")
		if r.URL.Query().Get("mip_opt_out") == "true" {
			text += " NoStore"
		}
		frame := map[string]any{"type": "Results", "is_final": true, "speech_final": true, "channel": map[string]any{"detected_language": r.URL.Query().Get("language"), "alternatives": []any{map[string]any{"transcript": text, "confidence": 1}}}}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "finished")
			encoded, _ := json.Marshal(frame)
			if err := conn.Write(r.Context(), websocket.MessageText, encoded); err != nil {
				t.Error(err)
			}
			_, _, _ = conn.Read(r.Context())
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{"channels": []any{frame["channel"]}}})
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		name                      string
		global, override, request provideropts.Values
		enabled                   bool
		noStore                   bool
	}{
		{name: "dictionary enabled", enabled: true},
		{name: "global opt out", global: provideropts.Values{provideropts.OptionVocabularyBias: false}},
		{name: "provider opt out", override: provideropts.Values{provideropts.OptionVocabularyBias: false}},
		{name: "request wins", override: provideropts.Values{provideropts.OptionVocabularyBias: false}, request: provideropts.Values{provideropts.OptionVocabularyBias: true}, enabled: true},
		{name: "privacy override", override: provideropts.Values{provideropts.OptionNoStore: true}, enabled: true, noStore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := deepgram.New(deepgram.Options{APIKey: "fixture", Model: "nova-3"})
			provider.BaseURL = server.URL
			provider.Validation = netsec.ValidationOptions{AllowHTTP: true, AllowLoopback: true}
			provider.ApplyTuning(deepgram.Tuning{Configured: true, UseVocabularyKeyterms: true, Keyterms: []string{"BaseVocabulary"}, LanguageOverride: "de"})
			request := (stt.TranscribeOpts{Language: "multi", Options: tc.global, RequestOptions: tc.request, ProviderOptionsByProvider: map[string]provideropts.Values{"deepgram": tc.override}}).WithVocabulary([]customize.Word{{Term: "ToolHive", Enabled: true}, {Term: "StackKits", Enabled: true}, {Term: "DisabledWord", Enabled: false}})
			batch, err := provider.Transcribe(context.Background(), make([]byte, 3200), request.ForProvider(provider.Name()))
			if err != nil {
				t.Fatal(err)
			}
			streamOpts := request.DictationStreamOptions(speechkit.DictationStreamOptions{SessionID: 41, InterimResults: true})
			// Request option overrides survive the shared conversion as well.

			stream, err := provider.StartDictationStream(context.Background(), streamOpts, speaker.AudioFormat{SampleRateHz: 16000, Channels: 1, Encoding: speaker.AudioEncodingPCM16})
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			live, err := stream.Receive(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(strings.Fields(batch.Text), strings.Fields(live.Text)) {
				t.Fatalf("batch/live recognition differ: %q / %q", batch.Text, live.Text)
			}
			expected := []string{"BaseVocabulary"}
			if tc.enabled {
				expected = append(expected, "ToolHive", "StackKits")
			}
			if tc.noStore {
				expected = append(expected, "NoStore")
			}
			if !reflect.DeepEqual(strings.Fields(live.Text), expected) {
				t.Fatalf("dictionary opt-out changed recognition incorrectly: %q", live.Text)
			}
			if live.Language != request.Language {
				t.Fatalf("stream changed requested language: %q", live.Language)
			}
		})
	}
}
