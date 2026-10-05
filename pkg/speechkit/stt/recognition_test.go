package stt_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/azurespeech"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt/openaicompat"
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
			if !slices.Equal(strings.Fields(batch.Text), strings.Fields(live.Text)) {
				t.Fatalf("batch/live recognition differ: %q / %q", batch.Text, live.Text)
			}
			expected := []string{"BaseVocabulary"}
			if tc.enabled {
				expected = append(expected, "ToolHive", "StackKits")
			}
			if tc.noStore {
				expected = append(expected, "NoStore")
			}
			if !slices.Equal(strings.Fields(live.Text), expected) {
				t.Fatalf("dictionary opt-out changed recognition incorrectly: %q", live.Text)
			}
			if live.Language != request.Language {
				t.Fatalf("stream changed requested language: %q", live.Language)
			}
		})
	}
}

// Regression: dictionary preview selected keyterms while recognition sent both
// keyterms and a prompt, and unimplemented channels were advertised as usable.
// This external consumer exercises the supplied-manifest boundary and shipped
// OpenAI model fallback against a provider fixture that recognizes sent hints.
func TestDictionaryManifestChannelParity(t *testing.T) {
	type recognitionInput struct {
		Prompt   string   `json:"prompt"`
		Keyterms []string `json:"keyterms"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input recognitionInput
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 10); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			input.Prompt = r.FormValue("prompt")
			input.Keyterms = r.MultipartForm.Value["keywords[]"]
			if definition := r.FormValue("definition"); definition != "" {
				var azure struct {
					PhraseList struct {
						Phrases []string `json:"phrases"`
					} `json:"phraseList"`
				}
				if err := json.Unmarshal([]byte(definition), &azure); err != nil {
					t.Error(err)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"combinedPhrases": []any{map[string]string{"text": strings.Join(azure.PhraseList.Phrases, " ")}}})
				return
			}
		} else if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": strings.Join(input.Keyterms, " ") + " " + input.Prompt})
	}))
	defer server.Close()
	ctx := context.Background()
	recognize := func(input recognitionInput) []string {
		t.Helper()
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result stt.Result
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return strings.Fields(result.Text)
	}
	words := []customize.Word{{Term: "ToolHive", Enabled: true}, {Term: "StackKits", Enabled: true}}
	native := provideropts.OptionSupport{ID: provideropts.OptionKeyterms, Status: provideropts.SupportNative, Implemented: true}
	prompt := provideropts.OptionSupport{ID: provideropts.OptionPromptHint, Status: provideropts.SupportDerived, Implemented: true}
	unimplemented := native
	unimplemented.Implemented = false
	providerDefault := native
	providerDefault.Status = provideropts.SupportProviderDefault
	unsupported := native
	unsupported.Status = provideropts.SupportUnsupported
	unknown := native
	unknown.Status = "unverified"
	for _, tc := range []struct {
		name    string
		rows    []provideropts.OptionSupport
		enabled bool
	}{
		{name: "native wins over prompt", rows: []provideropts.OptionSupport{native, prompt}, enabled: true},
		{name: "derived prompt", rows: []provideropts.OptionSupport{prompt}, enabled: true},
		{name: "unimplemented native falls back", rows: []provideropts.OptionSupport{unimplemented, prompt}, enabled: true},
		{name: "unsupported", rows: []provideropts.OptionSupport{unsupported}},
		{name: "unimplemented", rows: []provideropts.OptionSupport{unimplemented}},
		{name: "provider default cannot carry hints", rows: []provideropts.OptionSupport{providerDefault}},
		{name: "unknown support cannot carry hints", rows: []provideropts.OptionSupport{unknown}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest := provideropts.ProviderOptionManifest{Provider: "custom", Modality: provideropts.ModalitySTT, Options: tc.rows}
			request := (stt.TranscribeOpts{}).WithVocabulary(words)
			resolved := stt.ResolveTranscribeOptionsWithManifest(manifest, "", request, nil, nil)
			actual := recognize(recognitionInput{Prompt: resolved.Prompt, Keyterms: resolved.Keyterms})
			preview := customize.BuildProviderBiasForManifest(words, manifest).ByProvider[manifest.Provider]
			predicted := recognize(recognitionInput{Prompt: preview.String(provideropts.OptionPromptHint), Keyterms: preview.StringList(provideropts.OptionKeyterms)})
			var expected []string
			if tc.enabled {
				expected = []string{words[0].Term, words[1].Term}
			}
			if !slices.Equal(actual, expected) || !slices.Equal(predicted, expected) {
				t.Fatalf("dictionary recognition diverged from declared strategy: actual %v, preview %v, expected %v", actual, predicted, expected)
			}
		})
	}
	for _, tc := range []struct{ name, provider, model string }{
		{name: "OpenAI native keywords", provider: "openai", model: "gpt-transcribe"},
		{name: "OpenAI older model prompt", provider: "openai", model: "whisper-1"},
		{name: "custom multipart prompt", provider: "custom", model: "custom-model"},
		{name: "known identity on multipart transport", provider: "deepgram", model: "whisper-1"},
		{name: "Foundry multipart prompt", provider: "foundry", model: "gpt-transcribe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := openaicompat.New(openaicompat.Options{Name: tc.provider, APIKey: "fixture", BaseURL: server.URL, Model: tc.model})
			provider.Validation = netsec.ValidationOptions{AllowHTTP: true, AllowLoopback: true}
			request := (stt.TranscribeOpts{}).WithVocabulary(words)
			result, err := provider.Transcribe(ctx, make([]byte, 3200), request)
			if err != nil {
				t.Fatal(err)
			}
			preview := customize.BuildProviderBiasForManifest(words, provider.TranscribeManifest(tc.model)).ByProvider[provider.Name()]
			predicted := recognize(recognitionInput{Prompt: preview.String(provideropts.OptionPromptHint), Keyterms: preview.StringList(provideropts.OptionKeyterms)})
			expected := []string{words[0].Term, words[1].Term}
			if !slices.Equal(strings.Fields(result.Text), expected) || !slices.Equal(predicted, expected) {
				t.Fatalf("model dictionary channel differs: actual %q, preview %v, expected %v", result.Text, predicted, expected)
			}
		})
	}
	t.Run("Foundry Azure Speech phrase hints", func(t *testing.T) {
		provider := azurespeech.New(azurespeech.Options{APIKey: "fixture", Host: server.URL})
		provider.Validation = netsec.ValidationOptions{AllowHTTP: true, AllowLoopback: true}
		request := (stt.TranscribeOpts{}).WithVocabulary(words)
		result, err := provider.Transcribe(ctx, make([]byte, 3200), request)
		if err != nil {
			t.Fatal(err)
		}
		manifest, _ := provideropts.FindManifest(provider.Name(), provideropts.ModalitySTT)
		preview := customize.BuildProviderBiasForManifest(words, manifest).ByProvider[provider.Name()]
		predicted := recognize(recognitionInput{Prompt: preview.String(provideropts.OptionPromptHint), Keyterms: preview.StringList(provideropts.OptionKeyterms)})
		expected := []string{words[0].Term, words[1].Term}
		if !slices.Equal(strings.Fields(result.Text), expected) || !slices.Equal(predicted, expected) {
			t.Fatalf("Azure Speech dictionary channel differs: actual %q, preview %v, expected %v", result.Text, predicted, expected)
		}
	})
}
